package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/qualification/operationalreadiness"
	"axiom/internal/security"
)

type runFile struct {
	Identity              operationalReadiness.Identity     `json:"identity"`
	DurationSeconds       uint64                            `json:"duration_seconds"`
	SampleIntervalSeconds uint64                            `json:"sample_interval_seconds"`
	EvidenceRoot          string                            `json:"evidence_root"`
	DeclaredLoad          operationalReadiness.DeclaredLoad `json:"declared_load"`
}

type testManifest struct {
	SchemaVersion              string   `json:"schema_version"`
	DurationSeconds            uint64   `json:"duration_seconds"`
	SampleIntervalSeconds      uint64   `json:"sample_interval_seconds"`
	ClockOffsetThresholdMillis uint64   `json:"clock_offset_threshold_ms"`
	FaultScheduleSHA256        string   `json:"fault_schedule_sha256"`
	DeclaredLoad               []string `json:"declared_load"`
	ZeroTolerance              []string `json:"zero_tolerance"`
	IndependentVerdicts        []string `json:"independent_verdicts_not_replaced"`
}

func main() {
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "operational_readiness:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if os.Getenv("AXIOM_OPERATIONAL_READINESS_ENABLED") != "1" {
		return fmt.Errorf("readiness runner is default-off")
	}
	configuration, err := loadConfiguration()
	if err != nil {
		return err
	}
	preflightSource := operationalReadiness.FilePreflight{Path: os.Getenv("AXIOM_OPERATIONAL_READINESS_PREFLIGHT_FILE")}
	probe := &operationalReadiness.FileProbe{Path: os.Getenv("AXIOM_OPERATIONAL_READINESS_SAMPLE_FILE")}
	preflightCheck, err := preflightCheckEnabled()
	if err != nil {
		return err
	}
	if preflightCheck {
		return runPreflightCheck(ctx, configuration.Identity.Mode, preflightSource, probe, operationalReadiness.RealClock{})
	}
	store := &operationalReadiness.FileStore{Root: configuration.EvidenceRoot}
	evidence, runErr := (operationalReadiness.Runner{Clock: operationalReadiness.RealClock{},
		Preflight: preflightSource,
		Probe:     probe,
		Faults:    operationalReadiness.FileFaultSource{Path: os.Getenv("AXIOM_OPERATIONAL_READINESS_FAULT_EVIDENCE_FILE")},
		Store:     store}).Run(ctx, configuration)
	if evidence.EvidenceHash != "" {
		_, _ = fmt.Fprintf(os.Stdout, "run_id=%s state=%s qualified=%t evidence_hash=%s\n",
			evidence.Identity.RunID, evidence.State, evidence.Qualified, evidence.EvidenceHash)
	}
	return runErr
}

func preflightCheckEnabled() (bool, error) {
	switch os.Getenv("AXIOM_OPERATIONAL_READINESS_PREFLIGHT_CHECK") {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("preflight check mode invalid")
	}
}

func runPreflightCheck(
	ctx context.Context,
	mode operationalReadiness.Mode,
	preflightSource operationalReadiness.PreflightChecker,
	probe operationalReadiness.Probe,
	clock operationalReadiness.Clock,
) error {
	checkedAt := clock.Now()
	preflight, preflightErr := preflightSource.Check(ctx)
	sample, sampleErr := probe.Observe(ctx, 1, checkedAt)
	var preflightInput *operationalReadiness.Preflight
	var sampleInput *operationalReadiness.Sample
	if preflightErr == nil {
		preflightInput = &preflight
	}
	if sampleErr == nil {
		sampleInput = &sample
	}
	report := operationalReadiness.CheckPreflightSources(preflightInput, sampleInput, mode, checkedAt)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("preflight report write failed")
	}
	if !report.Ready {
		return fmt.Errorf("operational_readiness_preflight_check_failed")
	}
	return nil
}

func loadConfiguration() (operationalReadiness.Config, error) {
	var source runFile
	runPath := os.Getenv("AXIOM_OPERATIONAL_READINESS_RUN_FILE")
	if err := readJSON(runPath, &source); err != nil {
		return operationalReadiness.Config{}, err
	}
	if string(source.Identity.Mode) != os.Getenv("AXIOM_OPERATIONAL_READINESS_MODE") {
		return operationalReadiness.Config{}, fmt.Errorf("mode identity mismatch")
	}
	schedule, err := loadSchedule(source)
	if err != nil {
		return operationalReadiness.Config{}, err
	}
	key, err := signingKey(os.Getenv("AXIOM_OPERATIONAL_READINESS_SIGNING_KEY_FILE"))
	if err != nil {
		return operationalReadiness.Config{}, err
	}
	configuration := operationalReadiness.Config{Enabled: true, Identity: source.Identity,
		Duration:       time.Duration(source.DurationSeconds) * time.Second,
		SampleInterval: time.Duration(source.SampleIntervalSeconds) * time.Second,
		EvidenceRoot:   source.EvidenceRoot, DeclaredLoad: source.DeclaredLoad,
		FaultSchedule: schedule, SigningKey: key}
	if err = validateBuild(configuration); err != nil {
		return operationalReadiness.Config{}, err
	}
	if err = operationalReadiness.ValidateConfig(configuration); err != nil {
		return operationalReadiness.Config{}, err
	}
	return configuration, nil
}

func loadSchedule(source runFile) (operationalReadiness.FaultSchedule, error) {
	manifestPath := os.Getenv("AXIOM_OPERATIONAL_READINESS_TEST_MANIFEST_FILE")
	manifestHash, err := fileHash(manifestPath)
	if err != nil || manifestHash != source.Identity.TestManifestHash {
		return operationalReadiness.FaultSchedule{}, fmt.Errorf("test manifest identity mismatch")
	}
	var manifest testManifest
	if err = readJSON(manifestPath, &manifest); err != nil || validateTestManifest(manifest, source) != nil {
		return operationalReadiness.FaultSchedule{}, fmt.Errorf("test manifest contract mismatch")
	}
	schedulePath := os.Getenv("AXIOM_OPERATIONAL_READINESS_FAULT_SCHEDULE_FILE")
	scheduleHash, err := fileHash(schedulePath)
	if err != nil || scheduleHash != manifest.FaultScheduleSHA256 {
		return operationalReadiness.FaultSchedule{}, fmt.Errorf("fault schedule identity mismatch")
	}
	var schedule operationalReadiness.FaultSchedule
	if err = readJSON(schedulePath, &schedule); err != nil {
		return operationalReadiness.FaultSchedule{}, err
	}
	return schedule, nil
}

func validateBuild(configuration operationalReadiness.Config) error {
	if configuration.Identity.Mode == operationalReadiness.ModeFormal {
		info := buildinfo.Current()
		if info.Commit != configuration.Identity.SourceSHA || info.Dirty {
			return fmt.Errorf("formal build identity mismatch")
		}
	}
	return nil
}

func validateTestManifest(manifest testManifest, source runFile) error {
	if manifest.SchemaVersion != "axiom.operational_readiness.test-manifest.v1" ||
		manifest.DurationSeconds != source.DurationSeconds ||
		manifest.SampleIntervalSeconds != source.SampleIntervalSeconds ||
		manifest.ClockOffsetThresholdMillis != operationalReadiness.ClockThresholdMillis ||
		!exactStringSet(manifest.DeclaredLoad, []string{
			"collectors_and_recording", "coherent_sampling", "strategies_allocator_risk_accounting",
			"shadow_and_virtual_execution", "api_sse_and_ui", "lab_jobs",
			"reports_exports_and_alerts", "encrypted_backup_and_clean_restore", "resource_limits",
		}) || !exactStringSet(manifest.ZeroTolerance, []string{
		"stale_decision", "uninvalidated_gap", "duplicate_order", "lost_fill",
		"double_posted_fill", "unbalanced_journal", "replay_mismatch",
		"production_private_submission", "prohibited_capability",
	}) || !exactStringSet(manifest.IndependentVerdicts, []string{
		"coherent market-data qualification", "sandbox order and reconciliation qualification",
	}) {
		return fmt.Errorf("test_manifest_invalid")
	}
	return nil
}

func exactStringSet(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	wanted := make(map[string]struct{}, len(expected))
	for _, value := range expected {
		wanted[value] = struct{}{}
	}
	for _, value := range actual {
		if _, found := wanted[value]; !found {
			return false
		}
		delete(wanted, value)
	}
	return len(wanted) == 0
}

func readJSON(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("readiness input unavailable")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 10<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		return fmt.Errorf("readiness input invalid")
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("readiness input trailing data")
	}
	return nil
}

func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, io.LimitReader(file, 10<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func signingKey(path string) (ed25519.PrivateKey, error) {
	value, err := security.ReadSecretFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("operational_readiness signing key invalid")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
