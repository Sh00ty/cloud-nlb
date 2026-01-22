package yugabyte

import (
	"context"
	"fmt"
	"net"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/network-lb/health-check-node/internal/models"
	"github.com/Sh00ty/network-lb/health-check-node/internal/pgerror"
	"github.com/Sh00ty/network-lb/health-check-node/pkg/healthcheck"
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
	insert into settings (id, pool_name, interval, success_before_passing, 
	failures_before_critical, initial_state, strategy, strategy_settings)
	values ($1, $2, $3, $4, $5, $6, $7, $8);
	`
	_, err := r.db.Exec(ctx, sql,
		check.ID,
		"pool-name",
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

func (r *Repository) UpdateTargetsStatuses(ctx context.Context, events []models.HcEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}

	sql := `
	insert into target_statuses (real_ip, port, setting_id, status, last_error)
	values ($1, $2, $3, $4, $5)
	on conflict (real_ip, port, setting_id)
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
		intervalStr := fmt.Sprintf("%d microseconds", event.HcInverval.Microseconds())
		errorStr := ""
		if event.Error != nil {
			errorStr = event.Error.Error()
		}
		b.Queue(
			sql,
			event.Target.RealIP.String(),
			event.Target.Port,
			event.SettingID,
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

func (r *Repository) CreateTarget(ctx context.Context, target healthcheck.Target, vshard string) error {
	sql := `
	insert into targets (real_ip, port, setting_id, vshard)
	values ($1, $2, $3, $4);
	`

	_, err := r.db.Exec(
		ctx,
		sql,
		target.RealIP.String(),
		target.Port,
		target.SettingID,
		vshard,
	)
	if err != nil {
		constraint, ok := pgerror.GetConstraintName(err)
		if !ok {
			return fmt.Errorf("failed to create hc: %w", err)
		}
		switch constraint {
		case "targets_pkey":
			return fmt.Errorf(
				"target %s with setting %d already exists",
				target.ToAddr().String(),
				target.SettingID,
			)
		}
		return fmt.Errorf("failed to create target: %w", err)
	}
	return nil
}

func (r *Repository) GetTargets(ctx context.Context, vshards []uint) ([]healthcheck.Target, error) {
	sql := `
	select real_ip, port, setting_id 
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
			&target.SettingID,
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

func (r *Repository) GetSettingsForTargets(ctx context.Context, settingIDs []int64) (map[int64]healthcheck.Settings, error) {
	if len(settingIDs) == 0 {
		return nil, nil
	}

	sql, args, err := squirrel.Select(
		"id",
		"interval",
		"success_before_passing",
		"failures_before_critical",
		"initial_state",
		"strategy",
		"strategy_settings",
	).From(checksTable).
		Where(squirrel.Eq{"id": settingIDs}).
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

	result := make(map[int64]healthcheck.Settings, 10000)
	for rows.Next() {
		hc := healthcheck.Settings{}
		err = rows.Scan(
			&hc.ID,
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
		result[hc.ID] = hc
	}
	return result, nil
}
