package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	operationalReadiness "axiom/internal/qualification/operationalreadiness"
)

func TestHealthcheckReadsFreshSampleWithoutDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.json")
	t.Setenv("AXIOM_OPERATIONAL_READINESS_SAMPLE_FILE", path)
	t.Setenv("AXIOM_OPERATIONAL_READINESS_DRILL_OBSERVATION_FILE", filepath.Join(t.TempDir(), "drill.json"))
	if err := operationalReadiness.WriteLiveSample(path, operationalReadiness.Sample{
		SourceRevision: 1,
		ObservedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"--healthcheck"}); err != nil {
		t.Fatal(err)
	}
}

func TestHealthcheckRejectsStaleSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.json")
	t.Setenv("AXIOM_OPERATIONAL_READINESS_SAMPLE_FILE", path)
	t.Setenv("AXIOM_OPERATIONAL_READINESS_DRILL_OBSERVATION_FILE", filepath.Join(t.TempDir(), "drill.json"))
	if err := operationalReadiness.WriteLiveSample(path, operationalReadiness.Sample{
		SourceRevision: 1,
		ObservedAt:     time.Now().UTC().Add(-3 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"--healthcheck"}); err == nil {
		t.Fatal("stale observer sample passed the healthcheck")
	}
}
