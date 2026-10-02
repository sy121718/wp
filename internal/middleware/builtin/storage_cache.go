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
// 未知路径（含应答 404 的）同样落 must-revalidate：不为「将来可能出现的新命名」
// 预先放宽 —— 规则判错的代价是多一次条件请求，而不是让访客永久看到陈旧字节。

import (
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

// StorageCacheMiddleware 为 /storage 下发缓存头：带指纹的变体永久 immutable，
// 其余（原图与一切未知路径）协商缓存。
func StorageCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cacheControl := storageCacheRevalidate
		// path.Base 而不是 filepath.Base：URL 路径恒以 "/" 分隔，
		// filepath 在 Windows 上会按 "\" 切分。
		if storageImmutableVariant.MatchString(path.Base(c.Request.URL.Path)) {
			cacheControl = storageCacheImmutable
		}
		c.Header("Cache-Control", cacheControl)
		c.Next()
	}
}
