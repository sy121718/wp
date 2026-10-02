package builtin

// storage_cache.go — 媒体上传存储（/storage）缓存策略。
//
// 背景：/storage 是三个静态面里唯一没下发 Cache-Control 的一个（/site 走
// must-revalidate、/static 走 no-cache），浏览器只能按 Last-Modified 做启发式
// 缓存 —— 缓存多久由实现自定，服务端不可控。
//
// 但 /storage 不能整体放宽：原图 URL（/storage/<id>.<ext>）与内容无关 ——
// 换图时 media_replace 保持文件名不变、原地替换字节。若给原图 immutable，
// 浏览器与 CDN 会在 max-age 内直接复用旧字节，且没有任何 URL 能让访客拿到新图，
// 换图对老访客等于永久不可见。所以按「URL 是否带内容指纹」二分：
//
//   - 带指纹的变体（<id>_<type>-<generation>-<hash8>.jpg）：文件名含内容哈希，
//     同一 URL 的字节在构造上不可变 —— 换图会产出新 hash、新文件名，
//     因此可 immutable 长缓存，访客与 CDN 都不必回源。
//   - 其余一切（原图、无指纹的旧格式变体 <id>_<type>.jpg、草稿、未知路径）：
//     URL 与内容解耦，只能协商缓存 —— max-age=0 + must-revalidate，
//     仍可命中 304，但每次使用前必须回源验证，保证换图后立即可见。
//
// 判据是两段，缺一不可：
//
//	① URL **形状**决定「能不能长缓存」（指纹段证明 URL 与内容绑定）；
//	② 真实**状态码**决定「这一条响应算不算命中了一个不变的字节」。
//
// 只看形状是不够的，因为这里并不确认文件存在：形状合法而文件不存在
//（旧变体已被清理、变体生成失败只留下 DB 行、手工拼出的 URL）会得到
// immutable 404 —— 而浏览器同样会把 404 钉住一年，且这个盲区在「形状命中」的
// 全部路径上都存在，不是罕见的边角。所以本中间件不再在 c.Next() 之前一次性落头，
// 而是先落保守值（must-revalidate），等状态码已知后**只把确认为 2xx 的那些**
// 升级为 immutable。
//
// 为什么不在这里补一次 os.Stat 确认文件存在：静态服务自己就要打开 / stat 一次文件
//（http.FileServer → http.Dir.Open），中间件再探一次等于把一次文件系统探测变成两次
//（热路径上的重复 IO，且与「省一次 syscall」的既有取舍冲突）。状态码是**已经发生
// 的事实**，零额外 IO —— 判据从「猜文件在不在」换成「看服务返回了什么」，更准也更便宜。

import (
	"net/http"
	"path"
	"regexp"

	"github.com/gin-gonic/gin"
)

const (
	storageCacheImmutable  = "public, max-age=31536000, immutable"
	storageCacheRevalidate = "public, max-age=0, must-revalidate"
)

// storageImmutableVariant 匹配带内容指纹的变体文件名：
// <词干>_<变体类型>-<generation>-<内容哈希前 8 位>.jpg。
//
// 为什么**不锚定词干必须是数字 id**：上传路径用主键命名原图（<id>.<ext>），
// 多数变体的词干因此是数字 —— 但存量附件里存在语义化原名的形态
// （见 media_reconcile.go 对「语义化原名」的说明），它们的变体同样带指纹、
// 同样不可变。判据是「文件名里有没有指纹段」，不是「词干长什么样」：
// 指纹段本身已经证明 URL 与内容绑定。
//
// 代价方向是刻意选的：漏判 = 一个真实变体每次回源验证（多一次条件请求）；
// 误判 = 一个可变 URL 被当不可变（老访客看旧图）。所以只认**完整指纹形状**
// （类型词 + generation + 8 位小写 hex 同时命中），不认任何一部分。
//
// 包级预编译：中间件在每个 /storage 请求上执行，逐请求 MustCompile 等于把正则
// 编译放进热路径。
var storageImmutableVariant = regexp.MustCompile(`_(?:thumb|small|medium|full)-\d+-[0-9a-f]{8}\.jpg$`)

// StorageCacheMiddleware 为 /storage 下发缓存头：
// 状态码 2xx 且 URL 带指纹的变体 → immutable，其余一律协商缓存。
func StorageCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// path.Base 而不是 filepath.Base：URL 路径恒以 "/" 分隔，
		// filepath 在 Windows 上会按 "\" 切分。
		if storageImmutableVariant.MatchString(path.Base(c.Request.URL.Path)) {
			// 先落保守值：handler 什么都不写（或直接 return）时也一定带着
			// Cache-Control，不会退回启发式缓存。升级只发生在下面确认的 2xx 上。
			c.Header("Cache-Control", storageCacheRevalidate)
			c.Writer = &storageCacheWriter{ResponseWriter: c.Writer}
		} else {
			// 形状不命中：不可能拿到 immutable，也就没有需要延迟的东西 ——
			// 不包装 Writer（这条路径零成本）。
			c.Header("Cache-Control", storageCacheRevalidate)
		}
		c.Next()
	}
}

// storageCacheWriter 把「形状命中指纹」的那些响应的缓存头延迟到状态码已知时再落。
//
// 只覆盖 WriteHeader / Write：http.FileServer 在写内容前一定会显式 WriteHeader
// （404 走 http.Error、200 走 serveContent），所以这两个入口足以看到真实状态码。
// 嵌入 gin.ResponseWriter，其余方法（Hijack / Flush / Status / Size …）原样透传。
type storageCacheWriter struct {
	gin.ResponseWriter
}

// WriteHeader 只在 2xx 上升级为 immutable：3xx 重定向与 4xx / 5xx 一律保持
// 已经落好的 must-revalidate —— 状态码与缓存时长的语义必须一致，
// 「这个地址将来可能有个真变体」不能成为给一次失败响应钉一年缓存的理由。
func (w *storageCacheWriter) WriteHeader(code int) {
	if isCacheableOK(code) {
		w.ResponseWriter.Header().Set("Cache-Control", storageCacheImmutable)
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write 覆盖隐式 200 的路径（handler 直接 Write 而不调 WriteHeader）：
// 此时 gin 的 Status() 仍是默认 200，而真写过 404 的响应 Status() 已经是 404 ——
// 用状态而不是「有没有调过 Write」判断，避免把 404 的响应体写入时误升级。
func (w *storageCacheWriter) Write(b []byte) (int, error) {
	if isCacheableOK(w.ResponseWriter.Status()) {
		w.ResponseWriter.Header().Set("Cache-Control", storageCacheImmutable)
	}
	return w.ResponseWriter.Write(b)
}

// isCacheableOK 只有 2xx 才是「真的命中了一个不变的字节」。
func isCacheableOK(code int) bool {
	return code >= http.StatusOK && code < http.StatusMultipleChoices
}
