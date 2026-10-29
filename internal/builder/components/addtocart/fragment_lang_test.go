package addtocart

// 加购表单保留构建语言，避免组件数据读写与页面语言脱节。

import "testing"

// TestBuildViewCarriesLang 构建语言进入视图（模板据此输出 hidden 域）。
func TestBuildViewCarriesLang(t *testing.T) {
	v, err := BuildView(baseProps(), baseContent(), "proj-1", "en-US")
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if v.Lang != "en-US" {
		t.Fatalf("视图应带上构建语言，实际 %q", v.Lang)
	}
}

// TestBuildViewEmptyLangStaysEmpty 空语言保持空（单语言站点不输出多一个域）。
func TestBuildViewEmptyLangStaysEmpty(t *testing.T) {
	v, err := BuildView(baseProps(), baseContent(), "proj-1", "  ")
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if v.Lang != "" {
		t.Fatalf("空白语言应归一成空串，实际 %q", v.Lang)
	}
}
