package compliance

// compliance_test.go — 规则表的就近单测（纯函数，不碰库、不读盘）。
//
// 用例组织方式是「一份产物 + 期望命中的规则集合」：报告是对外输出，判断的粒度必须是
// 规则 id（不是「有没有结论」）—— 只断言「有结论」的测试在规则写错时会照样绿。

import (
	"encoding/json"
	"strings"
	"testing"
)

// htmlDoc 组装一份形状与 builder 产物一致的文档（head 内容由用例给定）。
func htmlDoc(lang, head string) []byte {
	return []byte("<!DOCTYPE html>\n<html lang=\"" + lang + "\">\n<head>\n<meta charset=\"utf-8\">\n" +
		head + "</head>\n<body><main id=\"main-content\"></main></body>\n</html>\n")
}

// baseHead 一份「处处合规」的 head：标题 + canonical + JSON-LD（name/url 与视图一致）。
func baseHead(title, canonical string) string {
	ld := map[string]any{
		"@context": "https://schema.org", "@type": "WebPage",
		"name": title, "headline": title, "url": canonical,
	}
	b, _ := json.Marshal(ld)
	return "<title>" + title + "</title>\n" +
		"<link rel=\"canonical\" href=\"" + canonical + "\">\n" +
		"<script type=\"application/ld+json\">" + string(b) + "</script>\n"
}

// rulesOf 取结论里的规则 id 集合。
func rulesOf(rep *Report) map[string]int {
	out := map[string]int{}
	for _, f := range rep.Findings {
		out[f.Rule]++
	}
	return out
}

// artifacts 两个语言的站点语言表（默认语言 zh-CN 不带前缀，en-US 带 /en）。
func twoLangs() []LangRule {
	return []LangRule{{Code: "zh-CN", Prefix: ""}, {Code: "en-US", Prefix: "/en"}}
}

// TestInspectBaselineClean 合规产物必须一条结论都不产生。
//
// 这是「不误报」的基线：校验器最容易的失败方式是天天喊狼来了，那它就等于不存在。
func TestInspectBaselineClean(t *testing.T) {
	rep := Inspect(Artifact{
		URL: "/about", Lang: "zh-CN", ArtifactHash: "h1",
		HTML: htmlDoc("zh-CN", baseHead("关于我们", "/about")),
	})
	if len(rep.Findings) != 0 {
		t.Fatalf("合规产物不应有任何结论，实际：%+v", rep.Findings)
	}
	if rep.Checks != perArtifactChecks {
		t.Fatalf("规则条目数应为 %d，实际 %d", perArtifactChecks, rep.Checks)
	}
	if !rep.Index.Index || !rep.Index.Follow || rep.Index.Explicit {
		t.Fatalf("未声明 robots 时应按默认 index/follow 且 Explicit=false，实际 %+v", rep.Index)
	}
	if !rep.OK() {
		t.Fatal("无 error 级结论时 OK() 应为 true")
	}
}

func TestInspectCanonicalSiteBoundary(t *testing.T) {
	cases := []struct {
		name, base, canonical, path string
		want                        []string
	}{
		{"本站绝对地址", "https://shop.test", "https://shop.test/about", "/about", nil},
		{"本站大小写域名", "https://SHOP.test", "https://shop.test/about", "/about", nil},
		{"本站子路径", "https://shop.test/store", "https://shop.test/store/about", "/about", nil},
		{"本站子路径根", "https://shop.test/store", "https://shop.test/store/", "/", nil},
		{"本站子路径无尾斜杠", "https://shop.test/store", "https://shop.test/store", "/", nil},
		{"本站绝对地址路径不符", "https://shop.test/store", "https://shop.test/store/other", "/about", []string{RuleCanonicalPathMismatch}},
		{"不同域名", "https://shop.test", "https://other.test/about", "/about", []string{RuleCanonicalExternal}},
		{"相似域名", "https://shop.test", "https://shop.test.evil/about", "/about", []string{RuleCanonicalExternal}},
		{"不同协议", "https://shop.test", "http://shop.test/about", "/about", []string{RuleCanonicalExternal}},
		{"不同端口", "https://shop.test:8443", "https://shop.test:9443/about", "/about", []string{RuleCanonicalExternal}},
		{"用户名注入", "https://shop.test", "https://shop.test@evil.test/about", "/about", []string{RuleCanonicalExternal}},
		{"伪造基址身份", "https://shop.test@evil.test", "https://evil.test/about", "/about", []string{RuleCanonicalExternal}},
		{"非法基址", "not-a-site", "https://shop.test/about", "/about", []string{RuleCanonicalExternal}},
		{"子路径外", "https://shop.test/store", "https://shop.test/storefront/about", "/about", []string{RuleCanonicalExternal}},
		{"子路径逃逸", "https://shop.test/store", "https://shop.test/store/../other", "/about", []string{RuleCanonicalExternal}},
		{"编码斜杠伪装前缀", "https://shop.test/store", "https://shop.test/store%2Fabout", "/about", []string{RuleCanonicalExternal}},
		{"其他子站", "https://shop.test/store", "https://shop.test/other/about", "/about", []string{RuleCanonicalExternal}},
		{"协议相对跨域", "https://shop.test", "//other.test/about", "/about", []string{RuleCanonicalExternal}},
		{"无基址的绝对地址", "", "https://shop.test/about", "/about", []string{RuleCanonicalExternal}},
		{"无基址的相对地址", "", "/about", "/about", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := Inspect(Artifact{URL: tc.path, SiteBaseURL: tc.base,
				HTML: htmlDoc("zh-CN", baseHead("关于我们", tc.canonical))})
			got := rulesOf(rep)
			if len(got) != len(tc.want) {
				t.Fatalf("canonical=%q base=%q: 结论=%v，期望=%v", tc.canonical, tc.base, got, tc.want)
			}
			for _, rule := range tc.want {
				if got[rule] != 1 {
					t.Errorf("规则 %s 命中 %d 次，结论=%v", rule, got[rule], got)
				}
			}
		})
	}
}

func TestInspectCanonicalLanguageWithSiteBase(t *testing.T) {
	base := "https://shop.test/store"
	langs := twoLangs()
	for _, tc := range []struct {
		name, canonical string
		want            []string
	}{
		{"本站当前语言", base + "/en/about", nil},
		{"本站另一语言", base + "/about", []string{RuleCanonicalPathMismatch, RuleCanonicalLangMismatch}},
		{"跨域另一语言不追加语言误报", "https://other.test/about", []string{RuleCanonicalExternal}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep := Inspect(Artifact{URL: "/en/about", Lang: "en-US", Langs: langs, SiteBaseURL: base,
				HTML: htmlDoc("en-US", baseHead("About", tc.canonical))})
			got := rulesOf(rep)
			if len(got) != len(tc.want) {
				t.Fatalf("规则=%v，期望=%v", got, tc.want)
			}
			for _, rule := range tc.want {
				if got[rule] != 1 {
					t.Errorf("规则 %s 命中 %d 次，实际 %v", rule, got[rule], got)
				}
			}
		})
	}
}

// TestInspectRules 逐条规则：给定产物形状 → 必须命中指定的规则 id。
func TestInspectRules(t *testing.T) {
	cases := []struct {
		name    string
		art     Artifact
		want    []string // 期望命中的规则（集合相等，顺序不敏感）
		wantLev map[string]Level
	}{
		{
			name: "缺标题",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				"<link rel=\"canonical\" href=\"/a\">\n")},
			want: []string{RuleTitleMissing},
		},
		{
			name: "缺canonical",
			art:  Artifact{URL: "/a", HTML: htmlDoc("zh-CN", baseHead("标题", ""))},
			want: []string{RuleCanonicalMissing},
		},
		{
			name: "canonical的href为空",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				"<title>标题</title>\n<link rel=\"canonical\" href=\"\">\n")},
			want: []string{RuleCanonicalMissing, RuleJSONLDMissing},
		},
		{
			name: "两条canonical",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				baseHead("标题", "/a")+"<link rel=\"canonical\" href=\"/b\">\n")},
			want: []string{RuleCanonicalMultiple},
		},
		{
			name: "canonical指向别的路径",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				"<title>标题</title>\n<link rel=\"canonical\" href=\"/b\">\n")},
			want:    []string{RuleCanonicalPathMismatch, RuleJSONLDMissing},
			wantLev: map[string]Level{RuleCanonicalPathMismatch: LevelWarn},
		},
		{
			name: "跨域canonical（需人工确认，不升级为error）",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				"<title>标题</title>\n<link rel=\"canonical\" href=\"https://other.example/a\">\n")},
			want:    []string{RuleCanonicalExternal, RuleJSONLDMissing},
			wantLev: map[string]Level{RuleCanonicalExternal: LevelWarn},
		},
		{
			name: "语种与路径不一致（en-US 的页面落在无前缀路径）",
			art: Artifact{URL: "/about", Lang: "en-US", Langs: twoLangs(),
				HTML: htmlDoc("en-US", baseHead("About", "/about"))},
			want: []string{RuleLangPathMismatch, RuleCanonicalLangMismatch},
		},
		{
			name: "canonical指向别的语言",
			art: Artifact{URL: "/en/about", Lang: "en-US", Langs: twoLangs(),
				HTML: htmlDoc("en-US", baseHead("About", "/about"))},
			want: []string{RuleCanonicalPathMismatch, RuleCanonicalLangMismatch},
		},
		{
			name: "noindex却进了sitemap",
			art: Artifact{URL: "/a", SitemapListed: true,
				HTML: htmlDoc("zh-CN", baseHead("标题", "/a")+
					"<meta name=\"robots\" content=\"noindex,follow\">\n")},
			want: []string{RuleSitemapNoindexConflict},
		},
		{
			name: "robots同时声明index与noindex",
			art: Artifact{URL: "/a",
				HTML: htmlDoc("zh-CN", baseHead("标题", "/a")+
					"<meta name=\"robots\" content=\"index,noindex\">\n")},
			want: []string{RuleRobotsInvalid},
		},
		{
			name: "robots出现白名单外的取值",
			art: Artifact{URL: "/a",
				HTML: htmlDoc("zh-CN", baseHead("标题", "/a")+
					"<meta name=\"robots\" content=\"noindex,archive\">\n")},
			want: []string{RuleRobotsInvalid},
		},
		{
			name: "JSON-LD不是合法JSON",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				"<title>标题</title>\n<link rel=\"canonical\" href=\"/a\">\n"+
					"<script type=\"application/ld+json\">{not json}</script>\n")},
			want: []string{RuleJSONLDUnparseable},
		},
		{
			name: "JSON-LD的url与canonical不一致",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				"<title>标题</title>\n<link rel=\"canonical\" href=\"/a\">\n"+
					"<script type=\"application/ld+json\">{\"@type\":\"WebPage\",\"name\":\"标题\",\"url\":\"/b\"}</script>\n")},
			want: []string{RuleJSONLDURLMismatch},
		},
		{
			name: "JSON-LD的name与标题不一致",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				"<title>标题</title>\n<link rel=\"canonical\" href=\"/a\">\n"+
					"<script type=\"application/ld+json\">{\"@type\":\"WebPage\",\"name\":\"别的标题\",\"url\":\"/a\"}</script>\n")},
			want: []string{RuleJSONLDTitleMismatch},
		},
		{
			name: "有标题但没有JSON-LD",
			art: Artifact{URL: "/a", HTML: htmlDoc("zh-CN",
				"<title>标题</title>\n<link rel=\"canonical\" href=\"/a\">\n")},
			want:    []string{RuleJSONLDMissing},
			wantLev: map[string]Level{RuleJSONLDMissing: LevelWarn},
		},
		{
			name: "hreflang重复语言码",
			art: Artifact{URL: "/a", Lang: "zh-CN", Langs: twoLangs(),
				HTML: htmlDoc("zh-CN", baseHead("标题", "/a")+
					"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/a\">\n"+
					"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/a\">\n")},
			want: []string{RuleHreflangDuplicateLang},
		},
		{
			name: "互指组缺自指",
			art: Artifact{URL: "/a", Lang: "zh-CN", Langs: twoLangs(),
				HTML: htmlDoc("zh-CN", baseHead("标题", "/a")+
					"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/a\">\n"+
					"<link rel=\"alternate\" hreflang=\"ja\" href=\"/ja/a\">\n")},
			want: []string{RuleHreflangSelfMissing, RuleHreflangUnknownLang, RuleHreflangXDefaultMissing},
		},
		{
			name: "空产物",
			art:  Artifact{URL: "/a"},
			want: []string{RuleArtifactEmpty},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := Inspect(c.art)
			got := rulesOf(rep)
			want := map[string]int{}
			for _, r := range c.want {
				want[r]++
			}
			for r, n := range want {
				if got[r] != n {
					t.Errorf("期望命中规则 %s 共 %d 次，实际 %d 次（全部结论：%+v）", r, n, got[r], rep.Findings)
				}
			}
			for r, n := range got {
				if want[r] == 0 {
					t.Errorf("产生了未预期的规则 %s（%d 次）：%+v", r, n, rep.Findings)
				}
			}
			for rule, lv := range c.wantLev {
				for _, f := range rep.Findings {
					if f.Rule == rule && f.Level != lv {
						t.Errorf("规则 %s 的等级应为 %s，实际 %s", rule, lv, f.Level)
					}
				}
			}
			// 每条结论都必须能定位到页面与产物：报告的可复核性靠这两个字段。
			for _, f := range rep.Findings {
				if f.URL != c.art.URL {
					t.Errorf("结论缺少可定位的 URL：%+v", f)
				}
				if f.Evidence == "" || f.Expect == "" {
					t.Errorf("结论必须带证据与判据：%+v", f)
				}
			}
		})
	}
}

// TestInspectDeterministic 同一输入必得同一结论（字节级）。
//
// 「确定性」是这次整改的硬要求：结论不能依赖 map 遍历顺序、时间或任何运行时状态。
func TestInspectDeterministic(t *testing.T) {
	art := Artifact{
		URL: "/en/about", Lang: "en-US", ArtifactHash: "h9",
		Langs: []LangRule{{Code: "zh-CN"}, {Code: "en-US", Prefix: "/en"}},
		HTML: htmlDoc("en-US",
			"<title>About</title>\n"+
				"<link rel=\"canonical\" href=\"/about\">\n"+
				"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/about\">\n"+
				"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"/about\">\n"+
				"<script type=\"application/ld+json\">{\"@type\":\"WebPage\",\"name\":\"About\",\"url\":\"/about\"}</script>\n"),
	}
	first, _ := json.Marshal(Inspect(art))
	for i := 0; i < 20; i++ {
		got, _ := json.Marshal(Inspect(art))
		if string(got) != string(first) {
			t.Fatalf("第 %d 次结论与首次不一致：\n%s\n%s", i, first, got)
		}
	}
	if !strings.Contains(string(first), "canonical.lang-mismatch") {
		t.Fatalf("用例本身应命中 canonical.lang-mismatch，实际：%s", first)
	}
}

// TestRuleRegistryConsistent 规则登记清单与计数常量必须一致。
func TestRuleRegistryConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range RuleIDs {
		if seen[r] {
			t.Fatalf("规则 id 重复登记：%s", r)
		}
		seen[r] = true
	}
	if len(RuleIDs) != perArtifactChecks+siteChecks {
		t.Fatalf("登记了 %d 条规则，但计数常量说 %d + %d 条 —— 新增规则必须同步计数",
			len(RuleIDs), perArtifactChecks, siteChecks)
	}
}

// ---------- 站点级（跨产物） ----------

// TestInspectSiteReciprocalClean 双向互指的一对产物不产生跨产物结论。
func TestInspectSiteReciprocalClean(t *testing.T) {
	zh := Artifact{URL: "/about", Lang: "zh-CN", ArtifactHash: "hz", Langs: twoLangs(),
		HTML: htmlDoc("zh-CN", baseHead("关于我们", "/about")+
			"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"x-default\" href=\"/about\">\n")}
	en := Artifact{URL: "/en/about", Lang: "en-US", ArtifactHash: "he", Langs: twoLangs(),
		HTML: htmlDoc("en-US", baseHead("About", "/en/about")+
			"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"x-default\" href=\"/about\">\n")}
	site := InspectSite([]Artifact{zh, en})
	if len(site.Findings) != 0 {
		t.Fatalf("双向互指不应产生跨产物结论：%+v", site.Findings)
	}
	if site.Checked != 2 || site.ErrorCount() != 0 || site.Healthy() != 2 {
		t.Fatalf("巡检汇总不正确：checked=%d errors=%d healthy=%d", site.Checked, site.ErrorCount(), site.Healthy())
	}
}

// TestInspectSiteOneWayIsReported 单向互指必须被指出（用另一侧的实际产物证伪）。
func TestInspectSiteOneWayIsReported(t *testing.T) {
	zh := Artifact{URL: "/about", Lang: "zh-CN", ArtifactHash: "hz", Langs: twoLangs(),
		HTML: htmlDoc("zh-CN", baseHead("关于我们", "/about")+
			"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/about\">\n")}
	// 英文页只声明了自己：没有回指中文页。
	en := Artifact{URL: "/en/about", Lang: "en-US", ArtifactHash: "he", Langs: twoLangs(),
		HTML: htmlDoc("en-US", baseHead("About", "/en/about")+
			"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"/en/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/about\">\n")}
	site := InspectSite([]Artifact{zh, en})
	got := map[string]int{}
	for _, f := range site.Findings {
		got[f.Rule+"@"+f.URL]++
	}
	if got[RuleHreflangNotReciprocal+"@/about"] != 1 {
		t.Fatalf("应报出中文页的单向互指，实际：%+v", site.Findings)
	}
}

// TestInspectSiteTargetLangMismatch 互指目标页面的语言与声明不符时以事实为准报出。
func TestInspectSiteTargetLangMismatch(t *testing.T) {
	zh := Artifact{URL: "/about", Lang: "zh-CN", ArtifactHash: "hz", Langs: twoLangs(),
		HTML: htmlDoc("zh-CN", baseHead("关于我们", "/about")+
			"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"ja\" href=\"/en/about\">\n")}
	en := Artifact{URL: "/en/about", Lang: "en-US", ArtifactHash: "he", Langs: twoLangs(),
		HTML: htmlDoc("en-US", baseHead("About", "/en/about")+
			"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"/about\">\n")}
	site := InspectSite([]Artifact{zh, en})
	found := false
	for _, f := range site.Findings {
		if f.Rule == RuleHreflangTargetLangMismatch && f.URL == "/about" {
			found = true
			if !strings.Contains(f.Evidence, "en-US") {
				t.Fatalf("证据里必须带目标页面的实际语言：%+v", f)
			}
		}
	}
	if !found {
		t.Fatalf("应报出 hreflang 目标语言不符，实际：%+v", site.Findings)
	}
}

// TestInspectSiteIgnoresAbsentTargets 目标不在巡检集合里时不下结论（不猜、不误报）。
//
// 「A 声明了 B，但 B 压根不存在」属于按激活清单抓取校验的范畴（审计 SEO-01 后半段，
// 本轮不做）：在只能看到 A 的时候下结论就会产生误报。
func TestInspectSiteIgnoresAbsentTargets(t *testing.T) {
	only := Artifact{URL: "/about", Lang: "zh-CN", ArtifactHash: "hz", Langs: twoLangs(),
		HTML: htmlDoc("zh-CN", baseHead("关于我们", "/about")+
			"<link rel=\"alternate\" hreflang=\"zh-CN\" href=\"/about\">\n"+
			"<link rel=\"alternate\" hreflang=\"en-US\" href=\"/en/about\">\n")}
	site := InspectSite([]Artifact{only})
	if len(site.Findings) != 0 {
		t.Fatalf("目标不在集合内时不应产生跨产物结论：%+v", site.Findings)
	}
}

// TestInspectSiteOrderIndependent 入参顺序不影响结论。
func TestInspectSiteOrderIndependent(t *testing.T) {
	zh := Artifact{URL: "/about", Lang: "zh-CN", ArtifactHash: "hz", Langs: twoLangs(),
		HTML: htmlDoc("zh-CN", baseHead("关于我们", "/about"))}
	en := Artifact{URL: "/en/about", Lang: "en-US", ArtifactHash: "he", Langs: twoLangs(),
		HTML: htmlDoc("en-US", baseHead("About", "/en/about"))}
	a, _ := json.Marshal(InspectSite([]Artifact{zh, en}).AllFindings())
	b, _ := json.Marshal(InspectSite([]Artifact{en, zh}).AllFindings())
	if string(a) != string(b) {
		t.Fatalf("结论依赖入参顺序：\n%s\n%s", a, b)
	}
}
