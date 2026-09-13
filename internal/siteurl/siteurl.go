// Package siteurl 站点 URL 规则：把「实体 → 详情页路径」从各处硬编码收成一处。
//
// 为什么要有这个包：详情页路径此前散在各处写死 —— 商品侧 productDetailPath 写死 /products/、
// 文章侧的发布表单写死 /blog/ —— 既不能按站点改，也没法一眼回答「这个站的文章 URL 长什么样」。
//
// 对齐目标是 WordPress 的固定链接（Settings → Permalinks + 每篇可单独改）：
//
//   - **站点级结构**：SiteSettings.urlPatterns 按实体类型配置路径模板（默认为 DefaultPatterns），
//     模式里用 {slug} / {id} 占位；
//   - **每条可覆盖**：这里派生出来的只是**表单预填值**，发布时显式传入的路径永远优先 ——
//     派生是"帮你想好"，不是"替你决定"；
//   - **内容改、URL 不改**：路径一旦发布就独立于内容 —— 改标题、改正文、甚至改 slug 都不动它。
//     slug 是实体身份、路径是站点事实，两者解耦是刻意的（系统页面槽位同样绑 id 不绑 URL：
//     绑 URL 的东西会在改路径那一刻静默失效）。
//
// 本包是纯函数：无 IO、不读配置来源、不碰数据库 —— 站点配置由调用方传进来，
// 这样它既能被后台表单用，也能被将来的构建期/CLI 用。
package siteurl

import (
	"fmt"
	"strings"
)

// 实体类型键。与实体类型注册表的键一致（article 来自 content 模块，
// product* 来自 product 模块）—— 这里写字面量而不 import 那两个模块：
// 本包是被它们双方依赖的底层规则，反向依赖会成环。
const (
	KindArticle  = "article"
	KindProduct  = "product"
	KindCategory = "product_category"
	KindBrand    = "product_brand"
	KindTag      = "product_tag"
)

// 模式占位符。
const (
	PlaceholderSlug = "{slug}"
	PlaceholderID   = "{id}"
)

// DefaultPatterns 各实体类型的默认路径模板（站点没有配置该项时用）。
//
// 选这几个前缀不是"标准"，是给一个能直接用的起点：/blog/ 与 /products/ 是最常见的心智模型，
// 站点想换（比如 /news/ 或 /shop/）改配置即可 —— 这正是把模式做成配置的意义。
var DefaultPatterns = map[string]string{
	KindArticle:  "/blog/" + PlaceholderSlug,
	KindProduct:  "/products/" + PlaceholderSlug,
	KindCategory: "/category/" + PlaceholderSlug,
	KindBrand:    "/brand/" + PlaceholderSlug,
	KindTag:      "/tag/" + PlaceholderSlug,
}

// PatternOf 取某实体类型的路径模板：站点配置优先，未配置回落默认，都没有返回空串。
func PatternOf(kind string, patterns map[string]string) string {
	if p := strings.TrimSpace(patterns[kind]); p != "" {
		return p
	}
	return DefaultPatterns[strings.TrimSpace(kind)]
}

// DetailPath 按模式派生详情页路径（站点未配置该类型时用默认模式）。
//
// 占位符替换规则：
//   - {slug} 用实体的 slug；
//   - {id} 用实体 id；
//   - **slug 为空时回落用 id**：宁可得到一个不好看但唯一、可用的路径，
//     也不要拼出 /blog/ 这种"和列表页撞车"的路径 —— 后者会占用一个公共路径，
//     而且占用者自己也不知道出了什么事。
//
// 返回空串表示"这个类型没有模式可依"（调用方据此让用户自己填，而不是塞一个猜的路径）。
func DetailPath(kind, slug, id string, patterns map[string]string) string {
	pattern := PatternOf(kind, patterns)
	if pattern == "" {
		return ""
	}
	slug = strings.TrimSpace(slug)
	id = strings.TrimSpace(id)
	if slug == "" {
		slug = id
	}
	out := strings.ReplaceAll(pattern, PlaceholderSlug, slug)
	out = strings.ReplaceAll(out, PlaceholderID, id)
	return Normalize(out)
}

// Normalize 规范化路径：补前导 /、折叠重复 /、去掉尾部 /。
//
// 与 page 模块的 normalizePagePath 同一口径（那边是页面路径的唯一入口）：
// 两处不一致的话，同一个路径在页面侧与实例侧会被当成两个不同的事实。
func Normalize(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return strings.TrimRight(p, "/")
}

// MaxPatternLen 模式长度上限（够长以容纳分组前缀，又不至于让配置页失控）。
const MaxPatternLen = 120

// ValidatePattern 校验一条路径模式（设置页保存时用，拒绝明显写错的值）。
//
// 只拦"一定错"的：空值、不以 / 开头、含空白或查询串、字符集越界、
// **没有任何占位符**（那会让所有实体指向同一个路径，是配置事故而不是风格选择）。
// 不拦"可能不合意"的（前缀用 /news 还是 /blog 是站点的自由）。
func ValidatePattern(pattern string) error {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return fmt.Errorf("路径模式不能为空")
	}
	if len(p) > MaxPatternLen {
		return fmt.Errorf("路径模式过长（上限 %d 字符）", MaxPatternLen)
	}
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("路径模式必须以 / 开头")
	}
	if strings.ContainsAny(p, "?#") {
		return fmt.Errorf("路径模式不能包含 ? 或 #")
	}
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/' || r == '-' || r == '_' || r == '.':
		case r == '{' || r == '}':
		default:
			return fmt.Errorf("路径模式只能包含字母、数字与 - _ . / { }（含空格与中文都不行）")
		}
	}
	if !strings.Contains(p, PlaceholderSlug) && !strings.Contains(p, PlaceholderID) {
		return fmt.Errorf("路径模式必须包含 %s 或 %s，否则所有内容会指向同一个路径", PlaceholderSlug, PlaceholderID)
	}
	return nil
}

// KnownKinds 内置类型的展示名（设置页按这个顺序渲染，也用于文案）。
//
// 顺序稳定（切片而非 map 遍历）：设置页每次刷新看到的顺序应当一样。
var KnownKinds = []struct {
	Kind  string
	Label string
}{
	{KindArticle, "文章详情页"},
	{KindProduct, "商品详情页"},
	{KindCategory, "商品分类页"},
	{KindBrand, "品牌页"},
	{KindTag, "标签页"},
}
