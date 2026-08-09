package postgres

import (
	"strings"
	"testing"

	"axiom/internal/api/generated"
)

func TestVerifyOperationalEvidenceAuditLinksAcceptsIdentityGapsFromRolledBackTransactions(t *testing.T) {
	first, second := strings.Repeat("a", 64), strings.Repeat("b", 64)
	result := verifyOperationalEvidenceAuditLinks([]operationalEvidenceAuditLink{
		{sequence: 1, stored: first, authoritativeHash: first},
		{sequence: 4, previous: first, stored: second, authoritativeHash: second},
	})
	if result.Verdict != generated.Valid || result.CheckedEvents != 2 || result.HeadHash != second {
		t.Fatalf("verification=%+v", result)
	}
}

func TestVerifyOperationalEvidenceAuditLinksReportsBrokenPreviousHash(t *testing.T) {
	first, second := strings.Repeat("a", 64), strings.Repeat("b", 64)
	result := verifyOperationalEvidenceAuditLinks([]operationalEvidenceAuditLink{
		{sequence: 1, stored: first, authoritativeHash: first},
		{sequence: 2, previous: strings.Repeat("c", 64), stored: second, authoritativeHash: second},
	})
	if result.Verdict != generated.Broken || result.FirstBrokenSequence == nil ||
		*result.FirstBrokenSequence != 2 || result.ReasonCode == nil {
		t.Fatalf("verification=%+v", result)
	}
}
