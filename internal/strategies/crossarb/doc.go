// Package crossarb implements the multi-strategy research cross-exchange arbitrage public-data-only, concurrent,
// inventory-backed cross-exchange arbitrage simulation boundary.
//
// It cannot submit authenticated or production orders. Every candidate is
// priced from one coherent market data coherent as-of view, requires owned inventory on both
// venues, and reports profit only after the complete inventory-restoration
// cycle is charged.
package crossarb
