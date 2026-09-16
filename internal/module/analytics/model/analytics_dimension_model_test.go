package analyticsmodel

// analytics_dimension_model_test.go — 维度白名单的两个不变量（纯逻辑，不触库）。

import "testing"

// TestDimensionColumnsCoverAllDimensions 每个维度常量都要有列映射，
// 否则 service 传进去的合法维度会变成「未知维度」错误。
func TestDimensionColumnsCoverAllDimensions(t *testing.T) {
	for _, dim := range []string{DimensionReferrer, DimensionUA, DimensionLang} {
		if dimensionColumns[dim] == "" {
			t.Errorf("维度 %q 没有列映射", dim)
		}
	}
}

// TestDimensionColumnsExcludeHashedIdentity 哈希列不得成为排行维度。
//
// 这是 BIZ-8 隐私边界在代码里的落点：session_id / visitor_hash / ip_hash 只参与去重计数，
// 把其中任何一个做成排行维度展示，等于给出「同一个人还去过哪些页面」的入口。
// 断言写在映射本身上，而不是靠「没人会这么加」的默契。
func TestDimensionColumnsExcludeHashedIdentity(t *testing.T) {
	forbidden := map[string]bool{"session_id": true, "visitor_hash": true, "ip_hash": true}
	for dim, column := range dimensionColumns {
		if forbidden[column] {
			t.Errorf("维度 %q 映射到了哈希列 %q：它只用于去重计数，不能作为排行维度", dim, column)
		}
	}
}
