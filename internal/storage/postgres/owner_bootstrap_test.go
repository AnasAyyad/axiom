package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestEnsureOwnerConsoleAssetScreeningIsRestartIdempotentAndFailClosed(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		row       pgx.Row
		configID  string
		wantExec  int
		wantError string
	}{
		{name: "first bootstrap inserts", row: ownerConsoleBootstrapRow{err: pgx.ErrNoRows},
			configID: "configuration-trend-following", wantExec: 1},
		{name: "restart reuses exact immutable row", row: ownerConsoleBootstrapRow{exact: true},
			configID: "configuration-trend-following"},
		{name: "later configuration reuses unchanged immutable screening", row: ownerConsoleBootstrapRow{exact: true},
			configID: "configuration-sandbox_runtime"},
		{name: "restart rejects conflicting immutable row", row: ownerConsoleBootstrapRow{exact: false},
			configID: "configuration-trend-following", wantError: "owner_console_reference_bootstrap_conflict"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &ownerConsoleBootstrapExecutor{row: test.row}
			err := ensureOwnerConsoleAssetScreening(context.Background(), executor, "BTC", "approved", test.configID, now)
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

type ownerConsoleBootstrapExecutor struct {
	row            pgx.Row
	execCalls      int
	queryArguments []any
}

func (executor *ownerConsoleBootstrapExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	executor.execCalls++
	return pgconn.CommandTag{}, nil
}

func (executor *ownerConsoleBootstrapExecutor) QueryRow(_ context.Context, _ string, arguments ...any) pgx.Row {
	executor.queryArguments = append([]any(nil), arguments...)
	return executor.row
}

type ownerConsoleBootstrapRow struct {
	exact bool
	err   error
}

func (row ownerConsoleBootstrapRow) Scan(destinations ...any) error {
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
