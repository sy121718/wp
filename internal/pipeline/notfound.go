package pipeline

// notfound.go — 站点自定义 404 页（审计 SEO-013）在激活目录里的位置与读写。
//
// 为什么放在 pipeline：访问面（/site 静态服务）与控制面（发布时写入）需要同一份
// 位置约定，与 ActiveRoot 同源。两边各写一份路径字符串，分叉的表现是
// 「后台配了 404 页、线上永远不生效」这种静默失效 —— 与本包已有单源约定同一理由。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NotFoundFileName 自定义 404 页在激活目录根的文件名。
//
// 与 sitemap.xml / robots.txt / feed.xml 同级：它们都是「写在激活目录根的站点级
// 真实文件」，而不是指向 artifacts 的激活链接（后者一根路径一个产物，
// 条目名即 URL 路径）。
const NotFoundFileName = "404.html"

// siteRootFileNames 允许以**真实文件**（非符号链接）出现在激活目录根的站点级产物。
//
// 为什么需要白名单：激活目录的常规内容是「一根路径一个符号链接」
// （见 relActivePath），但站点级文件没有对应的站点路径 —— sitemap.xml 不是
// 「某个页面的产物」，只能以真实文件落地。AuditActiveLinks 的判据是
// 「非符号链接即异常」，没有这份白名单，这些文件每次审计都被报成 issue，
// 噪音会淹没真正的失联链接（那才是审计要发现的东西）。
//
// 名字在这里写字面量而不 import internal/seo：依赖方向是 seo → pipeline
// （seo 的站点语言规则复用 pipeline.LangURLRule），反向 import 即成环。
var siteRootFileNames = map[string]struct{}{
	"sitemap.xml":    {}, // internal/seo/sitemap.go
	"robots.txt":     {}, // internal/seo/sitemap.go
	"feed.xml":       {}, // internal/seo/feed.go（FeedFileName）
	NotFoundFileName: {},
}

// IsSiteRootFile 判断激活目录内的相对路径是否为白名单内的站点级真实文件。
//
// 只认**根层**：白名单说的是「激活目录根的文件」，深层同名条目（如 /docs/404.html）
// 是页面产物或异常占位，不属于站点级文件。
func IsSiteRootFile(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || strings.Contains(rel, "/") {
		return false
	}
	_, ok := siteRootFileNames[rel]
	return ok
}

// NotFoundPagePath 返回自定义 404 页在激活目录中的绝对路径。
func NotFoundPagePath(activeRoot string) string {
	return filepath.Join(activeRoot, NotFoundFileName)
}

// ReadNotFoundPage 读取激活目录根的自定义 404 页。
//
// ok=false 表示未配置（activeRoot 为空、文件不存在或内容为空）：访问面必须退回
// 原有 404 行为，而不是返回一个空 HTML —— 后者在浏览器里是白屏，比默认提示更糟。
func ReadNotFoundPage(activeRoot string) (body []byte, ok bool) {
	if strings.TrimSpace(activeRoot) == "" {
		return nil, false
	}
	data, err := os.ReadFile(NotFoundPagePath(activeRoot))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// RemoveNotFoundPage 删除激活目录根的自定义 404 页（幂等：未配置时返回 nil）。
func RemoveNotFoundPage(activeRoot string) error {
	if strings.TrimSpace(activeRoot) == "" {
		return nil
	}
	if err := os.Remove(NotFoundPagePath(activeRoot)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SyncNotFoundPage 把站点自定义 404 页同步到激活目录根。
//
// html 为空 = 站点未配置自定义 404 页：**删除既有文件**（而不是不写）。
// 激活目录直接对外服务，不删等于继续把已下线的旧页面当 404 响应体返回 ——
// 与 feed 开关关闭即删除同一约定（internal/seo/feed.go 的 RemoveFeed）。
//
// 写入走「临时文件 + rename」：直接 os.WriteFile 中途崩溃会留下截断的 HTML，
// 而它正是所有死链的响应体。激活目录不存在时按幂等成功处理 —— 从未发布过的
// 站点没有访问面可服务，不该让发布失败在这种可自愈的状态上。
func SyncNotFoundPage(activeRoot, html string) error {
	if strings.TrimSpace(activeRoot) == "" {
		return nil
	}
	if strings.TrimSpace(html) == "" {
		return RemoveNotFoundPage(activeRoot)
	}
	if _, serr := os.Stat(activeRoot); serr != nil {
		if os.IsNotExist(serr) {
			return nil
		}
		return serr
	}
	target := NotFoundPagePath(activeRoot)
	tmp := target + ".tmp"
	if werr := os.WriteFile(tmp, []byte(html), 0o644); werr != nil {
		return fmt.Errorf("写入自定义 404 页失败: %w", werr)
	}
	if rerr := os.Rename(tmp, target); rerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("落盘自定义 404 页失败: %w", rerr)
	}
	return nil
}
