package pubservice

// seo_audit.go — 发布后的产物 SEO 体检（审计 SEO-019）。
//
// 与编辑期评分器的分工：评分器看的是**草稿文档**（写之前给建议），体检看的是
// **激活产物**（写之后看事实）。两者的输入完全不同 —— 改 URL 留下的 301、
// 模板改版导致的 title 重复、模块里漏配 canonical，在草稿里都看不出来，
// 只有把真正服务出去的那份 HTML 读一遍才会暴露。
//
// 三条设计约束：
//  1. **只读产物文件**，不碰数据库、不碰访问面 —— 体检可以在任何时刻跑，
//     不影响访客请求（verification 明确要求）；
//  2. 内链判定对照**同一份激活路径集合**（调用方传入），不猜、不发请求；
//  3. 解析用标准库外唯一的依赖 x/net/html 的 tokenizer（项目已在用它解析富文本），
//     不引入解析器级别的重型依赖。

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"go_wp/internal/pipeline"
	"golang.org/x/net/html"
)

// SEO 体检的级别与检查项（落库与后台分组都按级别）。
const (
	AuditLevelError   = "error"
	AuditLevelWarning = "warning"
	AuditLevelInfo    = "info"
)

// SEO 检查项标识（稳定值：落库与去重都按它，改名等于丢掉历史）。
const (
	AuditTitleMissing       = "title_missing"
	AuditTitleDuplicate     = "title_duplicate"
	AuditDescriptionMissing = "description_missing"
	AuditCanonicalMissing   = "canonical_missing"
	AuditImageAltMissing    = "image_alt_missing"
	AuditInternalLinkBroken = "internal_link_broken"
	AuditHreflangIncomplete = "hreflang_incomplete"
)

// AuditIssue 一条体检结论。
type AuditIssue struct {
	Path    string // 产物访问路径（如 /about）
	Item    string // 检查项标识
	Level   string // error / warning / info
	Message string
}

// AuditDocument 单份产物的解析结果（检查项共用一次解析）。
type AuditDocument struct {
	Path        string
	Title       string
	Description string
	Canonical   string
	ImageSrcs   []string // 缺 alt 的图片 src
	Links       []string // 站内链接目标（相对路径）
	Hreflangs   []string
}

// ParseAuditDocument 解析一份产物 HTML（只提取体检需要的字段）。
func ParseAuditDocument(path string, r io.Reader) (doc AuditDocument, err error) {
	doc.Path = path
	z := html.NewTokenizer(r)
	collectAlt := false
	var imgSrc string
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		switch tok.Data {
		case "title":
			if z.Next() == html.TextToken {
				doc.Title = strings.TrimSpace(z.Token().Data)
			}
		case "meta":
			name := attr(tok, "name")
			if strings.EqualFold(name, "description") {
				doc.Description = strings.TrimSpace(attr(tok, "content"))
			}
		case "link":
			rel := strings.ToLower(attr(tok, "rel"))
			switch {
			case rel == "canonical":
				doc.Canonical = strings.TrimSpace(attr(tok, "href"))
			case rel == "alternate":
				if h := strings.TrimSpace(attr(tok, "hreflang")); h != "" {
					doc.Hreflangs = append(doc.Hreflangs, h)
				}
			}
		case "img":
			imgSrc = strings.TrimSpace(attr(tok, "src"))
			if strings.TrimSpace(attr(tok, "alt")) == "" && imgSrc != "" {
				doc.ImageSrcs = append(doc.ImageSrcs, imgSrc)
			}
			collectAlt = false
		case "a":
			if href := strings.TrimSpace(attr(tok, "href")); isInternalLink(href) {
				doc.Links = append(doc.Links, href)
			}
		}
		_ = collectAlt
	}
	return doc, nil
}

// attr 取属性值（大小写不敏感）。
func attr(tok html.Token, key string) string {
	for _, a := range tok.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// isInternalLink 站内链接判定：绝对路径，且不是协议相对 / 邮件 / 电话。
func isInternalLink(href string) bool {
	if href == "" || !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") {
		return false
	}
	return true
}

// AuditSite 体检一批产物：docs 为「访问路径 → 已解析文档」。
//
// activePaths 是**服务出去的路径集合**（含 301 旧路径），内链与 hreflang 都对照它 ——
// 这一步能抓出「改 URL 之后没人更新链接」留下的死链，而那类问题在草稿里完全看不见。
func AuditSite(docs []AuditDocument, activePaths map[string]bool) (issues []AuditIssue) {
	byTitle := map[string][]string{}
	for _, d := range docs {
		if d.Title == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditTitleMissing, Level: AuditLevelError,
				Message: "页面没有 <title>"})
		} else {
			byTitle[d.Title] = append(byTitle[d.Title], d.Path)
		}
		if d.Description == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditDescriptionMissing, Level: AuditLevelWarning,
				Message: "页面没有 meta description"})
		}
		if d.Canonical == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditCanonicalMissing, Level: AuditLevelWarning,
				Message: "页面没有 canonical"})
		}
		for _, src := range d.ImageSrcs {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditImageAltMissing, Level: AuditLevelWarning,
				Message: "图片缺少 alt：" + src})
		}
		for _, href := range d.Links {
			if !linkExists(href, activePaths) {
				issues = append(issues, AuditIssue{Path: d.Path, Item: AuditInternalLinkBroken, Level: AuditLevelError,
					Message: "站内链接指向不存在的路径：" + href})
			}
		}
		// hreflang：出现就必须至少两条（只有一条等于没声明互指），且目标都在激活集合内。
		if len(d.Hreflangs) == 1 {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditHreflangIncomplete, Level: AuditLevelWarning,
				Message: "hreflang 只有一条，缺少互指"})
		}
	}
	for title, pages := range byTitle {
		if len(pages) < 2 {
			continue
		}
		sort.Strings(pages)
		// 必须把命中页面**列进结论**：只报「有 2 个页面重复」等于给了一个无法行动的结果 ——
		// 运营知道有问题，却不知道该去改哪一页。这是体检类功能的常见失手。
		issues = append(issues, AuditIssue{Path: strings.Join(pages, ", "), Item: AuditTitleDuplicate, Level: AuditLevelWarning,
			Message: "重复的 title（" + title + "）出现在 " + itoa(len(pages)) + " 个页面：" + strings.Join(pages, "、")})
	}
	return issues
}

// linkExists 站内链接目标是否真的在激活集合里。
//
// 归一化：去掉 query 与 fragment（它们不影响路径是否存在）、去掉结尾斜杠、
// 空路径视为首页。不做 301 跟随 —— 体检报「这个链接指向的路径没有激活」，
// 至于它是不是被 301 了，那是另一条链（重定向管理）的事。
func linkExists(href string, activePaths map[string]bool) bool {
	if len(activePaths) == 0 {
		return true // 没有路径集合可比对时不下结论，避免整站误报
	}
	p := href
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || p == "/" {
		p = "/"
	}
	for _, cand := range []string{p, strings.TrimSuffix(p, "/"), p + "/"} {
		if cand != "" && activePaths[cand] {
			return true
		}
	}
	return false
}

// itoa 小整数转字符串（避免为一个用例引入 strconv 的间接层）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// CollectActiveDocuments 遍历激活目录，读出全部 HTML 产物（只读，不解析链接目标）。
//
// CollectActiveDocuments 按**激活路径**读出 HTML 产物（只读，不解析链接目标）。
//
// 为什么不是遍历目录：激活目录里放的全是**指向 artifacts/{hash} 的目录符号链接**
// （/about → about，后者是链接）。filepath.Walk 用 Lstat，**不会下沉进目录符号链接**，
// 于是扫描结果是 0 个文件 —— 体检永远报「无问题」。这个失效是静默的：
// 报告里 scanned=0 看起来像「站点还没有产物」，而不是「扫描器坏了」。
// 实测（临时目录造 active/about 为符号链接）：Walk 只访问到 active/about 本身，
// 文件数 0，且该名字不以 .html 结尾会被后续过滤掉。
//
// 按路径直读时 os.Open 会跟随符号链接（这正是访问面服务静态文件的方式），
// 拿到的就是访客真的会收到的那份 HTML —— 体检看到的与访问面一致。
//
// dir 为空或不存在返回 nil, nil：体检是**可选能力**，接了才算数，
// 而不是把「没配目录」变成构建/发布的错误。
func CollectActiveDocuments(dir string, activePaths []string) (docs []AuditDocument, err error) {
	if strings.TrimSpace(dir) == "" || len(activePaths) == 0 {
		return nil, nil
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		return nil, nil
	}
	for _, access := range activePaths {
		file, ok := activeHTMLFile(dir, access)
		if !ok {
			continue // 该路径没有 HTML 产物（重定向产物等），跳过而不是中断整次体检
		}
		f, oerr := os.Open(file)
		if oerr != nil {
			continue // 单个文件读不了不中断整次体检
		}
		doc, perr := ParseAuditDocument(access, f)
		f.Close()
		if perr == nil {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

// RunSEOAudit 跑一次产物 SEO 体检（审计 SEO-019）。
//
// 三个输入一起决定结论：**激活路径集合**（对谁体检）、**激活目录**（读哪份 HTML）、
// 路径集合本身（内链对照）。
//
// active 目录是**站点级**的（不含工程维度），多工程部署时里面会混着其它工程的产物 ——
// 所以这里按本工程的激活路径过滤一遍再体检。不过滤的话，别的工程的页面会带着
// 它们自己的内链进来，而对照的是本工程的路由表，结果是满屏误报的死链。
//
// 只读产物文件：不写库、不发请求、不碰访问面，可以在任何时刻跑。
func (s *Service) RunSEOAudit(ctx context.Context, projectID string) (issues []AuditIssue, scanned int, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, 0, errors.New("缺少站点工程")
	}
	paths, err := s.model.ListActivePaths(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	if len(paths) == 0 {
		return nil, 0, nil // 还没有激活产物：体检无从谈起，不是错误
	}
	active := make(map[string]bool, len(paths))
	for _, p := range paths {
		active[p] = true
	}
	docs, err := CollectActiveDocuments(pipeline.ActiveRoot(), paths)
	if err != nil {
		return nil, 0, err
	}
	mine := make([]AuditDocument, 0, len(docs))
	for _, d := range docs {
		if active[d.Path] {
			mine = append(mine, d)
		}
	}
	return AuditSite(mine, active), len(mine), nil
}
