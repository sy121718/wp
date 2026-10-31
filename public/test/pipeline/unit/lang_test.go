package unit

import (
	"testing"

	"go_wp/internal/pipeline"
)

func TestLangURLRuleDefaultPlain(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	cases := []struct {
		name string
		lang string
		path string
		want string
	}{
		{"默认语言首页映射 /index", "zh-CN", "/", "/index"},
		{"默认语言单段路径无前缀", "zh-CN", "/about", "/about"},
		{"默认语言多段路径无前缀", "zh-CN", "/products/phone", "/products/phone"},
		{"非默认语言首页", "en-US", "/", "/en/index"},
		{"非默认语言单段路径用短码", "en-US", "/about", "/en/about"},
		{"非默认语言多段路径用短码", "en-US", "/products/phone", "/en/products/phone"},
		{"尾斜杠规范化", "en-US", "/about/", "/en/about"},
		{"未加前导斜杠补斜杠", "ja", "about", "/ja/about"},
		{"未收录语言回退主语言子标签", "sv-SE", "/about", "/sv/about"},
		{"带脚本子标签回退主语言", "zh-Hans-CN", "/about", "/zh/about"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := rule.Path(c.lang, c.path)
			if err != nil {
				t.Fatalf("Path(%q, %q) 失败: %v", c.lang, c.path, err)
			}
			if got != c.want {
				t.Fatalf("Path(%q, %q) = %q，期望 %q", c.lang, c.path, got, c.want)
			}
		})
	}
}

func TestLangURLRuleAllPrefix(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, true, "zh-CN", nil)
	cases := []struct{ lang, path, want string }{
		{"zh-CN", "/", "/zh/index"},
		{"zh-CN", "/about", "/zh/about"},
		{"en-US", "/about", "/en/about"},
		{"en-US", "/products/phone", "/en/products/phone"},
	}
	for _, c := range cases {
		if got, err := rule.Path(c.lang, c.path); err != nil || got != c.want {
			t.Errorf("Path(%q, %q) = %q (%v)，期望 %q", c.lang, c.path, got, err, c.want)
		}
	}
}

func TestLangURLRuleOff(t *testing.T) {
	rule := pipeline.NewLangURLRule(false, false, "zh-CN", nil)
	if got, err := rule.Path("en-US", "/about"); err != nil || got != "/about" {
		t.Fatalf("off 方案 Path = %q (%v)，期望 /about", got, err)
	}
	if got, err := rule.Path("zh-CN", "/"); err != nil || got != "/" {
		t.Fatalf("off 方案根路径应保持 /，实际 %q (%v)", got, err)
	}
}

func TestLangURLRuleCodes(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", map[string]string{"zh-TW": "tw", "xx-YY": "xy"})
	cases := []struct{ lang, want string }{
		{"zh-CN", "zh"},
		{"en-US", "en"},
		{"zh-TW", "tw"},
		{"xx-YY", "xy"},
		{"sv-SE", "sv"},
		{"sw", "sw"},
	}
	for _, c := range cases {
		if got := rule.URLCode(c.lang); got != c.want {
			t.Errorf("URLCode(%q) = %q，期望 %q", c.lang, got, c.want)
		}
	}

	lower := pipeline.NewLangURLRule(true, false, "zh-CN", map[string]string{"zh-tw": "tw"})
	if got := lower.URLCode("zh-TW"); got != "tw" {
		t.Fatalf("配置键大小写不敏感匹配失败: %q", got)
	}
}

func TestLangURLRuleValidate(t *testing.T) {
	ok := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	if err := ok.Validate([]string{"zh-CN", "en-US"}); err != nil {
		t.Fatalf("zh-CN/en-US 不应冲突: %v", err)
	}

	bad := pipeline.NewLangURLRule(true, false, "en-US", nil)
	if err := bad.Validate([]string{"en-US", "zh-TW", "zh-Hant"}); err == nil {
		t.Fatal("zh-TW 与 zh-Hant 映射同一短码，应报错")
	}

	def := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	if err := def.Validate([]string{"zh-CN", "en-US"}); err != nil {
		t.Fatalf("默认语言不占短码: %v", err)
	}
}

func TestLangURLRuleStrip(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	cases := []struct{ lang, path, want string }{
		{"zh-CN", "/index", "/"},
		{"zh-CN", "/about", "/about"},
		{"en-US", "/en/index", "/"},
		{"en-US", "/en/about", "/about"},
		{"en-US", "/en/a/b", "/a/b"},
		{"en-US", "/about", "/about"},
		{"zh-CN", "/en/about", "/en/about"},
	}
	for _, c := range cases {
		if got := rule.Strip(c.lang, c.path); got != c.want {
			t.Errorf("Strip(%q, %q) = %q，期望 %q", c.lang, c.path, got, c.want)
		}
	}
}

func TestLangURLRuleLocate(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	langs := []string{"zh-CN", "en-US"}
	cases := []struct {
		path        string
		wantLang    string
		wantLogical string
		wantOK      bool
	}{
		{"/about", "zh-CN", "/about", true},
		{"/index", "zh-CN", "/", true},
		{"/en/about", "en-US", "/about", true},
		{"/en/index", "en-US", "/", true},
		{"/en/a/b", "en-US", "/a/b", true},
	}
	for _, c := range cases {
		lang, logical, ok := rule.Locate(c.path, langs)
		if ok != c.wantOK || lang != c.wantLang || logical != c.wantLogical {
			t.Errorf("Locate(%q) = (%q, %q, %v)，期望 (%q, %q, %v)",
				c.path, lang, logical, ok, c.wantLang, c.wantLogical, c.wantOK)
		}
	}

	all := pipeline.NewLangURLRule(true, true, "zh-CN", nil)
	if _, _, ok := all.Locate("/about", langs); ok {
		t.Fatal("all_prefix 下未带前缀的路径不应参与语言分组")
	}
	if lang, logical, ok := all.Locate("/zh/about", langs); !ok || lang != "zh-CN" || logical != "/about" {
		t.Fatalf("all_prefix Locate(/zh/about) = (%q, %q, %v)", lang, logical, ok)
	}
}

func TestLangPathRejects(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	cases := []struct {
		name string
		lang string
		path string
	}{
		{"空语言", "", "/about"},
		{"空白语言", "   ", "/about"},
		{"语言含斜杠", "zh/CN", "/about"},
		{"语言含点", "..", "/about"},
		{"语言含路径穿越", "../../etc", "/about"},
		{"语言含空格", "zh CN", "/about"},
		{"语言过长", "zh-very-long-language-tag-exceeding-limit", "/about"},
		{"路径穿越", "zh-CN", "/../etc/passwd"},
		{"保留路径", "zh-CN", "/api/login"},
		{"空路径", "zh-CN", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := rule.Path(c.lang, c.path); err == nil {
				t.Fatalf("Path(%q, %q) 应报错", c.lang, c.path)
			}
		})
	}
}

func TestLangPathLegacyCompat(t *testing.T) {
	if got, err := pipeline.LangPath("zh-CN", "/about"); err != nil || got != "/zh/about" {
		t.Fatalf("LangPath = %q (%v)，期望 /zh/about", got, err)
	}
	if got := pipeline.StripLangPath("zh-CN", "/zh/about"); got != "/about" {
		t.Fatalf("StripLangPath = %q，期望 /about", got)
	}
}

func TestNormalizeLang(t *testing.T) {
	if got, err := pipeline.NormalizeLang(" en-US "); err != nil || got != "en-US" {
		t.Fatalf("NormalizeLang 应去空白并保留原大小写: %q %v", got, err)
	}
	if _, err := pipeline.NormalizeLang(""); err == nil {
		t.Fatal("空语言码应报错")
	}
}
