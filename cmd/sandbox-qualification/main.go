package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"axiom/internal/config"
	"axiom/internal/qualification/sandboxqualification"
	postgresstore "axiom/internal/storage/postgres"
)

func main() {
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "sandbox_qualification-soak:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if os.Getenv("AXIOM_SANDBOX_QUALIFICATION_ENABLED") != "1" ||
		os.Getenv("AXIOM_SANDBOX_QUALIFICATION_MODE") != "formal" {
		return fmt.Errorf("formal runner is default-off")
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
	store, err := postgresstore.NewSandboxQualificationStore(pool)
	if err != nil {
		return err
	}
	configuration, err := formalConfiguration(ctx, store)
	if err != nil {
		return err
	}
	evidence, runErr := (sandboxQualification.Runner{
		Clock: sandboxQualification.RealClock{}, Probe: store, Store: store, Chaos: store,
	}).Run(ctx, configuration)
	if err = writeTerminalEvidence(evidence); err != nil {
		return err
	}
	return runErr
}

func formalConfiguration(
	ctx context.Context,
	store *postgresstore.SandboxQualificationStore,
) (sandboxQualification.Config, error) {
	configurationHash := os.Getenv("AXIOM_SANDBOX_QUALIFICATION_CONFIGURATION_HASH")
	accounts, err := store.QualificationAccounts(ctx, configurationHash)
	if err != nil {
		return sandboxQualification.Config{}, err
	}
	interval, err := time.ParseDuration(
		value("AXIOM_SANDBOX_QUALIFICATION_SAMPLE_INTERVAL", "1m"),
	)
	if err != nil || interval < 15*time.Second || interval > 5*time.Minute {
		return sandboxQualification.Config{}, fmt.Errorf("sample interval rejected")
	}
	sourceDirty, err := strconv.ParseBool(
		value("AXIOM_SANDBOX_QUALIFICATION_SOURCE_DIRTY", "true"),
	)
	if err != nil {
		return sandboxQualification.Config{}, fmt.Errorf("source identity rejected")
	}
	configuration := sandboxQualification.Config{
		Enabled: true,
		Identity: sandboxQualification.Identity{
			RunID: os.Getenv("AXIOM_SANDBOX_QUALIFICATION_RUN_ID"), Mode: sandboxQualification.ModeFormal,
			CommitSHA:         os.Getenv("AXIOM_SANDBOX_QUALIFICATION_COMMIT_SHA"),
			BuildHash:         os.Getenv("AXIOM_SANDBOX_QUALIFICATION_BUILD_HASH"),
			ExecutableHash:    os.Getenv("AXIOM_SANDBOX_QUALIFICATION_EXECUTABLE_HASH"),
			ImageHash:         os.Getenv("AXIOM_SANDBOX_QUALIFICATION_IMAGE_HASH"),
			ConfigurationHash: configurationHash,
			SourceDirty:       sourceDirty, Accounts: accounts,
		},
		Duration: sandboxQualification.FormalDuration, SampleInterval: interval,
		EvidencePath: os.Getenv("AXIOM_SANDBOX_QUALIFICATION_EVIDENCE_PATH"),
	}
	if err = sandboxQualification.ValidateConfig(configuration); err != nil {
		return sandboxQualification.Config{}, err
	}
	return configuration, nil
}

func writeTerminalEvidence(evidence sandboxQualification.Evidence) error {
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"event_code": "sandbox_qualification_terminal",
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
