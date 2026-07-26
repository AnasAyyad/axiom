package bybit

import (
	"encoding/json"
	"errors"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

type decoderFailureEvidence struct {
	Kind        string                      `json:"kind"`
	FailureKind exchangecontracts.ErrorKind `json:"failure_kind"`
	Operation   exchangecontracts.Operation `json:"operation"`
	Cause       string                      `json:"cause,omitempty"`
}

func boundedDecoderFailureEvidence(cause error) []byte {
	evidence := decoderFailureEvidence{Kind: "decoder_error",
		FailureKind: exchangecontracts.ErrorValidation,
		Operation:   exchangecontracts.OperationStream}
	var failure *exchangecontracts.Error
	if errors.As(cause, &failure) && failure != nil {
		evidence.FailureKind = failure.Kind
		evidence.Operation = failure.Operation
		evidence.Cause = failure.Cause
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return []byte(`{"kind":"decoder_error","failure_kind":"validation_rejected","operation":"stream"}`)
	}
	return payload
}
