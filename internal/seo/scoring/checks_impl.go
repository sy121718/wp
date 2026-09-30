package scoring

import (
	"regexp"
	"strings"
)

// checks_impl.go — 各检查项的纯函数实现（与 checks.go 的 Check 表一一对应）。

// ---------- title ----------

func chkTitlePresent(in *Input) (int, string) {
	n := len([]rune(strings.TrimSpace(in.Title)))
	if n == 0 {
		return 0, "未填写"
	}
	if n < 20 {
		return 2, fmtInt(n, "字符（偏短）")
	}
	return 5, fmtInt(n, "字符")
}

func chkTitleLength(in *Input) (int, string) {
	w := displayWidth(strings.TrimSpace(in.Title))
	switch {
	case w == 0:
		return 0, "未填写"
	case w >= titleWidthIdealMin && w <= titleWidthIdealMax:
		return 5, fmtInt(w, "宽度单位（理想区间）")
	case w > titleWidthIdealMax && w <= titleWidthHardMax:
		return 3, fmtInt(w, "宽度单位（略长）")
	case w < titleWidthIdealMin:
		return 2, fmtInt(w, "宽度单位（偏短）")
	default:
		return 0, fmtInt(w, "宽度单位（会被 SERP 截断）")
	}
}

func chkTitleKeywordFirstHalf(in *Input) (int, string) {
	if in.FocusKeyword == "" {
		return 0, "未设置主关键词"
	}
	t := strings.ToLower(strings.TrimSpace(in.Title))
	kw := strings.ToLower(in.FocusKeyword)
	idx := strings.Index(t, kw)
	if idx < 0 {
		return 0, "标题未含主关键词"
	}
	half := len([]rune(t)) / 2
	pos := len([]rune(t[:idx]))
	if pos <= half {
		return 5, fmtInt(pos, "字符处命中（前半段）")
	}
	return 2, fmtInt(pos, "字符处命中（后半段）")
}

// ---------- meta ----------

func chkMetaLength(in *Input) (int, string) {
	n := len([]rune(strings.TrimSpace(in.MetaDescription)))
	switch {
	case n == 0:
		return 0, "未填写"
	case n >= metaLenIdealMin && n <= metaLenIdealMax:
		return 3, fmtInt(n, "字符（理想区间）")
	case n > metaLenIdealMax:
		return 1, fmtInt(n, "字符（过长）")
	default:
		return 1, fmtInt(n, "字符（偏短）")
	}
}

var ctaRe = regexp.MustCompile("(?i)(shop|buy|learn|discover|get|order|start|read|explore|download|try|立即|选购|了解|查看|获取|开始|下载)")

func chkMetaCTA(in *Input) (int, string) {
	if strings.TrimSpace(in.MetaDescription) == "" {
		return 0, "未填写"
	}
	if ctaRe.MatchString(in.MetaDescription) {
		return 2, "含行动号召"
	}
	return 0, "缺少行动号召"
}

// ---------- headings ----------

func h1s(in *Input) []string {
	var out []string
	for _, h := range in.Headings {
		if h.Level == 1 {
			out = append(out, h.Text)
		}
	}
	return out
}

func chkSingleH1(in *Input) (int, string) {
	n := len(h1s(in))
	switch n {
	case 1:
		return 4, "1 个 H1"
	case 0:
		return 0, "缺少 H1"
	default:
		return 1, fmtInt(n, "个 H1（重复）")
	}
}

func chkH1Keyword(in *Input) (int, string) {
	hs := h1s(in)
	if len(hs) == 0 {
		return 0, "缺少 H1"
	}
	if in.FocusKeyword == "" {
		return 0, "未设置主关键词"
	}
	kw := strings.ToLower(in.FocusKeyword)
	for _, h := range hs {
		if strings.Contains(strings.ToLower(h), kw) {
			return 3, "H1 含主关键词"
		}
	}
	return 0, "H1 未含主关键词"
}

func chkHierarchy(in *Input) (int, string) {
	if len(in.Headings) == 0 {
		return 0, "无标题"
	}
	prev, jumps := 0, 0
	for _, h := range in.Headings {
		if h.Level < 1 || h.Level > 6 {
			continue
		}
		if prev > 0 && h.Level > prev+1 {
			jumps++
		}
		prev = h.Level
	}
	if jumps == 0 {
		return 3, "层级递进正常"
	}
	return 1, fmtInt(jumps, "处跳级")
}

// ---------- content ----------

func chkContentLength(in *Input) (int, string) {
	b, ok := contentLengthBenchmarks[in.Intent]
	if !ok {
		b = contentLengthBenchmarks[IntentInformational]
	}
	n := in.WordCount
	if n <= 0 {
		n = len([]rune(in.BodyText))
	}
	switch {
	case n >= b.Full:
		return 15, fmtInt(n, " 字/词（达标）")
	case n >= b.Partial:
		return 9, fmtInt(n, " 字/词（部分分）")
	case b.Partial == 0:
		return 0, fmtInt(n, " 字/词")
	default:
		return 15 * n / b.Partial / 2, fmtInt(n, " 字/词（偏薄）")
	}
}

func chkParagraphLength(in *Input) (int, string) {
	limit := paragraphWarnWords
	if isCJK(in.Locale) {
		limit = paragraphWarnCJK
	}
	paras := splitParagraphs(in.BodyText)
	if len(paras) == 0 {
		return 0, "无正文"
	}
	long := 0
	for _, p := range paras {
		if textUnitLen(p, in.Locale) > limit {
			long++
		}
	}
	switch {
	case long == 0:
		return 5, "段落长度正常"
	case long*2 <= len(paras):
		return 3, fmtInt(long, " 段过长")
	default:
		return 1, fmtInt(long, " 段过长（占比偏高）")
	}
}

func chkSentenceLength(in *Input) (int, string) {
	limit := sentenceWarnWords
	if isCJK(in.Locale) {
		limit = sentenceWarnCJK
	}
	sents := splitSentences(in.BodyText)
	if len(sents) == 0 {
		return 0, "无正文"
	}
	long := 0
	for _, s := range sents {
		if textUnitLen(s, in.Locale) > limit {
			long++
		}
	}
	pct := long * 100 / len(sents)
	switch {
	case pct <= 20:
		return 5, fmtInt(pct, "% 长句")
	case pct <= 40:
		return 3, fmtInt(pct, "% 长句")
	default:
		return 1, fmtInt(pct, "% 长句（偏高）")
	}
}

// ---------- keywords ----------

func chkKeywordDensity(in *Input) (int, string) {
	if in.FocusKeyword == "" {
		return 0, "未设置主关键词"
	}
	d := keywordDensity(in)
	if isCJK(in.Locale) && len([]rune(in.FocusKeyword)) < 2 {
		switch {
		case d == 0:
			return 0, "关键词未出现（短词密度可能不准）"
		case d <= densityWarnMax:
			return 6, fmtPct(d) + "（短关键词，密度仅供参考）"
		default:
			return 0, fmtPct(d) + "（短关键词，堆砌风险）"
		}
	}
	switch {
	case d == 0:
		return 0, "关键词未出现"
	case d < densityMinAllowed:
		return 6, fmtPct(d) + "（偏低）"
	case d <= densityFullMax:
		return 8, fmtPct(d)
	case d <= densityWarnMax:
		return 6, fmtPct(d) + "（偏高）"
	default:
		return 0, fmtPct(d) + "（堆砌）"
	}
}

func chkKeywordPositions(in *Input) (int, string) {
	if in.FocusKeyword == "" {
		return 0, "未设置主关键词"
	}
	kw := strings.ToLower(in.FocusKeyword)
	hit := 0
	if strings.Contains(strings.ToLower(in.Title), kw) {
		hit++
	}
	for _, h := range h1s(in) {
		if strings.Contains(strings.ToLower(h), kw) {
			hit++
			break
		}
	}
	if strings.Contains(strings.ToLower(firstWords(in.BodyText, 100, in.Locale)), kw) {
		hit++
	}
	if strings.Contains(strings.ToLower(in.URL), strings.ReplaceAll(kw, " ", "-")) {
		hit++
	}
	for _, img := range in.Images {
		if strings.Contains(strings.ToLower(img.Alt), kw) {
			hit++
			break
		}
	}
	if strings.Contains(strings.ToLower(in.MetaDescription), kw) {
		hit++
	}
	return hit * 5 / 6, fmtInt(hit, "/6 点位命中")
}

func chkSecondaryKeywords(in *Input) (int, string) {
	if len(in.SecondaryKeywords) == 0 {
		return 0, "未设置次级关键词"
	}
	body := strings.ToLower(in.BodyText)
	hit := 0
	for _, kw := range in.SecondaryKeywords {
		if kw != "" && strings.Contains(body, strings.ToLower(kw)) {
			hit++
		}
	}
	switch {
	case hit >= 2:
		return 2, fmtInt(hit, " 个次级词命中")
	case hit == 1:
		return 1, "1 个次级词命中"
	default:
		return 0, "次级词未出现"
	}
}

// ---------- links ----------

func chkInternalLinks(in *Input) (int, string) {
	n := in.WordCount
	if n <= 0 {
		n = len([]rune(in.BodyText))
	}
	b := internalLinkBenchmarks[0]
	for _, cand := range internalLinkBenchmarks {
		if n >= cand.MinWords {
			b = cand
		}
	}
	c := in.InternalLinks
	switch {
	case c == 0:
		return 0, "0 条内链"
	case c > b.TooMany:
		return 2, fmtInt(c, " 条（过多）")
	case c >= b.IdealMin && c <= b.IdealMax:
		return 6, fmtInt(c, " 条（理想）")
	case c >= b.Min:
		return 4, fmtInt(c, " 条（达标）")
	default:
		return 2, fmtInt(c, " 条（偏少）")
	}
}

func chkExternalLinks(in *Input) (int, string) {
	if in.ExternalLinks <= 0 {
		return 0, "0 条外链"
	}
	return 2, fmtInt(in.ExternalLinks, " 条外链")
}

var vagueAnchorRe = regexp.MustCompile("(?i)^(click here|here|read more|this|link|点击这里|这里|更多|详情)$")

func chkAnchors(in *Input) (int, string) {
	if len(in.AnchorTexts) == 0 {
		if noContent(in) {
			return 0, "无内容"
		}
		return 2, "无锚文本（不适用）"
	}
	bad := 0
	for _, a := range in.AnchorTexts {
		if vagueAnchorRe.MatchString(strings.TrimSpace(a)) {
			bad++
		}
	}
	if bad == 0 {
		return 2, "锚文本描述性良好"
	}
	return 0, fmtInt(bad, " 处模糊锚文本")
}

// ---------- images ----------

func contentImages(in *Input) []Image {
	var out []Image
	for _, img := range in.Images {
		if img.Kind == "icon" {
			continue
		}
		out = append(out, img)
	}
	return out
}

func chkAltCoverage(in *Input) (int, string) {
	imgs := contentImages(in)
	if len(imgs) == 0 {
		if noContent(in) {
			return 0, "无内容"
		}
		return 5, "无内容图（不适用）"
	}
	miss := 0
	for _, img := range imgs {
		if strings.TrimSpace(img.Alt) == "" {
			miss++
		}
	}
	if miss == 0 {
		return 5, "alt 全覆盖"
	}
	return 5 * (len(imgs) - miss) / len(imgs), fmtInt(miss, " 张缺 alt")
}

func chkImageWeight(in *Input) (int, string) {
	if len(in.Images) == 0 {
		if noContent(in) {
			return 0, "无内容"
		}
		return 3, "无图片（不适用）"
	}
	over, measured := 0, 0
	for _, img := range in.Images {
		if img.SizeKB <= 0 {
			continue
		}
		measured++
		limit := imageWeightKB[img.Kind]
		if limit == 0 {
			limit = imageWeightKB["content"]
		}
		if img.SizeKB > limit {
			over++
		}
	}
	// 一张图都没有体积数据时不判满分：原来直接落到「体积达标」，等于给所有页面
	// 无条件送一个满分项（该项永远不构成区分度）。
	if measured == 0 {
		return -1, "未纳入评分（缺少图片体积数据）"
	}
	if over == 0 {
		return 3, "体积达标"
	}
	return 1, fmtInt(over, " 张超体积")
}

func chkImageFormat(in *Input) (int, string) {
	imgs := contentImages(in)
	if len(imgs) == 0 {
		if noContent(in) {
			return 0, "无内容"
		}
		return 2, "无内容图（不适用）"
	}
	modern := 0
	for _, img := range imgs {
		l := strings.ToLower(img.Src)
		if strings.HasSuffix(l, ".webp") || strings.HasSuffix(l, ".avif") || strings.HasSuffix(l, ".svg") {
			modern++
		}
	}
	pct := modern * 100 / len(imgs)
	switch {
	case pct >= 80:
		return 2, fmtInt(pct, "% 现代格式")
	case pct >= 40:
		return 1, fmtInt(pct, "% 现代格式")
	default:
		return 0, fmtInt(pct, "% 现代格式（偏低）")
	}
}

// ---------- tech ----------

var urlBadRe = regexp.MustCompile("[A-Z?=&]")

func chkURLClean(in *Input) (int, string) {
	u := strings.TrimSpace(in.URL)
	if u == "" {
		return 0, "未设置 URL"
	}
	n := len([]rune(u))
	switch {
	case n <= 75 && !urlBadRe.MatchString(u):
		return 3, fmtInt(n, " 字符（干净）")
	case n <= 100:
		return 1, fmtInt(n, " 字符（含参数或大写）")
	default:
		return 0, fmtInt(n, " 字符（过长）")
	}
}

func chkCanonical(in *Input) (int, string) {
	if in.HasCanonical {
		return 3, "已配置"
	}
	return 0, "未配置"
}

func chkSchema(in *Input) (int, string) {
	if in.HasSchema {
		return 2, "已输出 JSON-LD"
	}
	return 0, "缺少结构化数据"
}

func chkHTTPS(in *Input) (int, string) {
	if in.IsHTTPS {
		return 2, "HTTPS"
	}
	return 0, "非 HTTPS"
}

// noContent 输入完全没有内容（用于区分"不适用"与"缺失"）。
func noContent(in *Input) bool {
	return strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.BodyText) == "" && len(in.Headings) == 0
}

// ---------- 文本工具 ----------

func fmtInt(n int, suffix string) string { return itoa(n) + suffix }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// splitParagraphs 切分正文段落。
//
// 按单换行切分并过滤空段：这样同时兼容「双换行分段」与「单换行分段」两种来源。
// 原实现只按双换行切，而富文本提取器产出的是单换行 —— 整篇正文被当成一段，
// 段落长度检查恒判「偏长」（1/5 分空转，与内容质量无关）。
func splitParagraphs(text string) []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var sentenceSplitRe = regexp.MustCompile("[.!?。！？；;]+")

func splitSentences(text string) []string {
	var out []string
	for _, p := range sentenceSplitRe.Split(text, -1) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// textUnitLen 文本长度单位：CJK 按字符，英文按词数。
func textUnitLen(text, locale string) int {
	if isCJK(locale) {
		return len([]rune(text))
	}
	return len(strings.Fields(text))
}

// firstWords 取正文前 n 个单位（CJK 按字符，英文按词）。
func firstWords(text string, n int, locale string) string {
	if isCJK(locale) {
		r := []rune(text)
		if len(r) > n {
			r = r[:n]
		}
		return string(r)
	}
	f := strings.Fields(text)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}

// needsComparisonTable 该意图是否要求结构化对比表。
//
// 只有比较型与购买型要求：它们的问题是「选哪个」，一张逐项对照的表是决策资产；
// 信息型/定义型的问题是「这是什么」，强制要表只会逼出规格堆砌 —— 那正是新规则里
// 明确排除的（"not a spec repeat"）。本地型同理不要求（它要的是门店/服务信息）。
func needsComparisonTable(intent QueryIntent) bool {
	return intent == IntentCommercial || intent == IntentTransactional
}

// chkComparisonTable 比较型 / 购买型意图下，正文是否给了结构化对比表。
//
// 返回负数 = 不适用（引擎据此跳过该项、不计入维度分母）：信息型 / 定义型 / 本地型，
// 以及**意图未知**（空）都不要求 —— 意图是从页面/商品元数据推断的，推不出来时
// 扣分是拿编辑者无法控制的东西罚他。
//
// 只判「有没有表」而不判「表好不好」：判定后者需要读懂表里在比什么（列是不是决策维度、
// 值能不能对照），那是人/模型的判断，不是纯函数能给的。规则原文的 "not a spec repeat"
// 因此留在提示文案里由人把关，评分只保证「有这个结构」。
func chkComparisonTable(in *Input) (int, string) {
	if !needsComparisonTable(in.Intent) {
		return -1, "不适用（该意图不要求对比表）"
	}
	if in.HasComparisonTable {
		return 5, "有结构化对比表"
	}
	return 0, "缺少对比表"
}
