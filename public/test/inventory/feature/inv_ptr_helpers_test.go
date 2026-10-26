package feature

import (
	"strings"
	"testing"
)

// inventoryInternalTokens 库存侧页面对外的黑名单：驱动文案、SQLSTATE、表名与约束名前缀。
//
// 对运营没有意义、对攻击者是有用的地图 —— 原文只进结构化日志，页面上一律归口文案。
// （采购侧的同类表在 public/test/purchase/feature 里，那份多列了四张采购表。）
var inventoryInternalTokens = []string{
	"SQLSTATE", "42P01", "23505", "does not exist",
	"inventory_stocks", "inventory_warehouses", "inventory_stock_movements",
	"uq_", "fk_", "pg_",
}

// assertNoInternalTokens 断言给定文本里没有任何内部实现细节。
func assertNoInternalTokens(t *testing.T, where, text string) {
	t.Helper()
	for _, token := range inventoryInternalTokens {
		if strings.Contains(text, token) {
			t.Fatalf("%s 泄漏了内部实现细节 %q", where, token)
		}
	}
}

// floatPtr 取 float64 地址（金额 / 成本入参用指针表达「未填」）。
func floatPtr(v float64) *float64 { return &v }
