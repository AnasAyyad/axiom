package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"axiom/internal/config"
	"axiom/internal/qualification/sandboxqualification"
	postgresstore "axiom/internal/storage/postgres"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type gateResult struct {
	Passed         bool
	Status         string
	TranscriptHash string
	Fact           string
}

type gateExecutor interface {
	execute(context.Context, string, string, string) gateResult
}

type makeGate struct{}

type controllerConfig struct {
	runID          string
	commitSHA      string
	sourceRoot     string
	controllerHash string
}

type fixedHarness struct {
	passed bool
	fact   string
}

func main() {
	if err := run(context.Background(), makeGate{}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "sandbox-qualification-chaos:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, gate gateExecutor) error {
	if os.Getenv("AXIOM_SANDBOX_QUALIFICATION_CHAOS_ENABLED") != "1" ||
		os.Getenv("AXIOM_SANDBOX_QUALIFICATION_CHAOS_MODE") != "formal" {
		return fmt.Errorf("chaos controller is default-off")
	}
	configuration, err := loadControllerConfig(ctx, gate)
	if err != nil {
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
	store, err := postgresstore.NewSandboxQualificationStore(pool)
	if err != nil {
		return err
	}
	if _, err = store.AssertChaosRun(
		ctx, configuration.runID, configuration.commitSHA,
	); err != nil {
		return err
	}
	return recordChaos(ctx, gate, store, configuration)
}

func loadControllerConfig(
	ctx context.Context,
	gate gateExecutor,
) (controllerConfig, error) {
	controllerHash := os.Getenv("AXIOM_SANDBOX_QUALIFICATION_CHAOS_EXECUTABLE_HASH")
	if err := sandboxQualification.VerifyCurrentExecutableHash(controllerHash); err != nil {
		return controllerConfig{}, err
	}
	runID := os.Getenv("AXIOM_SANDBOX_QUALIFICATION_RUN_ID")
	commitSHA := os.Getenv("AXIOM_SANDBOX_QUALIFICATION_COMMIT_SHA")
	sourceRoot := os.Getenv("AXIOM_SANDBOX_QUALIFICATION_SOURCE_ROOT")
	if runID == "" || !commitPattern.MatchString(commitSHA) ||
		!filepath.IsAbs(sourceRoot) || gate == nil {
		return controllerConfig{}, fmt.Errorf("sandbox_qualification_chaos_configuration_rejected")
	}
	canonicalRoot, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil || !filepath.IsAbs(canonicalRoot) {
		return controllerConfig{}, fmt.Errorf("sandbox_qualification_source_identity_rejected")
	}
	if err = verifySourceIdentity(ctx, canonicalRoot, commitSHA); err != nil {
		return controllerConfig{}, err
	}
	return controllerConfig{
		runID: runID, commitSHA: commitSHA, sourceRoot: canonicalRoot,
		controllerHash: controllerHash,
	}, nil
}

func recordChaos(
	ctx context.Context,
	gate gateExecutor,
	store *postgresstore.SandboxQualificationStore,
	configuration controllerConfig,
) error {
	result := gate.execute(
		ctx, configuration.sourceRoot, configuration.commitSHA,
		configuration.controllerHash,
	)
	seedBytes := sha256.Sum256(
		[]byte(
			"axiom-sandbox-qualification-chaos-v1|" + configuration.runID + "|" +
				configuration.commitSHA,
		),
	)
	seed := hex.EncodeToString(seedBytes[:])
	events, err := sandboxQualification.RunDeterministicChaos(
		ctx,
		fixedHarness{passed: result.Passed, fact: result.Fact},
		seed,
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	if err = store.AppendChaosEvents(
		ctx, configuration.runID, events,
	); err != nil {
		return err
	}
	terminal := map[string]any{
		"event_code":             "sandbox_qualification_chaos_terminal",
		"run_id":                 configuration.runID,
		"outcome":                result.Status,
		"event_count":            len(events),
		"transcript_hash":        result.TranscriptHash,
		"controller_hash":        configuration.controllerHash,
		"profitability_evidence": false,
	}
	if err = json.NewEncoder(os.Stdout).Encode(terminal); err != nil {
		return err
	}
	if !result.Passed {
		return fmt.Errorf("sandbox_qualification_chaos_gate_failed")
	}
	return nil
}

func (makeGate) execute(
	ctx context.Context,
	sourceRoot, commitSHA, controllerHash string,
) gateResult {
	command := exec.CommandContext(ctx, "make", "sandbox-chaos-qualify")
	command.Dir = sourceRoot
	command.Env = qualificationEnvironment(os.Environ())
	transcript := sha256.New()
	command.Stdout = transcript
	command.Stderr = transcript
	err := command.Run()
	status := "PASSED"
	if err != nil {
		status = "FAILED"
	}
	transcriptHash := digest(transcript)
	factBytes, marshalErr := json.Marshal(struct {
		Gate           string `json:"gate"`
		CommitSHA      string `json:"commit_sha"`
		ControllerHash string `json:"controller_hash"`
		GoVersion      string `json:"go_version"`
		Status         string `json:"status"`
		TranscriptHash string `json:"transcript_hash"`
	}{
		Gate: "make sandbox-chaos-qualify", CommitSHA: commitSHA,
		ControllerHash: controllerHash, GoVersion: runtime.Version(),
		Status: status, TranscriptHash: transcriptHash,
	})
	if marshalErr != nil {
		return gateResult{
			Passed: false, Status: "FAILED", TranscriptHash: transcriptHash,
			Fact: "sandbox_qualification_chaos_fact_encoding_failed",
		}
	}
	return gateResult{
		Passed: err == nil, Status: status, TranscriptHash: transcriptHash,
		Fact: string(factBytes),
	}
}

// Exercise binds one closed scenario to the single deterministic gate result.
func (harness fixedHarness) Exercise(
	_ context.Context,
	scenario, _ string,
) (bool, string, error) {
	if harness.fact == "" || !containsScenario(scenario) {
		return false, "", fmt.Errorf("sandbox_qualification_chaos_fact_rejected")
	}
	return harness.passed, harness.fact + "|scenario=" + scenario, nil
}

func verifySourceIdentity(
	ctx context.Context,
	sourceRoot, expectedCommit string,
) error {
	head, err := gitOutput(ctx, sourceRoot, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != expectedCommit {
		return fmt.Errorf("sandbox_qualification_source_commit_mismatch")
	}
	status, err := gitOutput(
		ctx, sourceRoot, "status", "--porcelain", "--untracked-files=all",
	)
	if err != nil || strings.TrimSpace(status) != "" {
		return fmt.Errorf("sandbox_qualification_source_dirty")
	}
	return nil
}

func gitOutput(
	ctx context.Context,
	root string,
	arguments ...string,
) (string, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = root
	command.Env = qualificationEnvironment(os.Environ())
	output, err := command.Output()
	if len(output) > 1<<20 {
		return "", fmt.Errorf("sandbox_qualification_source_identity_output_rejected")
	}
	return string(output), err
}

func qualificationEnvironment(environ []string) []string {
	allowed := map[string]bool{
		"CC": true, "CGO_ENABLED": true, "CXX": true,
		"GOFLAGS": true, "GOCACHE": true, "GOMODCACHE": true,
		"GOPATH": true, "GOTOOLCHAIN": true, "HOME": true,
		"PATH": true, "TMPDIR": true,
	}
	result := make([]string, 0, len(allowed)+2)
	for _, entry := range environ {
		key, _, found := strings.Cut(entry, "=")
		if found && allowed[key] {
			result = append(result, entry)
		}
	}
	return append(
		result,
		"AXIOM_SANDBOX_RUNTIME_TEST_DSN=",
		"AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN=",
	)
}

func containsScenario(candidate string) bool {
	for _, scenario := range sandboxQualification.RequiredChaosScenarios {
		if scenario == candidate {
			return true
		}
	}
	return false
}

func digest(value hash.Hash) string {
	return hex.EncodeToString(value.Sum(nil))
}
