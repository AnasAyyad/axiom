package postgres

import (
	"testing"
	"time"

	"axiom/internal/sandbox"
)

func TestNormalizeSandboxRuntimeArmForPersistenceMatchesPostgresPrecision(t *testing.T) {
	createdAt := time.Date(
		2026, 7, 29, 8, 13, 12, 84_893_721, time.UTC,
	)
	arm := sandbox.Arm{
		ID:        "arm-1",
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(sandbox.ArmLifetime),
	}

	normalized := normalizeSandboxRuntimeArmForPersistence(arm)

	if normalized.CreatedAt.Nanosecond() != 84_893_000 {
		t.Fatalf("created_at=%s", normalized.CreatedAt.Format(time.RFC3339Nano))
	}
	if normalized.ExpiresAt.Nanosecond() != 84_893_000 {
		t.Fatalf("expires_at=%s", normalized.ExpiresAt.Format(time.RFC3339Nano))
	}
	if normalized.ExpiresAt.Sub(normalized.CreatedAt) != sandbox.ArmLifetime {
		t.Fatal("normalization changed the arm lifetime")
	}
	if normalized.ID != arm.ID {
		t.Fatal("normalization changed non-timestamp arm state")
	}
}
