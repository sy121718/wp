package compliance

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/seo"
)

type Level string

const (
	LevelError Level = "error"
	LevelWarn  Level = "warn"
)

const (
	RuleArtifactEmpty = "artifact.empty"

	RuleTitleMissing = "title.missing"

	RuleCanonicalMissing = "canonical.missing"

	RuleCanonicalMultiple = "canonical.multiple"

	RuleCanonicalExternal = "canonical.external"

	RuleCanonicalPathMismatch = "canonical.path-mismatch"

	RuleCanonicalLangMismatch = "canonical.lang-mismatch"

	RuleLangPathMismatch = "lang.path-mismatch"

	RuleRobotsInvalid = "robots.invalid"

	RuleSitemapNoindexConflict = "sitemap.noindex-conflict"

	RuleJSONLDUnparseable = "jsonld.unparseable"

	RuleJSONLDMissing = "jsonld.missing"

	RuleJSONLDURLMismatch = "jsonld.url-mismatch"

	RuleJSONLDTitleMismatch = "jsonld.title-mismatch"

	RuleHreflangDuplicateLang = "hreflang.duplicate-lang"

	RuleHreflangSelfMissing = "hreflang.self-missing"

	RuleHreflangUnknownLang = "hreflang.lang-unknown"

	RuleHreflangXDefaultMissing = "hreflang.x-default-missing"

	RuleHreflangNotReciprocal = "hreflang.not-reciprocal"

	RuleHreflangTargetLangMismatch = "hreflang.target-lang-mismatch"
)

var RuleIDs = []string{
	RuleArtifactEmpty,
	RuleTitleMissing,
	RuleCanonicalMissing,
	RuleCanonicalMultiple,
	RuleCanonicalExternal,
	RuleCanonicalPathMismatch,
	RuleCanonicalLangMismatch,
	RuleLangPathMismatch,
	RuleRobotsInvalid,
	RuleSitemapNoindexConflict,
	RuleJSONLDUnparseable,
	RuleJSONLDMissing,
	RuleJSONLDURLMismatch,
	RuleJSONLDTitleMismatch,
	RuleHreflangDuplicateLang,
	RuleHreflangSelfMissing,
	RuleHreflangUnknownLang,
	RuleHreflangXDefaultMissing,
	RuleHreflangNotReciprocal,
	RuleHreflangTargetLangMismatch,
}

const perArtifactChecks = 18

const siteChecks = 2

type LangRule struct {
	Code   string
	Prefix string
}

type Artifact struct {
	URL string

	Lang string

	ArtifactHash string

	HTML []byte

	Langs []LangRule

	SiteBaseURL string

	SitemapListed bool
}

type IndexPolicy struct {
	Index bool

	Follow bool

	Explicit bool

	Raw string
}

type Finding struct {
	Rule string `json:"rule"`

	Level        Level  `json:"level"`
	URL          string `json:"url"`
	Lang         string `json:"lang,omitempty"`
	ArtifactHash string `json:"artifactHash,omitempty"`

	Evidence string `json:"evidence"`

	Expect string `json:"expect"`
}

type Report struct {
	URL          string      `json:"url"`
	Lang         string      `json:"lang,omitempty"`
	ArtifactHash string      `json:"artifactHash,omitempty"`
	Index        IndexPolicy `json:"indexPolicy"`

	Checks int `json:"checks"`

	Findings []Finding `json:"findings"`
}

func (r *Report) OK() bool {
	if r == nil {
		return true
	}
	for _, f := range r.Findings {
		if f.Level == LevelError {
			return false
		}
	}
	return true
}

func (r *Report) add(a Artifact, rule string, level Level, evidence, expect string) {
	r.Findings = append(r.Findings, Finding{
		Rule: rule, Level: level, URL: a.URL, Lang: a.Lang,
		ArtifactHash: a.ArtifactHash,
		Evidence:     truncateEvidence(evidence),
		Expect:       expect,
	})
}

const maxEvidenceRunes = 200

func truncateEvidence(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if len([]rune(s)) <= maxEvidenceRunes {
		return s
	}
	return string([]rune(s)[:maxEvidenceRunes]) + "…"
}

func Inspect(a Artifact) *Report {
	rep := &Report{URL: a.URL, Lang: a.Lang, ArtifactHash: a.ArtifactHash, Checks: perArtifactChecks}
	if len(a.HTML) == 0 {
		rep.add(a, RuleArtifactEmpty, LevelError,
			"产物入口 HTML 为空", "产物目录里必须有非空 index.html")
		return rep
	}
	raw := string(a.HTML)
	head := headOf(raw)
	doc := parseHead(head)

	checkTitle(rep, a, doc)
	canonical := checkCanonical(rep, a, doc)
	checkLangPath(rep, a)
	checkCanonicalLang(rep, a, canonical)
	policy := checkRobots(rep, a, doc)
	rep.Index = policy
	checkSitemapConflict(rep, a, policy)
	checkJSONLD(rep, a, raw, head, doc, canonical)
	checkHreflang(rep, a, doc)
	return rep
}

func checkTitle(rep *Report, a Artifact, doc headDoc) {
	if strings.TrimSpace(doc.Title) == "" {
		rep.add(a, RuleTitleMissing, LevelError,
			"head 中没有 <title> 或内容为空", "每份产物必须有非空 <title>（标题明确是索引策略的前提）")
	}
}

func checkCanonical(rep *Report, a Artifact, doc headDoc) string {
	nonEmpty := make([]string, 0, len(doc.Canonicals))
	for _, c := range doc.Canonicals {
		if strings.TrimSpace(c) != "" {
			nonEmpty = append(nonEmpty, strings.TrimSpace(c))
		}
	}
	switch {
	case len(doc.Canonicals) == 0:
		rep.add(a, RuleCanonicalMissing, LevelError,
			"head 中没有 <link rel=\"canonical\">", "每份产物必须有且只有一条 canonical")
	case len(nonEmpty) == 0:
		rep.add(a, RuleCanonicalMissing, LevelError,
			"canonical 标签存在但 href 为空", "canonical 必须给出可访问的地址")
	case len(nonEmpty) > 1:
		rep.add(a, RuleCanonicalMultiple, LevelError,
			fmt.Sprintf("head 中有 %d 条 canonical：%s", len(nonEmpty), strings.Join(nonEmpty, " | ")),
			"只能有一条 canonical —— 多条时搜索引擎只能猜哪条权威")
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	canonical := nonEmpty[0]

	if isExternal(canonical, a.SiteBaseURL) {
		rep.add(a, RuleCanonicalExternal, LevelWarn,
			fmt.Sprintf("canonical=%s（跨域或站点基址未知），产物访问路径=%s", canonical, a.URL),
			"未证实属于可信站点基址的 canonical 需人工确认；跨域地址会将本页的收录权重让给目标")
		return canonical
	}

	want := seo.CanonicalPublicPath(a.URL)
	got := seo.CanonicalPublicPath(canonicalPathOf(canonical, a.SiteBaseURL))
	if got != want {
		rep.add(a, RuleCanonicalPathMismatch, LevelWarn,
			fmt.Sprintf("canonical=%s，产物访问路径=%s", canonical, a.URL),
			"canonical 应与产物自身的访问路径一致，否则本页在宣称自己住在另一个地址")
	}
	return canonical
}

func checkLangPath(rep *Report, a Artifact) {
	if len(a.Langs) < 2 {
		return
	}
	expected, ok := ruleForLang(a.Langs, a.Lang)
	if !ok {
		return
	}
	got, matched := ruleForPath(a.Langs, seo.CanonicalPublicPath(a.URL))
	if !matched || !strings.EqualFold(got.Code, expected.Code) {
		detail := "路径不匹配任何已启用语言的形态"
		if matched {
			detail = fmt.Sprintf("路径归属语言 %s", got.Code)
		}
		rep.add(a, RuleLangPathMismatch, LevelError,
			fmt.Sprintf("构建语言=%s（前缀 %q），url=%s：%s", a.Lang, expected.Prefix, a.URL, detail),
			"同一语言的所有页面必须落在该语言的路径前缀下，否则语言之间的关系无法被声明")
	}
}

func checkCanonicalLang(rep *Report, a Artifact, canonical string) {
	if len(a.Langs) < 2 || canonical == "" {
		return
	}
	if isExternal(canonical, a.SiteBaseURL) {
		return
	}
	expected, ok := ruleForLang(a.Langs, a.Lang)
	if !ok {
		return
	}
	got, matched := ruleForPath(a.Langs, seo.CanonicalPublicPath(canonicalPathOf(canonical, a.SiteBaseURL)))
	if !matched || !strings.EqualFold(got.Code, expected.Code) {
		rep.add(a, RuleCanonicalLangMismatch, LevelError,
			fmt.Sprintf("构建语言=%s，canonical=%s", a.Lang, canonical),
			"canonical 必须指向本语言的地址：指向另一种语言等于把本页声明成它的副本")
	}
}

func checkRobots(rep *Report, a Artifact, doc headDoc) IndexPolicy {
	policy := IndexPolicy{Index: true, Follow: true}
	raw := strings.TrimSpace(doc.Robots)
	if raw == "" {
		return policy
	}
	policy.Explicit = true
	policy.Raw = raw
	var bad []string
	indexSeen, noindexSeen := false, false
	for _, tok := range strings.Split(strings.ToLower(raw), ",") {
		tok = strings.TrimSpace(tok)
		switch tok {
		case "":
		case "index":
			indexSeen = true
		case "noindex":
			noindexSeen = true
		case "follow":
			policy.Follow = true
		case "nofollow":
			policy.Follow = false
		default:
			bad = append(bad, tok)
		}
	}
	policy.Index = !noindexSeen
	switch {
	case len(bad) > 0:
		rep.add(a, RuleRobotsInvalid, LevelError,
			fmt.Sprintf("robots content=%q 含白名单外取值：%s", raw, strings.Join(bad, ",")),
			"robots 只能是 index/noindex/follow/nofollow 的组合（取值不受控等于索引策略不明确）")
	case indexSeen && noindexSeen:
		rep.add(a, RuleRobotsInvalid, LevelError,
			fmt.Sprintf("robots content=%q 同时声明 index 与 noindex", raw),
			"同一份产物不能同时要求收录与不收录")
	}
	return policy
}

func checkSitemapConflict(rep *Report, a Artifact, policy IndexPolicy) {
	if policy.Index || !a.SitemapListed {
		return
	}
	rep.add(a, RuleSitemapNoindexConflict, LevelError,
		"robots=noindex 但该 URL 出现在激活清单（sitemap 由激活清单生成）",
		"noindex 的页面不应进 sitemap：两条指令互相抵消，抓取预算被浪费")
}

func checkJSONLD(rep *Report, a Artifact, raw, head string, doc headDoc, canonical string) {

	all := jsonLDScripts(raw)
	headCount := len(jsonLDScripts(head))
	if headCount == 0 && strings.TrimSpace(doc.Title) != "" {
		rep.add(a, RuleJSONLDMissing, LevelWarn,
			"有 <title> 但 head 里没有 application/ld+json",
			"标题存在时应当输出页面级结构化数据，否则搜索结果拿不到实体信息")
	}
	type parsed struct {
		src   string
		nodes []map[string]any
	}
	docs := make([]parsed, 0, len(all))
	for _, src := range all {
		var v any
		if err := json.Unmarshal([]byte(src), &v); err != nil {
			rep.add(a, RuleJSONLDUnparseable, LevelError,
				fmt.Sprintf("JSON-LD 解析失败：%v；脚本=%s", err, src),
				"结构化数据必须是合法 JSON，否则整份断言失效")
			continue
		}
		docs = append(docs, parsed{src: src, nodes: mainEntities(v)})
	}

	headSrc := jsonLDScripts(head)
	if len(headSrc) == 0 {
		return
	}
	for _, p := range docs {
		if !containsStr(headSrc, p.src) {
			continue
		}
		for _, node := range p.nodes {
			if u, ok := node["url"].(string); ok && strings.TrimSpace(u) != "" && canonical != "" {
				if seo.CanonicalPublicPath(pathOf(u)) != seo.CanonicalPublicPath(pathOf(canonical)) {
					rep.add(a, RuleJSONLDURLMismatch, LevelError,
						fmt.Sprintf("jsonld url=%s，canonical=%s", u, canonical),
						"同一份产物里两处地址断言必须一致（搜索引擎按它判定实体身份）")
				}
			}
			name := nodeString(node, "headline")
			if name == "" {
				name = nodeString(node, "name")
			}
			if name != "" && strings.TrimSpace(doc.Title) != "" && strings.TrimSpace(name) != strings.TrimSpace(doc.Title) {
				rep.add(a, RuleJSONLDTitleMismatch, LevelError,
					fmt.Sprintf("jsonld name=%q，<title>=%q", name, doc.Title),
					"结构化数据的标题必须与可见标题一致，否则语义与页面不符")
			}
		}
	}
}

func checkHreflang(rep *Report, a Artifact, doc headDoc) {
	entries := doc.Hreflangs
	if len(entries) == 0 {
		return
	}
	seen := map[string]string{}
	var dup []string
	xDefault := 0
	selfPath := seo.CanonicalPublicPath(a.URL)
	selfFound := false
	var unknown []string
	for _, e := range entries {
		lang := strings.TrimSpace(e.Lang)
		lower := strings.ToLower(lang)
		if lower == "x-default" {
			xDefault++
			continue
		}
		if prev, ok := seen[lower]; ok {
			dup = append(dup, fmt.Sprintf("%s（href=%s 与 %s）", lang, e.Href, prev))
			continue
		}
		seen[lower] = e.Href
		if strings.EqualFold(lang, a.Lang) && seo.CanonicalPublicPath(pathOf(e.Href)) == selfPath {
			selfFound = true
		}
		if len(a.Langs) >= 2 {
			if _, ok := ruleForLang(a.Langs, lang); !ok {
				unknown = append(unknown, lang)
			}
		}
	}
	if len(dup) > 0 {
		rep.add(a, RuleHreflangDuplicateLang, LevelError,
			"重复的 hreflang："+strings.Join(dup, "；"),
			"同一语言只能声明一条互指（重复标注属无效声明，搜索引擎会忽略整组）")
	}

	if len(seen) >= 2 && !selfFound {
		rep.add(a, RuleHreflangSelfMissing, LevelError,
			fmt.Sprintf("互指组内没有本语言（%s，路径 %s）的自指条目", a.Lang, selfPath),
			"互指组必须包含本页自身：缺自指时搜索引擎无法确认这组声明覆盖当前页面")
	}
	if len(unknown) > 0 {
		rep.add(a, RuleHreflangUnknownLang, LevelWarn,
			"hreflang 不在站点启用语言清单内："+strings.Join(unknown, ","),
			"互指到未启用的语言通常意味着语言码写错或该语言已下线")
	}
	if len(seen) >= 2 && xDefault == 0 {
		rep.add(a, RuleHreflangXDefaultMissing, LevelWarn,
			"互指组有 "+fmt.Sprint(len(seen))+" 种语言但没有 x-default",
			"多语言站点应当声明 x-default，否则未匹配语言的用户由搜索引擎自行裁决")
	}
}
