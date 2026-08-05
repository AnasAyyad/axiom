// Package runs defines the semantic, server-authoritative run catalogue.
//
// It is deliberately independent of exchange adapters and the browser. A
// selection is admitted only after this catalogue has established that its
// strategy, mode, venue, instrument, and input source are compatible. The
// eventual evaluator still must use allocation, risk, execution, accounting,
// and reconciliation through the shared pipeline.
package runs
