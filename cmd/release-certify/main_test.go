package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestFinalCertificationIsDefaultOff(t *testing.T) {
	t.Setenv("AXIOM_RELEASE_CERTIFICATION_ENABLED", "")
	var output bytes.Buffer
	err := run(context.Background(), &output)
	if err == nil || !strings.Contains(err.Error(), "default-off") || output.Len() != 0 {
		t.Fatalf("default-off certification result: output=%q error=%v", output.String(), err)
	}
}
