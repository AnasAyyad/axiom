package postgres

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

// RecordAuthenticatedRequest durably appends one redacted request record
// before the authenticated client is allowed to perform network I/O.
func (store *V1CDispatcherStore) RecordAuthenticatedRequest(
	ctx context.Context,
	record exchangecontracts.AuthenticatedRequestEvidence,
) error {
	if err := exchangecontracts.ValidateAuthenticatedRequestEvidence(record); err != nil {
		return err
	}
	enumerated, err := json.Marshal(record.Enumerated)
	if err != nil {
		return fmt.Errorf("v1c_authenticated_evidence_encode_failed")
	}
	if _, err = store.pool.Exec(ctx, `
INSERT INTO v1c_authenticated_request_evidence(
  exchange,host,method,path,field_names,enumerated_fields,
  request_hash,configuration_id,recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		record.Exchange,
		record.Host,
		record.Method,
		record.Path,
		record.FieldNames,
		string(enumerated),
		hex.EncodeToString(record.RequestHash[:]),
		record.ConfigurationID,
		record.RecordedAt,
	); err != nil {
		return fmt.Errorf("v1c_authenticated_evidence_insert_failed")
	}
	return nil
}

var _ exchangecontracts.AuthenticatedEvidenceSink = (*V1CDispatcherStore)(nil)
