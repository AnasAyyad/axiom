package postgres

import (
	"context"
	"regexp"

	"axiom/internal/api/generated"
)

var d4SHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type d4AuditLink struct {
	sequence          int64
	previous, stored  string
	authoritativeHash string
}

// D4AuditVerification explicitly reports whether the immutable sidecar remains
// linked to every authoritative general-audit event.
func (store *A11ConsoleStore) D4AuditVerification(ctx context.Context) (generated.AuditVerification, error) {
	rows, err := store.pool.Query(ctx, `SELECT chain.chain_sequence,
coalesce(chain.previous_event_hash,''),chain.event_hash::text,audit.event_hash::text
FROM v1d_audit_chain chain JOIN audit_events audit ON audit.id=chain.audit_event_id
ORDER BY chain.chain_sequence`)
	if err != nil {
		return generated.AuditVerification{}, err
	}
	defer rows.Close()
	links := []d4AuditLink{}
	for rows.Next() {
		var link d4AuditLink
		if err = rows.Scan(&link.sequence, &link.previous, &link.stored, &link.authoritativeHash); err != nil {
			return generated.AuditVerification{}, err
		}
		links = append(links, link)
	}
	if err = rows.Err(); err != nil {
		return generated.AuditVerification{}, err
	}
	verdict := verifyD4AuditLinks(links)
	verdict.VerifiedAt = store.clock.Now().UTC
	return verdict, nil
}

func verifyD4AuditLinks(links []d4AuditLink) generated.AuditVerification {
	result := generated.AuditVerification{Verdict: generated.Valid,
		CheckedEvents: int64(len(links)), HeadHash: ""}
	prior := ""
	var priorSequence int64
	for index, link := range links {
		if link.sequence <= priorSequence || link.previous != prior || link.stored != link.authoritativeHash ||
			!d4SHA256.MatchString(link.stored) {
			reason := "audit_chain_link_invalid"
			sequence := link.sequence
			if sequence < 1 {
				sequence = int64(index + 1)
			}
			result.Verdict, result.FirstBrokenSequence, result.ReasonCode =
				generated.Broken, &sequence, &reason
			return result
		}
		prior, priorSequence, result.HeadHash = link.stored, link.sequence, link.stored
	}
	return result
}
