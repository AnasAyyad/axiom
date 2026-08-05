package postgres

import "testing"

func TestOwnerRunOutputKindDoesNotExposeStorageProjectionNames(t *testing.T) {
	stored, exposed, ok := ownerRunOutputKind("projection")
	if !ok || stored != "projection" || exposed != "execution" {
		t.Fatalf("execution mapping = %q/%q/%t", stored, exposed, ok)
	}
	if _, _, ok = ownerRunOutputKind("balance"); ok {
		t.Fatal("internal balance output became a generic browser collection")
	}
}

func TestOwnerRunCanonicalPayloadIsBoundedJSON(t *testing.T) {
	if !ownerRunCanonicalPayloadValid(`[]`) || ownerRunCanonicalPayloadValid(`not-json`) || ownerRunCanonicalPayloadValid(``) {
		t.Fatal("canonical payload boundary was not enforced")
	}
}
