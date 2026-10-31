package addtocart

import "testing"

func TestBuildViewCarriesLang(t *testing.T) {
	v, err := BuildView(baseProps(), baseContent(), "proj-1", "en-US")
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if v.Lang != "en-US" {
		t.Fatalf("视图应带上构建语言，实际 %q", v.Lang)
	}
}

func TestBuildViewEmptyLangStaysEmpty(t *testing.T) {
	v, err := BuildView(baseProps(), baseContent(), "proj-1", "  ")
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if v.Lang != "" {
		t.Fatalf("空白语言应归一成空串，实际 %q", v.Lang)
	}
}
