package postgres

import (
	"context"
	"fmt"
	"net"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/models"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/pgerror"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
)

const (
	checksTable = "settings"
)

var _ any = pgx.Batch{}

type Repository struct {
	db *pgxpool.Pool
}

func NewRepo(ctx context.Context, user, password, addr string, port uint16) (*Repository, error) {
	cfg, err := pgxpool.ParseConfig(
		fmt.Sprintf(
			"user=%s password=%s host=%s port=%d dbname=postgres sslmode=disable pool_max_conns=15",
			user, password, addr, port,
		),
	)
	if cfg == nil {
		return nil, fmt.Errorf("failed to parse pgx config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}
	err = pool.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ping db: %w", err)
	}
	return &Repository{
		db: pool,
	}, nil
}

func (r *Repository) CreateHealthCheckSetting(ctx context.Context, check healthcheck.Settings) error {
	sql := `
	insert into settings (target_group, interval, success_before_passing,
	failures_before_critical, initial_state, strategy, strategy_settings)
	values ($1, $2, $3, $4, $5, $6, $7);
	`
	_, err := r.db.Exec(ctx, sql,
		check.TargetGroup,
		check.Interval,
		check.SuccessBeforePassing,
		check.FailuresBeforeCritical,
		check.InitialState,
		check.Strategy,
		check.StrategySettings,
	)
	if err != nil {
		constraint, ok := pgerror.GetConstraintName(err)
		if !ok {
			return fmt.Errorf("failed to create hc: %w", err)
		}
		switch constraint {
		case "settings_pkey":
			return fmt.Errorf("can't create two same settings for one pool")
		}
	}
	return nil
}

func (r *Repository) CreateTargets(ctx context.Context, targets []healthcheck.Target, vshards []string) (uint, error) {
	if len(targets) == 0 {
		return 0, nil
	}
	if len(targets) != len(vshards) {
		return 0, fmt.Errorf("count of targets is not equal to vshards count")
	}

	sql := `
	insert into targets (real_ip, port, target_group, vshard)
	values ($1, $2, $3, $4);
	`

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to start creation transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	batch := &pgx.Batch{}
	for i, target := range targets {
		batch.Queue(
			sql,
			target.RealIP.String(),
			target.Port,
			target.TargetGroup,
			vshards[i],
		)
	}

	bResult := tx.SendBatch(ctx, batch)
	defer bResult.Close()

	created := uint(0)
	for _, target := range targets {
		_, err := bResult.Exec()
		if err != nil {
			constraint, ok := pgerror.GetConstraintName(err)
			if !ok {
				return 0, fmt.Errorf("failed to create hc: %w", err)
			}
			switch constraint {
			case "targets_pkey":
				log.Warn().Msgf(
					"target %s in target-group %s already exists",
					target.ToAddr().String(),
					target.TargetGroup,
				)
				created++
				continue
			}
			return 0, fmt.Errorf("failed to create targets: %w", err)
		}
		created++
	}
	err = bResult.Close()
	if err != nil {
		constraint, _ := pgerror.GetConstraintName(err)
		if constraint != "targets_pkey" {
			return 0, fmt.Errorf("failed to close tx batch: %w", err)
		}
	}
	err = tx.Commit(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to commit targets creation batch tx: %w", err)
	}
	return created, nil
}

func (r *Repository) RemoveTargets(ctx context.Context, targets []healthcheck.Target) (uint, error) {
	if len(targets) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to start remove transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	batch := &pgx.Batch{}
	for _, target := range targets {
		ipStr := target.RealIP.String()
		batch.Queue(
			`delete from target_statuses where real_ip = $1 and port = $2 and target_group = $3`,
			ipStr,
			target.Port,
			target.TargetGroup,
		)
		batch.Queue(
			`delete from targets where real_ip = $1 and port = $2 and target_group = $3`,
			ipStr,
			target.Port,
			target.TargetGroup,
		)
	}

	bResult := tx.SendBatch(ctx, batch)
	defer bResult.Close()

	deleted := uint(0)
	for _, target := range targets {
		_, err := bResult.Exec()
		if err != nil {
			return 0, fmt.Errorf("failed to remove target status for %+v: %w", target, err)
		}
		tag, err := bResult.Exec()
		if err != nil {
			return 0, fmt.Errorf("failed to remove target %+v: %w", target, err)
		}
		deleted += uint(tag.RowsAffected())
	}

	if err := bResult.Close(); err != nil {
		return 0, fmt.Errorf("failed to close tx batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit targets removing batch tx: %w", err)
	}

	return deleted, nil
}

func (r *Repository) UpdateTargetsStatuses(ctx context.Context, events []models.HcEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}

	sql := `
	insert into target_statuses (real_ip, port, target_group, status, last_error)
	values ($1, $2, $3, $4, $5)
	on conflict (real_ip, port, target_group)
	do update set
		status = excluded.status,
		last_error = excluded.last_error,
		updated_at = case
			when excluded.status = true then now()
			when target_statuses.updated_at <= now() - $6::interval then now()
			else target_statuses.updated_at
		end
	where excluded.status = true
	or target_statuses.updated_at <= now() - $6::interval;
	`
	b := pgx.Batch{}
	for _, event := range events {
		intervalStr := fmt.Sprintf("%d microseconds", event.HcInterval.Microseconds())
		errorStr := ""
		if event.Error != nil {
			errorStr = event.Error.Error()
		}
		b.Queue(
			sql,
			event.Target.RealIP.String(),
			event.Target.Port,
			event.TargetGroup,
			event.NewStatus,
			errorStr,
			intervalStr,
		)
	}
	result := r.db.SendBatch(ctx, &b)
	defer result.Close()

	for i, event := range events {
		tag, err := result.Exec()
		if err != nil {
			return i, fmt.Errorf("failed to update event: %w", err)
		}
		if tag.RowsAffected() > 0 {
			log.Info().Msgf("updated target status by event: %+v", event)
		} else {
			log.Warn().Msgf("not updated target status by event %+v, several nodes make updates, skip this", event)
		}
	}
	return len(events), nil
}

func (r *Repository) GetTargets(ctx context.Context, vshards []uint) ([]healthcheck.Target, error) {
	sql := `
	select real_ip, port, target_group
	from targets
	where vshard = any($1);
	`

	rows, err := r.db.Query(ctx, sql, vshards)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	result := make([]healthcheck.Target, 0, 100)
	for rows.Next() {
		target := healthcheck.Target{}
		var strIP string
		err = rows.Scan(
			&strIP,
			&target.Port,
			&target.TargetGroup,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan target value: %w", err)
		}
		ip := net.ParseIP(strIP)
		target.RealIP = ip
		result = append(result, target)
	}
	return result, nil
}

func (r *Repository) GetTargetStatuses(
	ctx context.Context,
	targetGroup healthcheck.TargetGroupID,
) ([]models.TargetStatus, error) {
	sql := `
	select real_ip, port, target_group, last_error, status
	from target_statuses
	where target_group = $1;
	`

	rows, err := r.db.Query(ctx, sql, targetGroup)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	result := make([]models.TargetStatus, 0, 100)
	for rows.Next() {
		target := models.TargetStatus{}
		var strIP string
		err = rows.Scan(
			&strIP,
			&target.Target.Port,
			&target.TargetGroup,
			&target.Error,
			&target.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan target value: %w", err)
		}
		ip := net.ParseIP(strIP)
		target.Target.RealIP = ip
		result = append(result, target)
	}
	return result, nil
}

func (r *Repository) GetSettingsForTargetGroups(
	ctx context.Context,
	targetGroups []healthcheck.TargetGroupID,
) (map[healthcheck.TargetGroupID]healthcheck.Settings, error) {
	if len(targetGroups) == 0 {
		return nil, nil
	}

	sql, args, err := squirrel.Select(
		"target_group",
		"interval",
		"success_before_passing",
		"failures_before_critical",
		"initial_state",
		"strategy",
		"strategy_settings",
	).From(checksTable).
		Where(squirrel.Eq{"target_group": targetGroups}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to create db request: %w", err)
	}

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	result := make(map[healthcheck.TargetGroupID]healthcheck.Settings, 10000)
	for rows.Next() {
		hc := healthcheck.Settings{}
		err = rows.Scan(
			&hc.TargetGroup,
			&hc.Interval,
			&hc.SuccessBeforePassing,
			&hc.FailuresBeforeCritical,
			&hc.InitialState,
			&hc.Strategy,
			&hc.StrategySettings,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan target value: %w", err)
		}
		result[hc.TargetGroup] = hc
	}
	return result, nil
}
