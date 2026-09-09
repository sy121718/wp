package unit

// lang_test.go — 多语言访问路径映射（多语言 P2/P3，docs/06-D §5 方案 A'）。
//
// 覆盖：
//  1. default_plain（默认方案）：默认语言无前缀（/about、/index）、非默认语言短码（/en/about）；
//  2. all_prefix：全语言带短码前缀（/zh/about）；
//  3. off：全语言共用逻辑路径；
//  4. 语言码映射（内置表 / 配置覆盖 / 主语言子标签回退）与短码冲突检测；
//  5. Locate（sitemap 分组与导航反查）与 Strip 逆运算；
//  6. 非法输入必须报错（绝不产出半截前缀）。

import (
	"testing"

	"go_wp/internal/pipeline"
)

// TestLangURLRuleDefaultPlain 默认语言无前缀 + 非默认语言短码。
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

// TestLangURLRuleAllPrefix 全语言带短码前缀（历史全前缀方案的短码化）。
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

// TestLangURLRuleOff 未分离语言路径：全语言共用逻辑路径（单语言兼容）。
func TestLangURLRuleOff(t *testing.T) {
	rule := pipeline.NewLangURLRule(false, false, "zh-CN", nil)
	if got, err := rule.Path("en-US", "/about"); err != nil || got != "/about" {
		t.Fatalf("off 方案 Path = %q (%v)，期望 /about", got, err)
	}
	if got, err := rule.Path("zh-CN", "/"); err != nil || got != "/" {
		t.Fatalf("off 方案根路径应保持 /，实际 %q (%v)", got, err)
	}
}

// TestLangURLRuleCodes 短码映射：配置覆盖优先于内置表，内置表优先于回退。
func TestLangURLRuleCodes(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", map[string]string{"zh-TW": "tw", "xx-YY": "xy"})
	cases := []struct{ lang, want string }{
		{"zh-CN", "zh"}, // 内置表
		{"en-US", "en"}, // 内置表
		{"zh-TW", "tw"}, // 配置覆盖
		{"xx-YY", "xy"}, // 配置覆盖
		{"sv-SE", "sv"}, // 回退主语言子标签
		{"sw", "sw"},    // 回退整码小写
	}
	for _, c := range cases {
		if got := rule.URLCode(c.lang); got != c.want {
			t.Errorf("URLCode(%q) = %q，期望 %q", c.lang, got, c.want)
		}
	}
	// 大小写不敏感匹配配置键（viper 会把键小写化）。
	lower := pipeline.NewLangURLRule(true, false, "zh-CN", map[string]string{"zh-tw": "tw"})
	if got := lower.URLCode("zh-TW"); got != "tw" {
		t.Fatalf("配置键大小写不敏感匹配失败: %q", got)
	}
}

// TestLangURLRuleValidate 短码冲突必须在构建期报错（page_routes 唯一键会撞车）。
func TestLangURLRuleValidate(t *testing.T) {
	ok := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	if err := ok.Validate([]string{"zh-CN", "en-US"}); err != nil {
		t.Fatalf("zh-CN/en-US 不应冲突: %v", err)
	}
	// zh-TW 与 zh-Hant 都映射到 zh-tw（非默认语言，必须冲突）。
	bad := pipeline.NewLangURLRule(true, false, "en-US", nil)
	if err := bad.Validate([]string{"en-US", "zh-TW", "zh-Hant"}); err == nil {
		t.Fatal("zh-TW 与 zh-Hant 映射同一短码，应报错")
	}
	// 默认语言无前缀时不占短码：与另一语言同码不冲突。
	def := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	if err := def.Validate([]string{"zh-CN", "en-US"}); err != nil {
		t.Fatalf("默认语言不占短码: %v", err)
	}
}

// TestLangURLRuleStrip 逆运算：访问路径 → 逻辑路径。
func TestLangURLRuleStrip(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	cases := []struct{ lang, path, want string }{
		{"zh-CN", "/index", "/"},
		{"zh-CN", "/about", "/about"},
		{"en-US", "/en/index", "/"},
		{"en-US", "/en/about", "/about"},
		{"en-US", "/en/a/b", "/a/b"},
		{"en-US", "/about", "/about"}, // 未带本语言前缀：原样
		{"zh-CN", "/en/about", "/en/about"},
	}
	for _, c := range cases {
		if got := rule.Strip(c.lang, c.path); got != c.want {
			t.Errorf("Strip(%q, %q) = %q，期望 %q", c.lang, c.path, got, c.want)
		}
	}
}

// TestLangURLRuleLocate 语言归属（sitemap 分组 / 导航反查）：
// 默认语言无前缀的路径归属默认语言，这正是 /about 与 /en/about 互指的前提。
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
	// all_prefix：无前缀路径不参与分组。
	all := pipeline.NewLangURLRule(true, true, "zh-CN", nil)
	if _, _, ok := all.Locate("/about", langs); ok {
		t.Fatal("all_prefix 下未带前缀的路径不应参与语言分组")
	}
	if lang, logical, ok := all.Locate("/zh/about", langs); !ok || lang != "zh-CN" || logical != "/about" {
		t.Fatalf("all_prefix Locate(/zh/about) = (%q, %q, %v)", lang, logical, ok)
	}
}

// TestLangPathRejects 非法语言码/路径必须报错（防路径穿越与保留路径污染）。
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

// TestLangPathLegacyCompat 历史入口 LangPath / StripLangPath = 全语言短码前缀。
func TestLangPathLegacyCompat(t *testing.T) {
	if got, err := pipeline.LangPath("zh-CN", "/about"); err != nil || got != "/zh/about" {
		t.Fatalf("LangPath = %q (%v)，期望 /zh/about", got, err)
	}
	if got := pipeline.StripLangPath("zh-CN", "/zh/about"); got != "/about" {
		t.Fatalf("StripLangPath = %q，期望 /about", got)
	}
}

// TestNormalizeLang 语言码规范化。
func TestNormalizeLang(t *testing.T) {
	if got, err := pipeline.NormalizeLang(" en-US "); err != nil || got != "en-US" {
		t.Fatalf("NormalizeLang 应去空白并保留原大小写: %q %v", got, err)
	}
	if _, err := pipeline.NormalizeLang(""); err == nil {
		t.Fatal("空语言码应报错")
	}
}
