package main

import (
	"context"
	"strings"
	"testing"
)

func TestC6SoakIsDefaultOffAndFormalOnly(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled string
		mode    string
	}{
		{name: "disabled"},
		{name: "smoke cannot use manual formal command", enabled: "1", mode: "smoke"},
		{name: "wrong enable value", enabled: "true", mode: "formal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AXIOM_C6_SOAK_ENABLED", test.enabled)
			t.Setenv("AXIOM_C6_SOAK_MODE", test.mode)
			err := run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "default-off") {
				t.Fatalf("manual runner was not closed: %v", err)
			}
		})
	}
}
