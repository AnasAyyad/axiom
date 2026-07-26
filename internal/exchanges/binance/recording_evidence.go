package binance

import (
	"encoding/json"
	"errors"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

type decoderFailureEvidence struct {
	Kind        string                       `json:"kind"`
	Stage       string                       `json:"decoder_stage"`
	FailureKind exchangecontracts.ErrorKind  `json:"failure_kind"`
	Operation   exchangecontracts.Operation  `json:"operation"`
	StreamKind  exchangecontracts.StreamKind `json:"stream_kind,omitempty"`
	Cause       string                       `json:"cause"`
}

func boundedDecoderFailureEvidence(
	err error,
	stage string,
	streamKind exchangecontracts.StreamKind,
) []byte {
	evidence := decoderFailureEvidence{Kind: "decoder_error", Stage: stage,
		FailureKind: exchangecontracts.ErrorValidation,
		Operation:   exchangecontracts.OperationStream,
		StreamKind:  streamKind, Cause: "decoder_validation"}
	var failure *exchangecontracts.Error
	if errors.As(err, &failure) && failure != nil {
		evidence.FailureKind = failure.Kind
		evidence.Operation = failure.Operation
		if failure.Cause != "" {
			evidence.Cause = failure.Cause
		}
	}
	payload, marshalErr := json.Marshal(evidence)
	if marshalErr != nil {
		return []byte(`{"kind":"decoder_error","decoder_stage":"evidence_encode","failure_kind":"validation_rejected","operation":"stream","cause":"decoder_validation"}`)
	}
	return payload
}
