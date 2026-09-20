// Package compliance 实现构建期 SEO 合规校验（审计 SEO-01 的前半段）。
//
// 与 internal/seo/scoring 的边界（报告明确要求写清，不要让两者互相顶替）：
//   - scoring 回答「这份内容写得好不好」：标题长度、关键词密度、内链数量……
//     它是编辑期的辅助信号，分数高低与搜索引擎排名之间没有因果关系，**不是**
//     发布成功的判据，也**不得**被改造成发布闸门；
//   - 本包回答「这份产物自相矛盾吗」：对已经产出的产物字节做确定性事实校验，
//     不含任何评分项、不设主观阈值、不联网、不查库。同一输入必得同一结论。
//
// 两者都不等于排名算法。合规校验通过只说明产物内部一致（有且只有一条 canonical、
// 语种与路径一致、JSON-LD 可解析且与可见内容一致、标题与索引策略明确），它证明
// 不了线上已收录、更保证不了排名 —— 那些必须由站长平台的实测数据回答（见审计
// SEO-01 的后半段，本轮不做）。
//
// 因此本包只产出证据，结论的处置（阻断、忽略、复核）留给调用方：page 模块的接入
// 点是「构建期记日志 + 巡检接口」，两者都不据此改产物字节、不改发布结果。
package compliance

import (
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/seo"
)

// Level 结论等级。
//
// error = 产物自相矛盾（不需要任何外部知识就能断定是错的：两条 canonical、
// JSON-LD 里的地址与 canonical 不同、noindex 却被收进 sitemap）；
// warn = 需要人工确认的偏离（canonical 指向别处 —— 可能是作者有意的跨域声明、
// 缺少 x-default、缺少结构化数据）。等级只描述「要不要立刻看」，不描述阻断与否。
type Level string

// 结论等级取值。
const (
	LevelError Level = "error"
	LevelWarn  Level = "warn"
)

// 规则 id。这些字符串是对外契约（巡检报告按它筛选、工单按它引用），不要随手改；
// 改名等于让历史巡检结论失去可比性。
const (
	// RuleArtifactEmpty 产物入口 HTML 为空（文件缺失 / 未落盘）。
	RuleArtifactEmpty = "artifact.empty"
	// RuleTitleMissing 没有 <title>。索引策略的前提是标题明确。
	RuleTitleMissing = "title.missing"
	// RuleCanonicalMissing 没有 canonical。
	RuleCanonicalMissing = "canonical.missing"
	// RuleCanonicalMultiple 多于一条 canonical（唯一性被破坏）。
	RuleCanonicalMultiple = "canonical.multiple"
	// RuleCanonicalExternal canonical 是跨域绝对地址（本页把权重让给别的站点）。
	RuleCanonicalExternal = "canonical.external"
	// RuleCanonicalPathMismatch canonical 指向的路径不是本产物的访问路径。
	RuleCanonicalPathMismatch = "canonical.path-mismatch"
	// RuleCanonicalLangMismatch canonical 的语言前缀与本产物的构建语言不符。
	RuleCanonicalLangMismatch = "canonical.lang-mismatch"
	// RuleLangPathMismatch 本产物的访问路径前缀与它的构建语言不符。
	RuleLangPathMismatch = "lang.path-mismatch"
	// RuleRobotsInvalid robots 指令出现白名单外的取值或自相矛盾。
	RuleRobotsInvalid = "robots.invalid"
	// RuleSitemapNoindexConflict noindex 的页面出现在 sitemap 收录清单里。
	RuleSitemapNoindexConflict = "sitemap.noindex-conflict"
	// RuleJSONLDUnparseable JSON-LD 不是合法 JSON（整份结构化数据失效）。
	RuleJSONLDUnparseable = "jsonld.unparseable"
	// RuleJSONLDMissing 有标题却没有页面级 JSON-LD。
	RuleJSONLDMissing = "jsonld.missing"
	// RuleJSONLDURLMismatch JSON-LD 的 url 与 canonical 指向不同地址。
	RuleJSONLDURLMismatch = "jsonld.url-mismatch"
	// RuleJSONLDTitleMismatch JSON-LD 的 name/headline 与 <title> 不一致。
	RuleJSONLDTitleMismatch = "jsonld.title-mismatch"
	// RuleHreflangDuplicateLang 同一 hreflang 语言出现多次。
	RuleHreflangDuplicateLang = "hreflang.duplicate-lang"
	// RuleHreflangSelfMissing 有互指组但缺少自指（本语言那条）。
	RuleHreflangSelfMissing = "hreflang.self-missing"
	// RuleHreflangUnknownLang 互指语言不在站点启用语言清单里。
	RuleHreflangUnknownLang = "hreflang.lang-unknown"
	// RuleHreflangXDefaultMissing 多语言互指组里没有 x-default。
	RuleHreflangXDefaultMissing = "hreflang.x-default-missing"
	// RuleHreflangNotReciprocal 被指向的页面没有回指（站点级，两侧都在巡检集合内才判）。
	RuleHreflangNotReciprocal = "hreflang.not-reciprocal"
	// RuleHreflangTargetLangMismatch 互指条目的语言码与目标页面实际构建语言不符（站点级）。
	RuleHreflangTargetLangMismatch = "hreflang.target-lang-mismatch"
)

// RuleIDs 全部规则 id 的登记清单。
//
// 存在的意义是防漂移：perArtifactChecks / siteChecks 是报告里「检查了多少条规则」的
// 分母，新增规则却忘了改计数，报告就会谎报覆盖面（绿得很像样，实际少跑一条）。
// 测试 TestRuleRegistryConsistent 把三者钉在一起。
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

// perArtifactChecks 单份产物上评估的规则条目数（Checks 计数用）。
//
// 写死成常量而不是运行期累加：巡检报告的「检查了多少条规则」必须与实现同步，
// 少算会让人以为规则跑了、实际没跑。
const perArtifactChecks = 18

// siteChecks 跨产物规则的条目数。
const siteChecks = 2

// LangRule 站点语言在 URL 层的形状。
//
// Code 是 hreflang 用的完整语言码（zh-CN / en-US），Prefix 是该语言的路径前缀
// （"" 表示不带前缀，常见于「默认语言不占前缀」的方案）。
type LangRule struct {
	Code   string
	Prefix string
}

// Artifact 被校验的一份构建事实。
//
// 字段全部来自产物本身或构建期已知的站点配置，不含任何运行时状态 ——
// 「同一输入同一结论」的前提就在这里：同样字节的产物 + 同样的语言表，
// 今天跑和半年后跑必须得出同一份结论。
type Artifact struct {
	// URL 产物的访问路径（来自 Artifact.CanonicalPath 或激活路由行）。
	URL string
	// Lang 本次构建的语言（来自 Manifest.lang；空 = 单语言 / 未接入语言）。
	Lang string
	// ArtifactHash 产物内容寻址哈希：巡检结论的可复核锚点 ——
	// 报告里的每一条结论都能靠它回到具体的产物字节。
	ArtifactHash string
	// HTML 产物入口 HTML 字节（唯一证据来源：校验读的是**实际产出**，不是设置项）。
	HTML []byte
	// Langs 站点启用语言与各自路径前缀。少于两种语言时跳过「语种 / 路径一致」两项校验
	// —— 单语言站点没有语言前缀这个概念，硬判只会产生噪声。
	Langs []LangRule
	// SitemapListed 本 URL 是否在站点 sitemap 的收录清单里。
	//
	// sitemap 由「已激活路径」生成（publication.RefreshSiteFiles），因此巡检激活面时
	// 这一项恒为真；留成显式字段是为了让调用方能如实回答「这份产物会不会被写进
	// sitemap」，而不是让本包去猜站点级文件的状态。
	SitemapListed bool
}

// IndexPolicy 从产物里解析出的索引策略（报告要求「索引策略明确」）。
type IndexPolicy struct {
	// Index 是否允许收录（robots 缺省与显式 index 都为 true）。
	Index bool
	// Follow 是否允许跟踪链接。
	Follow bool
	// Explicit 索引策略是否由 <meta name="robots"> 显式声明。
	//
	// 默认页面不输出该 meta（产物字节与「没有这个功能」时逐字节一致，确定性构建
	// 不变量），因此「没有 meta」不等于「策略未定义」：搜索引擎的默认就是
	// index,follow。这个字段让「未声明」与「显式声明为默认」在报告里可区分。
	Explicit bool
	// Raw meta 的原始 content（未声明时为空串）。
	Raw string
}

// Finding 一条结论。
type Finding struct {
	Rule string `json:"rule"`
	// Level error / warn（见 Level 的说明）。
	Level        Level  `json:"level"`
	URL          string `json:"url"`
	Lang         string `json:"lang,omitempty"`
	ArtifactHash string `json:"artifactHash,omitempty"`
	// Evidence 证据：实际观测到的字节 / 取值（截断到 maxEvidenceRunes）。
	Evidence string `json:"evidence"`
	// Expect 判据：这一条为什么算不合规。
	Expect string `json:"expect"`
}

// Report 单份产物的巡检结论。
type Report struct {
	URL          string      `json:"url"`
	Lang         string      `json:"lang,omitempty"`
	ArtifactHash string      `json:"artifactHash,omitempty"`
	Index        IndexPolicy `json:"indexPolicy"`
	// Checks 本次评估的规则条目数（含未命中的规则）。
	Checks int `json:"checks"`
	// Findings 命中的结论，按规则执行顺序排列（确定性输出）。
	Findings []Finding `json:"findings"`
}

// OK 是否没有 error 级结论。
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

// add 追加一条结论（证据统一截断，避免把整份 head 塞进响应）。
func (r *Report) add(a Artifact, rule string, level Level, evidence, expect string) {
	r.Findings = append(r.Findings, Finding{
		Rule: rule, Level: level, URL: a.URL, Lang: a.Lang,
		ArtifactHash: a.ArtifactHash,
		Evidence:     truncateEvidence(evidence),
		Expect:       expect,
	})
}

// maxEvidenceRunes 单条证据的最大字符数。
const maxEvidenceRunes = 200

func truncateEvidence(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if len([]rune(s)) <= maxEvidenceRunes {
		return s
	}
	return string([]rune(s)[:maxEvidenceRunes]) + "…"
}

// Inspect 对一份产物做确定性合规校验。
//
// 纯函数：不读盘、不查库、不联网、不读时钟。调用方负责把产物字节与站点语言表
// 取好传进来（page 模块的接入点见 service/page_seo_patrol.go）。
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

// ---------- 各规则实现 ----------

func checkTitle(rep *Report, a Artifact, doc headDoc) {
	if strings.TrimSpace(doc.Title) == "" {
		rep.add(a, RuleTitleMissing, LevelError,
			"head 中没有 <title> 或内容为空", "每份产物必须有非空 <title>（标题明确是索引策略的前提）")
	}
}

// checkCanonical 校验「唯一 canonical」，返回第一个非空 canonical（供后续规则复用）。
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
	// 跨域 canonical 单独成一条结论并终止后续的语言/路径比对：
	// 它的权威版本在别的站点，本站的路径与语言前缀约定都管不到它 —— 继续比对只会
	// 产生「路径不符」「语言不符」这类无意义的噪声，真正要人看的是「你把权重让给了谁」。
	if isExternal(canonical) {
		rep.add(a, RuleCanonicalExternal, LevelWarn,
			fmt.Sprintf("canonical=%s（跨域），产物访问路径=%s", canonical, a.URL),
			"跨域 canonical 会把本页的收录权重让给该地址，必须是有意为之")
		return canonical
	}
	// 路径一致：canonical 与产物实际访问路径必须指向同一条 URL（/index 与 / 视为同一
	// 条，口径取自 seo.CanonicalPublicPath，与构建期写 canonical 时同一份实现）。
	want := seo.CanonicalPublicPath(a.URL)
	got := seo.CanonicalPublicPath(pathOf(canonical))
	if got != want {
		rep.add(a, RuleCanonicalPathMismatch, LevelWarn,
			fmt.Sprintf("canonical=%s，产物访问路径=%s", canonical, a.URL),
			"canonical 应与产物自身的访问路径一致，否则本页在宣称自己住在另一个地址")
	}
	return canonical
}

// checkLangPath 语种 / 路径一致：本产物 URL 的语言前缀必须与它的构建语言相符。
func checkLangPath(rep *Report, a Artifact) {
	if len(a.Langs) < 2 {
		return
	}
	expected, ok := ruleForLang(a.Langs, a.Lang)
	if !ok {
		return // 语言不在站点清单里：由 hreflang.lang-unknown 报，这里不重复下结论。
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

// checkCanonicalLang canonical 的语言前缀必须与本产物语言一致。
//
// 跨域 canonical 跳过：它的权威版本在别的站点，本站的语言前缀约定管不到它
// （跨域这件事本身已在 canonical.path-mismatch 的 warn 里被提出）。
func checkCanonicalLang(rep *Report, a Artifact, canonical string) {
	if len(a.Langs) < 2 || canonical == "" {
		return
	}
	if isExternal(canonical) {
		return
	}
	expected, ok := ruleForLang(a.Langs, a.Lang)
	if !ok {
		return
	}
	got, matched := ruleForPath(a.Langs, seo.CanonicalPublicPath(pathOf(canonical)))
	if !matched || !strings.EqualFold(got.Code, expected.Code) {
		rep.add(a, RuleCanonicalLangMismatch, LevelError,
			fmt.Sprintf("构建语言=%s，canonical=%s", a.Lang, canonical),
			"canonical 必须指向本语言的地址：指向另一种语言等于把本页声明成它的副本")
	}
}

// checkRobots 索引策略明确：robots 取值必须在白名单内且不自相矛盾。
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

// checkSitemapConflict 索引策略与收录清单一致：noindex 的页面不应出现在 sitemap 里。
//
// 为什么这是矛盾而不是取舍：sitemap 由「已激活路径」全量生成（不读 robots），
// 于是 noindex 页面一边告诉搜索引擎「别收录我」、一边出现在收录邀请清单里，
// 两条指令互相抵消 —— 抓取预算被浪费在明确的非收录页上。
func checkSitemapConflict(rep *Report, a Artifact, policy IndexPolicy) {
	if policy.Index || !a.SitemapListed {
		return
	}
	rep.add(a, RuleSitemapNoindexConflict, LevelError,
		"robots=noindex 但该 URL 出现在激活清单（sitemap 由激活清单生成）",
		"noindex 的页面不应进 sitemap：两条指令互相抵消，抓取预算被浪费")
}

// checkJSONLD JSON-LD 可解析且与可见内容一致。
func checkJSONLD(rep *Report, a Artifact, raw, head string, doc headDoc, canonical string) {
	// 可解析性看整份文档：正文里的组件结构化数据（FAQ / 商品）同样是机器读取的断言，
	// 解析不了就是无效结构化数据。
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
	// 与内容一致：只认 head 里那份页面级主实体（由 builder 的 BuildSEOHead 产出）。
	// 正文里组件自产的结构化数据有自己的领域规则，把它们一并纳入等于把组件规则抄进本包。
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

// checkHreflang 互指组的内部一致性（自指、重复语言码、语言清单、x-default）。
//
// 这里只判「单份产物内部」的一致性；跨产物的互指（你指我、我没指你）在 InspectSite。
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
	// 自指缺失只在真的构成了「互指组」时才判：至少两条非 x-default 条目。
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
