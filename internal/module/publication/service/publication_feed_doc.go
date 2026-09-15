package pubservice

// publication_feed_doc.go — 产物 HTML 的 feed 元数据提取（审计 SEO-012）。
//
// 为什么读产物而不是查数据库：access 面只服务产物字节，产物因此是「这个页面对外
// 到底是什么」的权威事实 —— 标题、摘要、结构化数据类型都在里面。content 模块的
// contents.data 在这条链上取不到（publication 不跨模块查内容表，跨模块取数要加端口
// 并改进两处装配签名），而产物恰好是唯一不需要跨模块就能拿到的同一份事实。
//
// 与 seo_audit.go 的 ParseAuditDocument 的分工：那个解析器服务体检（title / meta /
// canonical / 内链），本解析器服务 feed（多了一项 JSON-LD @type，用来判断「这是不是
// 一篇文章」）。两者都用 x/net/html 的 tokenizer，不引入解析器级别的重型依赖。
//
// 一个已知边界：**SEO 头注入落地之前构建的旧产物没有 JSON-LD**，因此不会进 feed，
// 直到该页面重新发布（presentation 侧 presentation_seo.go 的注释同样说明了这一点）。

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// feedDoc 一份产物 HTML 里 feed 需要的元数据。
type feedDoc struct {
	// Title <title> 文本（产物的页面标题）。
	Title string
	// Description meta[name=description] 的内容。
	Description string
	// Canonical link[rel=canonical] 的目标（当前不参与 feed 输出，留作诊断）。
	Canonical string
	// Types JSON-LD 里的 @type 集合（可以是字符串或数组，@graph 递归展开）。
	Types []string
}

// feedArticleTypes 视为「文章」的结构化数据类型（小写比较）。
//
// 白名单而不是「含 article 字样」：report / blogposting 这些都合法，而任意后缀
// 匹配会把将来可能出现的无关类型也吞进来。schema.org 的前缀在比对前剥掉。
var feedArticleTypes = map[string]bool{
	"article": true, "newsarticle": true, "blogposting": true, "techarticle": true,
	"socialmediaposting": true, "report": true, "scholarlyarticle": true,
}

// isArticle 该产物是否为文章类详情页。
//
// 判据取构建期注入的 JSON-LD @type：presentation 侧的 applyEntitySEO 对
// entityType=article 写 schemaType=article，builder 再映射成 @type=Article。
// 不用「路径像不像文章」这类猜测 —— URL 规则是站的自由，产物里的类型是构建期事实。
func (d feedDoc) isArticle() bool {
	for _, t := range d.Types {
		name := strings.ToLower(strings.TrimSpace(t))
		name = strings.TrimPrefix(name, "https://schema.org/")
		name = strings.TrimPrefix(name, "http://schema.org/")
		if feedArticleTypes[name] || strings.HasSuffix(name, "article") {
			return true
		}
	}
	return false
}

// parseFeedDoc 从产物 HTML 里提取 feed 元数据（只读，不修改任何状态）。
func parseFeedDoc(r io.Reader) (doc feedDoc, err error) {
	z := html.NewTokenizer(r)
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
			// tokenizer 里 <title> 的内容是紧随其后的 TextToken。
			if doc.Title == "" && z.Next() == html.TextToken {
				doc.Title = strings.TrimSpace(z.Token().Data)
			}
		case "meta":
			if strings.EqualFold(attr(tok, "name"), "description") && doc.Description == "" {
				doc.Description = strings.TrimSpace(attr(tok, "content"))
			}
		case "link":
			if strings.EqualFold(attr(tok, "rel"), "canonical") && doc.Canonical == "" {
				doc.Canonical = strings.TrimSpace(attr(tok, "href"))
			}
		case "script":
			if !strings.EqualFold(strings.TrimSpace(attr(tok, "type")), "application/ld+json") {
				continue
			}
			if z.Next() != html.TextToken {
				continue
			}
			doc.Types = append(doc.Types, jsonLDTypes(z.Token().Data)...)
		}
	}
	return doc, nil
}

// jsonLDTypes 从一个 JSON-LD 块里取出全部 @type 值（数组与 @graph 递归展开）。
//
// 解析失败一律当作「没有类型」而不是错误：一份产物里的结构化数据坏掉不该让整次
// 发布失败，代价只是这个页面不进 feed（且它在 SEO 体检里会被别的手段发现）。
func jsonLDTypes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil
	}
	var out []string
	collectLDTypes(root, &out)
	return out
}

func collectLDTypes(node any, out *[]string) {
	switch v := node.(type) {
	case []any:
		for _, item := range v {
			collectLDTypes(item, out)
		}
	case map[string]any:
		if t, ok := v["@type"]; ok {
			collectLDTypes(t, out)
		}
		if g, ok := v["@graph"]; ok {
			collectLDTypes(g, out)
		}
	case string:
		if s := strings.TrimSpace(v); s != "" {
			*out = append(*out, s)
		}
	}
}

// readFeedDoc 读取并解析一份产物文件（打不开或读不出元数据时 ok=false）。
func readFeedDoc(file string) (doc feedDoc, ok bool) {
	f, err := os.Open(file)
	if err != nil {
		return feedDoc{}, false
	}
	defer f.Close()
	parsed, perr := parseFeedDoc(f)
	if perr != nil {
		return feedDoc{}, false
	}
	return parsed, true
}

// activeHTMLFile 站点路径 → 激活目录里的产物文件（找不到时 ok=false）。
//
// 激活目录的布局是「路径段 → 指向 artifacts/{hash} 的符号链接」（见
// pipeline.LocalPublicationStore 的 relActivePath：/about → about，"/" → index），
// 所以同一个路径有两种落点：链接本身是目录时，产物在它里面的 index.html；
// 链接本身是文件时（历史布局 /index.html），产物就是它自己。两种都试，不猜。
//
// 不遍历激活目录：active 里放的全是指向产物的符号链接，而 filepath.Walk 默认
// 不跟随目录符号链接 —— 遍历会一个产物也读不到（这正是按路径直读的理由，
// 见 seo_audit.go 的 CollectActiveDocuments 的同一处陷阱）。
func activeHTMLFile(dir, path string) (string, bool) {
	rel := strings.Trim(strings.TrimSpace(path), "/")
	// 空路径折算成 "index"，与 pipeline.relActivePath 的语义**逐字一致**：
	// 首页的激活条目是 <dir>/index（指向 artifacts/<hash>/ 的符号链接），
	// 而不是 <dir>/index.html。这里必须跟着那个定义走，不能自己发明一套形状。
	if rel == "" {
		rel = "index"
	}
	cands := []string{
		filepath.Join(dir, filepath.FromSlash(rel), "index.html"),
		filepath.Join(dir, filepath.FromSlash(rel)),
	}
	for _, cand := range cands {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, true
		}
	}
	return "", false
}
