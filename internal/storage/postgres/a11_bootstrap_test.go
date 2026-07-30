package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestEnsureA11AssetScreeningIsRestartIdempotentAndFailClosed(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		row       pgx.Row
		configID  string
		wantExec  int
		wantError string
	}{
		{name: "first bootstrap inserts", row: a11BootstrapRow{err: pgx.ErrNoRows},
			configID: "configuration-v1a", wantExec: 1},
		{name: "restart reuses exact immutable row", row: a11BootstrapRow{exact: true},
			configID: "configuration-v1a"},
		{name: "later configuration reuses unchanged immutable screening", row: a11BootstrapRow{exact: true},
			configID: "configuration-v1c"},
		{name: "restart rejects conflicting immutable row", row: a11BootstrapRow{exact: false},
			configID: "configuration-v1a", wantError: "a11_reference_bootstrap_conflict"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &a11BootstrapExecutor{row: test.row}
			err := ensureA11AssetScreening(context.Background(), executor, "BTC", "approved", test.configID, now)
			if test.wantError == "" && err != nil {
				t.Fatalf("ensure screening failed: %v", err)
			}
			if test.wantError != "" && (err == nil || err.Error() != test.wantError) {
				t.Fatalf("ensure screening error = %v, want %s", err, test.wantError)
			}
			if executor.execCalls != test.wantExec {
				t.Fatalf("screening inserts = %d, want %d", executor.execCalls, test.wantExec)
			}
			if len(executor.queryArguments) != 3 {
				t.Fatalf("screening lookup arguments = %d, want current-fact tuple only", len(executor.queryArguments))
			}
		})
	}
}

type a11BootstrapExecutor struct {
	row            pgx.Row
	execCalls      int
	queryArguments []any
}

func (executor *a11BootstrapExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	executor.execCalls++
	return pgconn.CommandTag{}, nil
}

func (executor *a11BootstrapExecutor) QueryRow(_ context.Context, _ string, arguments ...any) pgx.Row {
	executor.queryArguments = append([]any(nil), arguments...)
	return executor.row
}

type a11BootstrapRow struct {
	exact bool
	err   error
}

func (row a11BootstrapRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected bootstrap scan destination")
	}
	exact, ok := destinations[0].(*bool)
	if !ok {
		return errors.New("unexpected bootstrap scan type")
	}
	*exact = row.exact
	return nil
}
