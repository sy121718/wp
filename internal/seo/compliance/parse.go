package compliance

// parse.go — 产物 head 的确定性解析（只读字节，不做任何网络或库查询）。
//
// 为什么从 HTML 字节里解析、而不是把 builder 的 SEO 结构体传进来：校验的证据必须是
// **实际产出的字节**。传结构体等于再算一遍构建期的逻辑，两边一旦分叉（例如模板少输出
// 了一条标签），校验会跟着错的那份逻辑一起绿 —— 审计要的正是「证明产物本身正确」。

import (
	"encoding/json"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"

	"go_wp/internal/seo"
)

// 各标签的匹配式全部在包级编译一次（正则编译成本不便宜，且包级只读、天然并发安全）。
var (
	reHeadStart = regexp.MustCompile(`(?is)<head\b[^>]*>`)
	reHeadEnd   = regexp.MustCompile(`(?is)</head\s*>`)
	reTitleTag  = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)
	reLinkTag   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	reMetaTag   = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	reJSONLDTag = regexp.MustCompile(`(?is)<script\b[^>]*type\s*=\s*["']?application/ld\+json["']?[^>]*>(.*?)</script\s*>`)
	reTagAttr   = regexp.MustCompile(`(?is)([a-z_:][-a-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>]+))`)
)

// headDoc 从 <head> 解析出的原始事实。
type headDoc struct {
	Title      string
	Canonicals []string
	Robots     string
	Hreflangs  []hreflangRef
}

// hreflangRef 一条 rel=alternate hreflang 声明。
type hreflangRef struct {
	Lang string
	Href string
}

// headOf 截取 <head> 段（找不到闭合标签时回退整份文档）。
//
// 回退而不是报错：产物骨架由 document.jet 渲染，理论上必有 head；但若有人拿一段
// 片段来校验（测试、诊断），回退到整份文档仍能给出结论，比直接判「不合规」有用。
func headOf(raw string) string {
	start := reHeadStart.FindStringIndex(raw)
	if start == nil {
		return raw
	}
	rest := raw[start[1]:]
	end := reHeadEnd.FindStringIndex(rest)
	if end == nil {
		return rest
	}
	return rest[:end[0]]
}

// parseHead 解析 head 里的标题 / canonical / robots / hreflang（按出现顺序，确定性）。
func parseHead(head string) headDoc {
	var doc headDoc
	if m := reTitleTag.FindStringSubmatch(head); m != nil {
		doc.Title = html.UnescapeString(strings.TrimSpace(m[1]))
	}
	for _, tag := range reLinkTag.FindAllString(head, -1) {
		attrs := tagAttrs(tag)
		rel := attrs["rel"]
		switch {
		case hasToken(rel, "canonical"):
			doc.Canonicals = append(doc.Canonicals, html.UnescapeString(strings.TrimSpace(attrs["href"])))
		case hasToken(rel, "alternate"):
			lang := strings.TrimSpace(attrs["hreflang"])
			if lang == "" {
				continue // 没有 hreflang 的 alternate 是 RSS / 预加载之类，不参与互指校验
			}
			doc.Hreflangs = append(doc.Hreflangs, hreflangRef{
				Lang: html.UnescapeString(lang),
				Href: html.UnescapeString(strings.TrimSpace(attrs["href"])),
			})
		}
	}
	for _, tag := range reMetaTag.FindAllString(head, -1) {
		attrs := tagAttrs(tag)
		if strings.EqualFold(strings.TrimSpace(attrs["name"]), "robots") {
			doc.Robots = html.UnescapeString(strings.TrimSpace(attrs["content"]))
		}
	}
	return doc
}

// jsonLDScripts 按出现顺序取出全部 application/ld+json 脚本内容。
func jsonLDScripts(doc string) []string {
	matches := reJSONLDTag.FindAllStringSubmatch(doc, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// tagAttrs 解析一个标签里全部 name=value 属性（小写化属性名）。
//
// 手写一个最小解析器而不是引 HTML 解析库：产物骨架由本仓库的模板生成，形状可控；
// 引入解析库会把「解析器的容错行为」变成结论的一部分，而这里要的恰恰是可预测。
func tagAttrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range reTagAttr.FindAllStringSubmatch(tag, -1) {
		name := strings.ToLower(m[1])
		val := ""
		switch {
		case m[2] != "":
			val = m[2]
		case m[3] != "":
			val = m[3]
		default:
			val = m[4]
		}
		// 同名属性取首次出现：HTML5 标准对重复属性的处理就是「忽略后续」。
		if _, exists := out[name]; !exists {
			out[name] = html.UnescapeString(val)
		}
	}
	return out
}

// hasToken rel 属性可能是空格分隔的多个 token（如 "alternate canonical"）。
func hasToken(value, token string) bool {
	for _, f := range strings.Fields(value) {
		if strings.EqualFold(f, token) {
			return true
		}
	}
	return false
}

// pathOf 取 URL 的路径部分（相对地址原样返回；无法解析时返回原值）。
func pathOf(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") && !strings.Contains(s, "://") {
		// 形如 "about" 的裸路径：按站点内相对路径处理。
		return "/" + strings.TrimPrefix(s, "./")
	}
	u, err := url.Parse(s)
	if err != nil || u.Opaque != "" {
		return s
	}
	if u.Path == "" {
		return "/"
	}
	return u.Path
}

// isExternal 判断 canonical 是否超出可信站点基址；缺少基址时绝对地址保持待确认。
// 只接受配置中的 HTTP(S) origin，绝不从 HTML 或请求 Host 反推本站身份。
func isExternal(raw, base string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if u.Opaque != "" {
		return true
	}
	if u.Host == "" {
		return u.Scheme != "" // 非 HTTP 的绝对地址也不是本站路径。
	}
	if u.Scheme == "" {
		return true // 协议相对地址缺少可信协议，不能证实属于本站。
	}
	b, err := url.Parse(strings.TrimSpace(base))
	if err != nil || (b.Scheme != "http" && b.Scheme != "https") || b.Host == "" || b.User != nil || b.RawQuery != "" || b.Fragment != "" {
		return true
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil ||
		!strings.EqualFold(u.Scheme, b.Scheme) || !strings.EqualFold(u.Hostname(), b.Hostname()) || u.Port() != b.Port() {
		return true
	}
	prefix := strings.TrimRight(b.Path, "/")
	if prefix == "" {
		return false
	}
	// 路径边界以 URL 的转义形式判断，避免 %2F 冒充层级分隔符。
	if strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return true
	}
	clean := path.Clean(u.Path)
	return clean != prefix && !strings.HasPrefix(clean, prefix+"/")
}

// canonicalPathOf 剥离可信站点部署前缀后返回产物的逻辑路径。
func canonicalPathOf(raw, base string) string {
	path := pathOf(raw)
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || isExternal(raw, base) {
		return path
	}
	b, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return path
	}
	prefix := strings.TrimRight(b.Path, "/")
	if prefix != "" {
		path = strings.TrimPrefix(path, prefix)
		if path == "" {
			return "/"
		}
	}
	return path
}

// ruleForLang 取某语言在站点语言表里的条目（大小写不敏感）。
func ruleForLang(langs []LangRule, code string) (LangRule, bool) {
	c := strings.TrimSpace(code)
	if c == "" {
		return LangRule{}, false
	}
	for _, l := range langs {
		if strings.EqualFold(strings.TrimSpace(l.Code), c) {
			return l, true
		}
	}
	return LangRule{}, false
}

// ruleForPath 判断某个访问路径归属哪条语言规则。
//
// 取**最长非空前缀**匹配（/en/about 命中 /en 而不是空前缀）；没有任何非空前缀命中时，
// 落到「前缀为空」的那条规则（默认语言不带前缀的方案）。返回 ok=false 表示该路径
// 不属于任何已启用语言的形态。
func ruleForPath(langs []LangRule, path string) (LangRule, bool) {
	p := seo.CanonicalPublicPath(path)
	best := LangRule{}
	found := false
	for _, l := range langs {
		prefix := strings.TrimSpace(l.Prefix)
		if prefix == "" {
			continue
		}
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			if !found || len(prefix) > len(strings.TrimSpace(best.Prefix)) {
				best, found = l, true
			}
		}
	}
	if found {
		return best, true
	}
	for _, l := range langs {
		if strings.TrimSpace(l.Prefix) == "" {
			return l, true
		}
	}
	return LangRule{}, false
}

// mainEntities 从一份 JSON-LD 里取出「页面级主实体」节点。
//
// 站点级节点（Organization / WebSite / BreadcrumbList）不参与内容一致性比对：
// 它们的 name 是站点名、url 是站点根，与页面标题本来就不同。
func mainEntities(v any) []map[string]any {
	out := []map[string]any{}
	switch doc := v.(type) {
	case map[string]any:
		if graph, ok := doc["@graph"].([]any); ok {
			for _, item := range graph {
				if node, ok := item.(map[string]any); ok && !isSiteNode(node) {
					out = append(out, node)
				}
			}
			return out
		}
		if !isSiteNode(doc) {
			out = append(out, doc)
		}
	case []any:
		for _, item := range doc {
			if node, ok := item.(map[string]any); ok && !isSiteNode(node) {
				out = append(out, node)
			}
		}
	}
	return out
}

// isSiteNode 站点级节点类型（不参与「与页面内容一致」的比对）。
func isSiteNode(node map[string]any) bool {
	for _, t := range typeNames(node) {
		switch t {
		case "organization", "website", "breadcrumblist":
			return true
		}
	}
	return false
}

// typeNames 取节点的 @type（字符串或字符串数组，统一小写）。
func typeNames(node map[string]any) []string {
	switch t := node["@type"].(type) {
	case string:
		return []string{strings.ToLower(strings.TrimSpace(t))}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, strings.ToLower(strings.TrimSpace(s)))
			}
		}
		return out
	}
	return nil
}

// nodeString 取节点的字符串字段（非字符串或空串返回 ""）。
func nodeString(node map[string]any, key string) string {
	s, _ := node[key].(string)
	return strings.TrimSpace(s)
}

// containsStr 判断切片里是否包含某元素。
func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// jsonUnmarshalAny 解析 JSON 为通用值（保持与规则实现同一份解析口径）。
func jsonUnmarshalAny(data []byte) (any, error) {
	var v any
	err := json.Unmarshal(data, &v)
	return v, err
}
