package feature

import (
	"strings"
	"testing"
)

var inventoryInternalTokens = []string{
	"SQLSTATE", "42P01", "23505", "does not exist",
	"inventory_stocks", "inventory_warehouses", "inventory_stock_movements",
	"uq_", "fk_", "pg_",
}

func assertNoInternalTokens(t *testing.T, where, text string) {
	t.Helper()
	for _, token := range inventoryInternalTokens {
		if strings.Contains(text, token) {
			t.Fatalf("%s 泄漏了内部实现细节 %q", where, token)
		}
	}
}

func floatPtr(v float64) *float64 { return &v }
