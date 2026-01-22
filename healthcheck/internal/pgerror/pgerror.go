package pgerror

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func GetConstraintName(err error) (string, bool) {
	if err == nil {
		return "", false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505",
			"23503",
			"23514",
			"23502":
			if pgErr.ConstraintName != "" {
				return pgErr.ConstraintName, true
			}
		}
	}
	return "", false
}
