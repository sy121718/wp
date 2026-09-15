package seo

// feed.go — 站点级 RSS 2.0 feed（审计 SEO-012）。
//
// 与 sitemap.go 的分工：sitemap 只声明「站点有哪些 URL」，feed 要声明「站点最近
// 发布了什么」—— 前者只需要路径，后者还需要标题、摘要与发布时间。因此两者的
// 输入不同（feed 的条目要读产物 HTML 的 <head>），但**链接的生成口径必须同源**：
// 两边都用 JoinURL(baseURL, path) 与激活路径原样拼装，不做第二套路径规则。
//
// 三条取舍：
//
//  1. **确定性输出**：条目按「发布时间降序 → 链接升序」排序，lastBuildDate 取
//     条目里的最大发布时间，而不是 time.Now()。同一批激活路由产生相同字节 ——
//     与 sitemap 同一条不变量（feed 每次发布都重写，若含当前时刻，字节比对永远
//     不相等，「这份 feed 到底变没变」也就无法回答）。
//
//  2. **channel 的 title / description 必填**：RSS 2.0 规范要求 channel 三个必需
//     元素（title / link / description）。空 title 的 feed 在各家阅读器里的表现是
//     「一堆没有名字的订阅源」，因此这里对空值做**兜底填 Link**，而不是输出空标签
//     让校验器去报错。
//
//  3. **开关与条数上限走环境变量**（GO_WP_SEO_FEED / GO_WP_SEO_FEED_LIMIT），
//     与 pkg/sitetz 的 GO_WP_SITE_TIMEZONE 同形：feed 是站点级开关，而它要写进
//     激活目录的那条链（page 发布 → RefreshSiteFiles）当前只接受「已激活路径 +
//     语言清单」这几个参数，加一个项目级设置要动 project / page 两个模块的签名。
//     环境变量先让能力可用且可关，项目级设置留给后续（见审计条目 SEO-012 备注）。

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FeedFileName 站点级 feed 的文件名（写在激活目录根，与 sitemap.xml 同级）。
const FeedFileName = "feed.xml"

// DefaultFeedLimit 站点级 feed 的默认条目上限。
const DefaultFeedLimit = 20

// feedEnabledEnv / feedLimitEnv feed 的开关与条数上限环境变量。
//
// 开关关闭的取值：off / 0 / false / no（大小写不敏感）—— 与部署脚本里常见的
// 布尔写法一致，不必为了关掉一个 feed 去记一个特殊的字面量。
const (
	feedEnabledEnv = "GO_WP_SEO_FEED"
	feedLimitEnv   = "GO_WP_SEO_FEED_LIMIT"
)

// FeedEnabled 报告是否生成站点级 feed（默认开启）。
func FeedEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(feedEnabledEnv))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// FeedLimit 站点级 feed 的条目上限（默认 DefaultFeedLimit）。
//
// 非法值（非数字 / 负数 / 零）回退默认而不报错：条数上限是展示口径，
// 一个填错的数字不该让发布链失败。
func FeedLimit() int {
	raw := strings.TrimSpace(os.Getenv(feedLimitEnv))
	if raw == "" {
		return DefaultFeedLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return DefaultFeedLimit
	}
	return n
}

// FeedChannel 站点级 feed 的元信息（RSS channel 的前三个必需元素 + 语言）。
type FeedChannel struct {
	Title       string
	Link        string
	Description string
	Language    string
}

// FeedItem 一条 feed 条目。
type FeedItem struct {
	Title       string
	Link        string // 绝对 URL（与 sitemap 的 loc 同源）
	Description string
	PubDate     time.Time // 零值 = 不输出 pubDate
}

// rssRoot / rssChannel / rssItem RSS 2.0 的 XML 结构。
//
// 用 encoding/xml 而不是拼字符串：feed 的 description 与 title 直接来自产物
// <head>（可能含 & < > 引号），转义规则交给编码器，少一处手写转义就少一处
// 「某个站点的标题里带了 &，整份 feed 解析不了」。
type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language,omitempty"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string  `xml:"title,omitempty"`
	Link        string  `xml:"link"`
	Description string  `xml:"description,omitempty"`
	PubDate     string  `xml:"pubDate,omitempty"`
	GUID        rssGUID `xml:"guid"`
}

// rssGUID 条目唯一标识：用绝对 URL 本身（isPermaLink=true）。
//
// 不另生成一份 id：条目链接已经由激活路径唯一决定，另造 id 只会引入
// 「同一个页面两条 guid」的可能，而阅读器把 guid 当身份 —— 重复订阅同一篇文章。
type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

// rfc822 是 RSS 的日期格式（RFC 822 / RFC 1123 的数值时区形态）。
const rfc822 = "Mon, 02 Jan 2006 15:04:05 -0700"

// BuildRSS 生成 RSS 2.0 内容（确定性：同输入同字节）。
//
// 过滤规则：链接为空的条目直接丢弃（没有链接的条目在阅读器里点不开，
// 是纯粹的噪声源）；链接重复的条目只保留第一条；超过 limit 条时取前 limit 条
// （limit <= 0 视为不限）。
func BuildRSS(ch FeedChannel, items []FeedItem, limit int) (string, error) {
	sorted := normalizeFeedItems(items, limit)
	ch = normalizeFeedChannel(ch)

	out := rssChannel{
		Title:       ch.Title,
		Link:        ch.Link,
		Description: ch.Description,
		Language:    ch.Language,
	}
	for _, it := range sorted {
		node := rssItem{
			Title:       it.Title,
			Link:        it.Link,
			Description: it.Description,
			GUID:        rssGUID{IsPermaLink: "true", Value: it.Link},
		}
		if !it.PubDate.IsZero() {
			node.PubDate = it.PubDate.UTC().Format(rfc822)
		}
		out.Items = append(out.Items, node)
	}
	// LastBuildDate 逐项取「字符串最大值」会随条目顺序变化，改为直接用最大时间。
	if max := maxFeedTime(sorted); !max.IsZero() {
		out.LastBuildDate = max.UTC().Format(rfc822)
	}

	body, err := xml.MarshalIndent(rssRoot{Version: "2.0", Channel: out}, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(body) + "\n", nil
}

// normalizeFeedChannel 补齐 channel 的必需字段（空 title / description 用 Link 兜底）。
func normalizeFeedChannel(ch FeedChannel) FeedChannel {
	ch.Title = strings.TrimSpace(ch.Title)
	ch.Link = strings.TrimSpace(ch.Link)
	ch.Description = strings.TrimSpace(ch.Description)
	ch.Language = strings.TrimSpace(ch.Language)
	if ch.Title == "" {
		ch.Title = ch.Link
	}
	if ch.Description == "" {
		ch.Description = ch.Title
	}
	return ch
}

// normalizeFeedItems 条目去重 + 排序（发布时间降序 → 链接升序）+ 截断。
//
// 排序键必须含第二维（链接）：同一批发布（同一秒内多条）在只按时间排序时顺序
// 取决于输入顺序，而输入顺序来自数据库的行序 —— 字节因此不稳定。
func normalizeFeedItems(items []FeedItem, limit int) []FeedItem {
	out := make([]FeedItem, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		link := strings.TrimSpace(it.Link)
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		it.Link = link
		it.Title = strings.TrimSpace(it.Title)
		it.Description = strings.TrimSpace(it.Description)
		out = append(out, it)
	}
	for i := 1; i < len(out); i++ { // 插入排序：条目数量级（≤ 上限）下足够
		for j := i; j > 0 && feedItemLess(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// feedItemLess 排序判据：发布时间新者在前；时间相同（含都为零值）按链接升序。
func feedItemLess(a, b FeedItem) bool {
	at, bt := a.PubDate.UTC(), b.PubDate.UTC()
	if !at.Equal(bt) {
		if at.IsZero() {
			return false
		}
		if bt.IsZero() {
			return true
		}
		return at.After(bt)
	}
	return a.Link < b.Link
}

// maxFeedTime 条目里的最大发布时间（全为零值时返回零值时间）。
func maxFeedTime(items []FeedItem) time.Time {
	var max time.Time
	for _, it := range items {
		if it.PubDate.After(max) {
			max = it.PubDate
		}
	}
	return max
}

// WriteFeed 把站点级 feed 写入 dir（覆盖写，原子替换）。
//
// dir 为空则跳过：与 WriteSiteFiles 同一约定 —— feed 是可选能力，
// 「没配激活目录」只表示这次调用不该写文件，而不是错误。
func WriteFeed(dir string, ch FeedChannel, items []FeedItem, limit int) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	body, err := BuildRSS(ch, items, limit)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, FeedFileName), []byte(body))
}

// RemoveFeed 删除已生成的站点级 feed（幂等：文件不存在不算错）。
//
// 关闭开关时必须删除而不是「不再重写」：激活目录是访问面直接服务的目录，
// 不重写等于旧 feed 继续对外服务，而条目指向的页面可能早已下线 ——
// 订阅者会持续收到 404 链接，且从后台看不出任何异常。
func RemoveFeed(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	err := os.Remove(filepath.Join(dir, FeedFileName))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
