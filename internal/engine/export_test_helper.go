package engine

import (
	"fossilcourt/internal/exchange"
	"fossilcourt/internal/store"
)

func ExportForTest(db *store.DB) (*exchange.Bundle, error) { return exchange.Export(db) }
func ImportForTest(db *store.DB, b *exchange.Bundle) error { return exchange.Import(db, b) }
