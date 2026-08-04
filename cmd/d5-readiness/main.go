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
	"axiom/internal/qualification/d5"
	"axiom/internal/security"
)

type runFile struct {
	Identity              d5.Identity     `json:"identity"`
	DurationSeconds       uint64          `json:"duration_seconds"`
	SampleIntervalSeconds uint64          `json:"sample_interval_seconds"`
	EvidenceRoot          string          `json:"evidence_root"`
	DeclaredLoad          d5.DeclaredLoad `json:"declared_load"`
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
		_, _ = fmt.Fprintln(os.Stderr, "d5-readiness:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if os.Getenv("AXIOM_D5_READINESS_ENABLED") != "1" {
		return fmt.Errorf("readiness runner is default-off")
	}
	configuration, err := loadConfiguration()
	if err != nil {
		return err
	}
	store := &d5.FileStore{Root: configuration.EvidenceRoot}
	evidence, runErr := (d5.Runner{Clock: d5.RealClock{},
		Preflight: d5.FilePreflight{Path: os.Getenv("AXIOM_D5_PREFLIGHT_FILE")},
		Probe:     &d5.FileProbe{Path: os.Getenv("AXIOM_D5_SAMPLE_FILE")},
		Faults:    d5.FileFaultSource{Path: os.Getenv("AXIOM_D5_FAULT_EVIDENCE_FILE")},
		Store:     store}).Run(ctx, configuration)
	if evidence.EvidenceHash != "" {
		_, _ = fmt.Fprintf(os.Stdout, "run_id=%s state=%s qualified=%t evidence_hash=%s\n",
			evidence.Identity.RunID, evidence.State, evidence.Qualified, evidence.EvidenceHash)
	}
	return runErr
}

func loadConfiguration() (d5.Config, error) {
	var source runFile
	runPath := os.Getenv("AXIOM_D5_RUN_FILE")
	if err := readJSON(runPath, &source); err != nil {
		return d5.Config{}, err
	}
	if string(source.Identity.Mode) != os.Getenv("AXIOM_D5_MODE") {
		return d5.Config{}, fmt.Errorf("mode identity mismatch")
	}
	schedule, err := loadSchedule(source)
	if err != nil {
		return d5.Config{}, err
	}
	key, err := signingKey(os.Getenv("AXIOM_D5_SIGNING_KEY_FILE"))
	if err != nil {
		return d5.Config{}, err
	}
	configuration := d5.Config{Enabled: true, Identity: source.Identity,
		Duration:       time.Duration(source.DurationSeconds) * time.Second,
		SampleInterval: time.Duration(source.SampleIntervalSeconds) * time.Second,
		EvidenceRoot:   source.EvidenceRoot, DeclaredLoad: source.DeclaredLoad,
		FaultSchedule: schedule, SigningKey: key}
	if err = validateBuild(configuration); err != nil {
		return d5.Config{}, err
	}
	if err = d5.ValidateConfig(configuration); err != nil {
		return d5.Config{}, err
	}
	return configuration, nil
}

func loadSchedule(source runFile) (d5.FaultSchedule, error) {
	manifestPath := os.Getenv("AXIOM_D5_TEST_MANIFEST_FILE")
	manifestHash, err := fileHash(manifestPath)
	if err != nil || manifestHash != source.Identity.TestManifestHash {
		return d5.FaultSchedule{}, fmt.Errorf("test manifest identity mismatch")
	}
	var manifest testManifest
	if err = readJSON(manifestPath, &manifest); err != nil || validateTestManifest(manifest, source) != nil {
		return d5.FaultSchedule{}, fmt.Errorf("test manifest contract mismatch")
	}
	schedulePath := os.Getenv("AXIOM_D5_FAULT_SCHEDULE_FILE")
	scheduleHash, err := fileHash(schedulePath)
	if err != nil || scheduleHash != manifest.FaultScheduleSHA256 {
		return d5.FaultSchedule{}, fmt.Errorf("fault schedule identity mismatch")
	}
	var schedule d5.FaultSchedule
	if err = readJSON(schedulePath, &schedule); err != nil {
		return d5.FaultSchedule{}, err
	}
	return schedule, nil
}

func validateBuild(configuration d5.Config) error {
	if configuration.Identity.Mode == d5.ModeFormal {
		info := buildinfo.Current()
		if info.Commit != configuration.Identity.SourceSHA || info.Dirty {
			return fmt.Errorf("formal build identity mismatch")
		}
	}
	return nil
}

func validateTestManifest(manifest testManifest, source runFile) error {
	if manifest.SchemaVersion != "axiom.d5.test-manifest.v1" ||
		manifest.DurationSeconds != source.DurationSeconds ||
		manifest.SampleIntervalSeconds != source.SampleIntervalSeconds ||
		manifest.ClockOffsetThresholdMillis != d5.ClockThresholdMillis ||
		!exactStringSet(manifest.DeclaredLoad, []string{
			"collectors_and_recording", "coherent_sampling", "strategies_allocator_risk_accounting",
			"shadow_and_virtual_execution", "api_sse_and_ui", "lab_jobs",
			"reports_exports_and_alerts", "encrypted_backup_and_clean_restore", "resource_limits",
		}) || !exactStringSet(manifest.ZeroTolerance, []string{
		"stale_decision", "uninvalidated_gap", "duplicate_order", "lost_fill",
		"double_posted_fill", "unbalanced_journal", "replay_mismatch",
		"production_private_submission", "prohibited_capability",
	}) || !exactStringSet(manifest.IndependentVerdicts, []string{
		"B2 market-data qualification", "C6 sandbox order and reconciliation qualification",
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
		return nil, fmt.Errorf("d5 signing key invalid")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
