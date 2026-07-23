// Package crossarb implements the V1B B5 public-data-only, concurrent,
// inventory-backed cross-exchange arbitrage simulation boundary.
//
// It cannot submit authenticated or production orders. Every candidate is
// priced from one B2 coherent as-of view, requires owned inventory on both
// venues, and reports profit only after the complete inventory-restoration
// cycle is charged.
package crossarb
