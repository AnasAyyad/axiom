package postgres

import (
	"testing"

	"axiom/internal/api/generated"
)

func TestOwnerRunOutputKindDoesNotExposeStorageProjectionNames(t *testing.T) {
	stored, exposed, ok := ownerRunOutputKind("projection")
	if !ok || stored != "projection" || exposed != "execution" {
		t.Fatalf("execution mapping = %q/%q/%t", stored, exposed, ok)
	}
	if _, _, ok = ownerRunOutputKind("balance"); ok {
		t.Fatal("internal balance output became a generic browser collection")
	}
}

func TestProjectedRunOutputHashesExactCanonicalPayload(t *testing.T) {
	first, err := projectedRunOutput(2, generated.Decision, `{"state":"approved","value":"10.00"}`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := projectedRunOutput(2, generated.Decision, `{"state":"approved","value":"10.00"}`)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.ContentHash != "bceab0f717a9a8bf79195788dfa06b4570d4e2bbe9beb62303bb9b9edf695f34" {
		t.Fatalf("projected output is not deterministic: %#v %#v", first, second)
	}
	if _, err = projectedRunOutput(0, generated.Decision, `{}`); err == nil {
		t.Fatal("zero ordinal accepted")
	}
	if _, err = projectedRunOutput(1, generated.Decision, `{`); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestOwnerRunCanonicalPayloadIsBoundedJSON(t *testing.T) {
	if !ownerRunCanonicalPayloadValid(`[]`) || ownerRunCanonicalPayloadValid(`not-json`) || ownerRunCanonicalPayloadValid(``) {
		t.Fatal("canonical payload boundary was not enforced")
	}
}
