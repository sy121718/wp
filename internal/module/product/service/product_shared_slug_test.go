package productservice

import "testing"

func TestNormalizeSlugRejectsStructureBreakingInput(t *testing.T) {
	for _, bad := range []string{
		"A&B",
		"a/b",
		"a?b",
		"a#b",
		"a=b",
		"a%20b",
		"a.b",
		"-abc",
		"abc-",
		"  -  ",
	} {
		if got := normalizeSlug(bad); got != "" {
			t.Errorf("normalizeSlug(%q) = %q，期望空串（由调用方回退到派生）", bad, got)
		}
	}
}

func TestNormalizeSlugKeepsLegalInput(t *testing.T) {
	cases := map[string]string{
		"Nike":          "nike",
		"  Sony  ":      "sony",
		"New Balance":   "new-balance",
		"iPhone-15-Pro": "iphone-15-pro",
		"a_b":           "a_b",
	}
	for in, want := range cases {
		if got := normalizeSlug(in); got != want {
			t.Errorf("normalizeSlug(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestNormalizeSlugAllowsCJK(t *testing.T) {
	for _, in := range []string{"山野", "小米-14", "品牌A"} {
		if got := normalizeSlug(in); got == "" {
			t.Errorf("normalizeSlug(%q) 返回空串，中文应放行", in)
		}
	}
}

func TestDeriveSlugIsStrict(t *testing.T) {
	cases := map[string]string{
		"Nike":          "nike",
		"New Balance":   "new-balance",
		"小米 14":         "14",
		"山野":            "",
		"A&B":           "a-b",
		"iPhone 15 Pro": "iphone-15-pro",
	}
	for in, want := range cases {
		if got := deriveSlug(in); got != want {
			t.Errorf("deriveSlug(%q) = %q，期望 %q", in, got, want)
		}
	}
}
