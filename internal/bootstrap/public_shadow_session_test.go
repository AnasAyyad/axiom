package bootstrap

import (
	"strings"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestPublicShadowPublicDataErrorIsBoundedAndDiagnostic(t *testing.T) {
	err := exchangecontracts.NewDetailedError(
		exchangecontracts.ErrorTransient,
		exchangecontracts.OperationMetadata,
		0,
		503,
		"http_server_error",
		exchangecontracts.FailureMetadata{RequestDuration: time.Second, SetupStage: "headers"},
	)
	got := publicShadowPublicDataError("shadow_metadata_unavailable", err).Error()
	want := "shadow_metadata_unavailable kind=transient_outage cause=http_server_error status=503 stage=headers"
	if got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
	if strings.Contains(got, "https://") || strings.Contains(got, "Authorization") {
		t.Fatalf("diagnostic exposed forbidden data: %q", got)
	}
}
