package bybit

import exchangecontracts "axiom/internal/exchanges/contracts"

func (collector *InstrumentCollector) emitLifecycleEvidence(diagnostic ReconnectDiagnostic) {
	if collector.evidenceSink == nil {
		return
	}
	event := exchangecontracts.CollectorLifecycleEvidence{
		ObservedAt: diagnostic.ObservedAt, Exchange: collectorExchange,
		Instrument: diagnostic.Instrument, Generation: diagnostic.Generation,
		Cycle: diagnostic.Cycle, Attempt: diagnostic.Attempt, Phase: diagnostic.Phase,
		Stage: diagnostic.Stage, Reason: diagnostic.Reason, Action: diagnostic.Action,
		Cause: diagnostic.Cause, Attribution: exchangecontracts.FailureAttribution(diagnostic.Attribution),
		FailureKind: diagnostic.FailureKind, Operation: diagnostic.Operation,
		RetryAfter: diagnostic.RetryAfter, HTTPStatus: diagnostic.HTTPStatus,
		Transport: exchangecontracts.FailureMetadata{
			RequestDuration:        diagnostic.RequestDuration,
			ResponseHeaderDuration: diagnostic.HeaderDuration,
			ResponseBodyDuration:   diagnostic.BodyDuration,
			ResponseBytes:          diagnostic.ResponseBytes, ContentLengthBytes: diagnostic.ContentLength,
			ContentLengthKnown: diagnostic.ContentKnown, BodyLimitBytes: diagnostic.BodyLimit,
			DNSDuration: diagnostic.DNSDuration, TCPDuration: diagnostic.TCPDuration,
			TLSDuration: diagnostic.TLSDuration, UpgradeDuration: diagnostic.UpgradeDuration,
			WriteDuration: diagnostic.WriteDuration, CandidateCount: diagnostic.CandidateCount,
			AttemptCount: diagnostic.TransportAttempts, AddressFamily: diagnostic.AddressFamily,
			SetupStage: diagnostic.SetupStage},
		ClockUncertainty: diagnostic.ClockUncertainty,
		AttemptDuration:  diagnostic.AttemptDuration, Backoff: diagnostic.Backoff,
		ResyncElapsed: diagnostic.ResyncElapsed, ReachedHealthy: diagnostic.ReachedHealthy}
	if err := collector.evidenceSink.AppendCollectorLifecycle(event); err != nil {
		collector.evidenceOnce.Do(func() {
			collector.evidenceMutex.Lock()
			collector.evidenceErr = streamError()
			collector.evidenceMutex.Unlock()
			close(collector.evidenceFailed)
		})
	}
}

func (collector *InstrumentCollector) lifecycleEvidenceError() error {
	select {
	case <-collector.evidenceFailed:
		collector.evidenceMutex.Lock()
		defer collector.evidenceMutex.Unlock()
		return collector.evidenceErr
	default:
		return nil
	}
}
