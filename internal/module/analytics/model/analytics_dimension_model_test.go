package analyticsmodel

import "testing"

func TestDimensionColumnsCoverAllDimensions(t *testing.T) {
	for _, dim := range []string{DimensionReferrer, DimensionUA, DimensionLang} {
		if dimensionColumns[dim] == "" {
			t.Errorf("维度 %q 没有列映射", dim)
		}
	}
}

func TestDimensionColumnsExcludeHashedIdentity(t *testing.T) {
	forbidden := map[string]bool{"session_id": true, "visitor_hash": true, "ip_hash": true}
	for dim, column := range dimensionColumns {
		if forbidden[column] {
			t.Errorf("维度 %q 映射到了哈希列 %q：它只用于去重计数，不能作为排行维度", dim, column)
		}
	}
}
