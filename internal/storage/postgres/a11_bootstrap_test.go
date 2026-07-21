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
		wantExec  int
		wantError string
	}{
		{name: "first bootstrap inserts", row: a11BootstrapRow{err: pgx.ErrNoRows}, wantExec: 1},
		{name: "restart reuses exact immutable row", row: a11BootstrapRow{exact: true}},
		{name: "restart rejects conflicting immutable row", row: a11BootstrapRow{exact: false}, wantError: "a11_reference_bootstrap_conflict"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &a11BootstrapExecutor{row: test.row}
			err := ensureA11AssetScreening(context.Background(), executor, "BTC", "approved", "configuration-v1a", now)
			if test.wantError == "" && err != nil {
				t.Fatalf("ensure screening failed: %v", err)
			}
			if test.wantError != "" && (err == nil || err.Error() != test.wantError) {
				t.Fatalf("ensure screening error = %v, want %s", err, test.wantError)
			}
			if executor.execCalls != test.wantExec {
				t.Fatalf("screening inserts = %d, want %d", executor.execCalls, test.wantExec)
			}
		})
	}
}

type a11BootstrapExecutor struct {
	row       pgx.Row
	execCalls int
}

func (executor *a11BootstrapExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	executor.execCalls++
	return pgconn.CommandTag{}, nil
}

func (executor *a11BootstrapExecutor) QueryRow(context.Context, string, ...any) pgx.Row {
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
