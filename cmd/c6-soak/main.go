package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"axiom/internal/config"
	"axiom/internal/qualification/c6"
	postgresstore "axiom/internal/storage/postgres"
)

func main() {
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "c6-soak:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if os.Getenv("AXIOM_C6_SOAK_ENABLED") != "1" ||
		os.Getenv("AXIOM_C6_SOAK_MODE") != "formal" {
		return fmt.Errorf("formal runner is default-off")
	}
	if err := c6.VerifyCurrentExecutableHash(os.Getenv("AXIOM_C6_EXECUTABLE_HASH")); err != nil {
		return err
	}
	runtimeConfig, err := config.LoadRuntime()
	if err != nil {
		return err
	}
	pool, err := postgresstore.Open(ctx, runtimeConfig.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	store, err := postgresstore.NewV1CC6QualificationStore(pool)
	if err != nil {
		return err
	}
	configuration, err := formalConfiguration(ctx, store)
	if err != nil {
		return err
	}
	evidence, runErr := (c6.Runner{
		Clock: c6.RealClock{}, Probe: store, Store: store, Chaos: store,
	}).Run(ctx, configuration)
	if err = writeTerminalEvidence(evidence); err != nil {
		return err
	}
	return runErr
}

func formalConfiguration(
	ctx context.Context,
	store *postgresstore.V1CC6QualificationStore,
) (c6.Config, error) {
	configurationHash := os.Getenv("AXIOM_C6_CONFIGURATION_HASH")
	accounts, err := store.QualificationAccounts(ctx, configurationHash)
	if err != nil {
		return c6.Config{}, err
	}
	interval, err := time.ParseDuration(
		value("AXIOM_C6_SAMPLE_INTERVAL", "1m"),
	)
	if err != nil || interval < 15*time.Second || interval > 5*time.Minute {
		return c6.Config{}, fmt.Errorf("sample interval rejected")
	}
	sourceDirty, err := strconv.ParseBool(
		value("AXIOM_C6_SOURCE_DIRTY", "true"),
	)
	if err != nil {
		return c6.Config{}, fmt.Errorf("source identity rejected")
	}
	configuration := c6.Config{
		Enabled: true,
		Identity: c6.Identity{
			RunID: os.Getenv("AXIOM_C6_RUN_ID"), Mode: c6.ModeFormal,
			CommitSHA:         os.Getenv("AXIOM_C6_COMMIT_SHA"),
			BuildHash:         os.Getenv("AXIOM_C6_BUILD_HASH"),
			ExecutableHash:    os.Getenv("AXIOM_C6_EXECUTABLE_HASH"),
			ImageHash:         os.Getenv("AXIOM_C6_IMAGE_HASH"),
			ConfigurationHash: configurationHash,
			SourceDirty:       sourceDirty, Accounts: accounts,
		},
		Duration: c6.FormalDuration, SampleInterval: interval,
		EvidencePath: os.Getenv("AXIOM_C6_EVIDENCE_PATH"),
	}
	if err = c6.ValidateConfig(configuration); err != nil {
		return c6.Config{}, err
	}
	return configuration, nil
}

func writeTerminalEvidence(evidence c6.Evidence) error {
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"event_code": "c6_qualification_terminal",
		"run_id":     evidence.Identity.RunID, "state": evidence.State,
		"qualified":              evidence.Qualified,
		"profitability_evidence": false,
		"evidence_hash":          evidence.EvidenceHash,
	})
}

func value(name, fallback string) string {
	if result := os.Getenv(name); result != "" {
		return result
	}
	return fallback
}
