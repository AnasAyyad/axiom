package postgres

import (
	"context"
	"fmt"

	"axiom/internal/recorder"

	"github.com/jackc/pgx/v5"
)

// VerifyRegistered proves that an older manifest without embedded build
// provenance already has a complete durable catalog row. It never substitutes
// the currently running build identity for missing historical provenance.
func (catalog *RecordedDatasetCatalog) VerifyRegistered(ctx context.Context,
	manifest recorder.DatasetManifest, kind string) (string, error) {
	if catalog == nil || catalog.pool == nil || len(manifest.Hash) < 24 ||
		(kind != "public_market" && kind != "decision_inputs") {
		return "", fmt.Errorf("owner_console_dataset_manifest_invalid")
	}
	id := "dataset-" + manifest.Hash[:24]
	tx, err := catalog.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyRecordedDatasetRegistration(ctx, tx, id, manifest, kind); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}
