package postgres

import (
	"errors"
	"strings"
	"testing"

	"axiom/internal/api/console"
	"axiom/internal/authentication"
)

func TestOwnerControlStorageRevalidatesHighRiskAuthorizationBinding(t *testing.T) {
	revision := int64(4)
	reason := "hold exact owner control incident artifact"
	principal := authentication.Principal{UserID: "owner-owner_control"}
	command := console.OwnerControlCommand{
		Kind: "artifact_hold", ExpectedRevision: revision, Reason: reason,
		Authorization: &authentication.ConsumedAuthorization{
			UserID: principal.UserID, Purpose: authentication.PurposeArtifactHold,
			ReasonHash:     authentication.AuthorizationBindingHash(reason),
			TargetRevision: &revision,
		},
	}
	if err := validateOwnerControlAuthorization(principal, command); err != nil {
		t.Fatalf("valid owner control high-risk binding rejected: %v", err)
	}
	for name, mutate := range map[string]func(*authentication.ConsumedAuthorization){
		"actor":    func(value *authentication.ConsumedAuthorization) { value.UserID = "other" },
		"purpose":  func(value *authentication.ConsumedAuthorization) { value.Purpose = authentication.PurposeRiskControl },
		"reason":   func(value *authentication.ConsumedAuthorization) { value.ReasonHash = strings.Repeat("0", 64) },
		"revision": func(value *authentication.ConsumedAuthorization) { other := int64(5); value.TargetRevision = &other },
	} {
		t.Run(name, func(t *testing.T) {
			copy := *command.Authorization
			mutate(&copy)
			candidate := command
			candidate.Authorization = &copy
			if err := validateOwnerControlAuthorization(principal, candidate); !errors.Is(err, console.ErrPrecondition) {
				t.Fatalf("invalid %s binding error=%v", name, err)
			}
		})
	}
}

func TestOwnerControlExportFormatsAreDeterministicAndSpreadsheetSafe(t *testing.T) {
	record := map[string]string{"alpha": "=unsafe", "beta": "line\nbreak"}
	for _, format := range []string{"txt", "csv", "json", "jsonl"} {
		first, contentType, err := encodeOwnerControlExport(record, format)
		if err != nil || first == "" || contentType == "" {
			t.Fatalf("owner control %s export content=%q type=%q error=%v", format, first, contentType, err)
		}
		second, _, err := encodeOwnerControlExport(record, format)
		if err != nil || second != first {
			t.Fatalf("owner control %s export is nondeterministic", format)
		}
		if format == "csv" && !strings.Contains(first, "'=unsafe") {
			t.Fatalf("owner control CSV formula was not neutralized: %q", first)
		}
	}
}
