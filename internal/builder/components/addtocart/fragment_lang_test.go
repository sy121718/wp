package addtocart

// fragment_lang_test.go — 加购表单必须把语言带进 POST（I18N-011）。
//
// 加购是 POST，而片段端点收集参数时**只读 PostForm**（runtimefragment.
// collectFragmentParams）：query 串会被整个忽略，所以语言只能走表单 hidden 域 ——
// 不能像 GET 片段那样拼在 action 的查询串里。
//
// 不带语言的表现是：片段恒回落工程默认语言，英文站加购一次，购物车面板换成中文。

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
