package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"axiom/internal/backup"
)

func restore(ctx context.Context, settings settings, passfile string) error {
	if _, err := backup.ValidateIndependentDestination(settings.destination, settings.protected); err != nil {
		return err
	}
	started := time.Now().UTC()
	if !filepath.IsAbs(settings.manifest) {
		return fmt.Errorf("restore_manifest_invalid")
	}
	manifest, err := backup.ReadArtifactManifest(settings.manifest)
	if err != nil || manifest.Spec.Database != settings.database {
		return fmt.Errorf("restore_manifest_invalid")
	}
	root := filepath.Dir(settings.manifest)
	if err = backup.RestoreArtifact(root, manifest, io.Discard, settings.key); err != nil {
		return err
	}
	if err = validateArchive(ctx, root, settings.validationRoot, manifest, settings.key); err != nil {
		return err
	}
	empty, err := targetIsEmpty(ctx, settings, passfile)
	if err != nil || !empty {
		return fmt.Errorf("restore_target_not_clean")
	}
	if err = executeDatabaseRestore(ctx, settings, passfile, root, manifest); err != nil {
		return err
	}
	if err = verifyRestoredDatabase(ctx, settings, passfile, manifest); err != nil {
		return err
	}
	references, err := restoredMarketSegments(ctx, settings, passfile)
	if err != nil {
		return err
	}
	marketEvidence, err := backup.VerifyMarketRecovery(settings.marketRoot, references)
	if err != nil {
		return err
	}
	if settings.requireMarketRecovery && marketEvidence.VerifiedSegments == 0 {
		return fmt.Errorf("restore_market_recovery_empty")
	}
	return writeRestoreCompletion(root, manifest, started, marketEvidence, settings.key)
}

func executeDatabaseRestore(ctx context.Context, settings settings, passfile, root string,
	manifest backup.ArtifactManifest) error {
	command := exec.CommandContext(ctx, "pg_restore", restoreArguments(settings)...)
	command.Env = append(os.Environ(), "PGPASSFILE="+passfile)
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("restore_process_unavailable")
	}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err = command.Start(); err != nil {
		return fmt.Errorf("restore_process_unavailable")
	}
	decryptErr := backup.RestoreArtifact(root, manifest, stdin, settings.key)
	closeErr := stdin.Close()
	waitErr := command.Wait()
	if decryptErr != nil || closeErr != nil || waitErr != nil {
		return fmt.Errorf("restore_failed")
	}
	return nil
}

func writeRestoreCompletion(root string, manifest backup.ArtifactManifest, started time.Time,
	market backup.MarketRecoveryEvidence, key [32]byte) error {
	evidencePath, evidence, err := backup.WriteRestoreEvidence(
		root, manifest, started, time.Now().UTC(), market, key,
	)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "restore_complete name=%s schema=%s duration_seconds=%d market_segments=%d evidence=%s hash=%s\n",
		manifest.Spec.Name, manifest.Spec.SchemaVersion, evidence.DurationSeconds,
		evidence.MarketSegmentsVerified, filepath.Base(evidencePath), evidence.EvidenceHash)
	return nil
}

func restoredMarketSegments(ctx context.Context, settings settings,
	passfile string) ([]backup.MarketSegmentReference, error) {
	output, err := runPSQL(ctx, settings, passfile, restoredMarketSegmentsQuery)
	if err != nil || len(output) == 0 || len(output) > 64<<20 {
		return nil, fmt.Errorf("restore_market_catalogue_unavailable")
	}
	var references []backup.MarketSegmentReference
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&references); err != nil {
		return nil, fmt.Errorf("restore_market_catalogue_invalid")
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("restore_market_catalogue_invalid")
	}
	return references, nil
}
