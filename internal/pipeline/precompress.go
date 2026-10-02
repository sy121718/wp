// precompress.go —— 构建期预压缩（PIPE-GZ）：文本类产物在构建期多落一份 <name>.gz，
// 访问面命中即直出，省掉每请求的实时 gzip。
//
// 为什么值得做：HTML 的字节在构建期就已定型（同一 Page Document + BuildContext +
// Registry + Compiler ⇒ 同一份字节，AGENTS.md 不变量 5），而访问面此前是**纯实时压缩**
// （middleware/builtin/gzip.go）—— gzip 比一次 syscall 贵一个数量级，单核因此被锁在
// 千级 rps。构建是离线的一次性成本，把 CPU 花在那里，访客路径只剩一次 open/read。
//
// 为什么不交给 nginx 的 gzip_static 就完事：/site 面上挂着 AccessGuard（登录可见 /
// 密码保护）、SiteRedirect（redirect.json）与站点根目录映射三种语义，**必须保持 Go 直出**
// （docs/deploy-storage-nginx.md）。预压缩因此要在 Go 里做完，nginx 只是可选的第二层加速。
package pipeline

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"
)

// PrecompressedSuffix 预压缩产物的文件名后缀。
//
// 与明文**同目录同名**（index.html → index.html.gz）：产物目录是不可变的内容寻址目录，
// .gz 跟着同一个 {hash} 走，天然随明文一起激活 / 回滚 / 删除 —— 不需要在产物之外再维护
// 一份「预压缩缓存」，也就不存在「缓存与产物不同步」这一类故障。
//
// 单源：访问面中间件按本常量拼接查找路径；部署侧若额外启用 nginx 的 gzip_static，
// 它硬编码只认 ".gz"，与本值同。
const PrecompressedSuffix = ".gz"

// PrecompressMinSize 小于该字节数的条目不做预压缩。
//
// 与传输中间件的 gzipMinSize（middleware/builtin/gzip.go）同值：两边不一致的后果是
// 「构建期生成了、中间件却认为该实时压」，或反过来白压一堆小文件。真值对齐由
// middleware/builtin 的 TestPrecompressMinSizeAlignedWithGzipMiddleware 钉住。
//
// 判据是**明文长度**而不是压缩后长度：压缩后长度只有在压完之后才知道，拿它当阈值
// 等于每次都要先压一遍 —— 预压缩的全部收益就没了。
const PrecompressMinSize = 1024

// gzipOSUnknown gzip header 的 OS 字节（RFC 1952 §2.3.1）：255 = unknown。
//
// 必须显式写死。该字节的默认值取决于构建机器，Go 的 compress/gzip 当前固定填 255，
// 但那是实现细节而不是语言保证。不写死就等于「同一份文档在 A 机器与 B 机器上构建出
// 不同字节」—— 而失败方式是静默的：产物 hash 仍然是同一个（.gz 不进 hash），
// 只有逐字节比对才会发现。
const gzipOSUnknown = 255

// precompressibleExts 构建期预压缩的扩展名白名单（文本类）。
//
// 不含图片 / 字体 / 音视频：它们本身已是压缩格式，再 gzip 一次是纯 CPU 浪费，
// 产出的 .gz 常常比原文还大。png / jpg / webp / avif / woff2 / gz / br / zip
// 因此一律不在此表内（访问面走明文，实时压缩的类型白名单也不压它们）。
//
// 用扩展名而不是 Content-Type：产物条目的类型事实在**访问面**才被推出来
// （AssetContentType，同一份判据），构建期还没有可依赖的 Content-Type；
// 而扩展名在构建期就是确定的。两份判据都只看扩展名，因此不可能出现
// 「一个说 text/html、另一个说不压」这种分叉。
var precompressibleExts = map[string]struct{}{
	".html":        {},
	".htm":         {},
	".css":         {},
	".js":          {},
	".mjs":         {},
	".json":        {},
	".xml":         {},
	".svg":         {},
	".txt":         {},
	".webmanifest": {},
	".map":         {},
}

// IsPrecompressible 条目是否参与构建期预压缩（扩展名白名单 + 明文长度阈值）。
func IsPrecompressible(name string, size int) bool {
	if size < PrecompressMinSize {
		return false
	}
	_, ok := precompressibleExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

// GzipDeterministic 确定性 gzip 压缩：同一份输入，任何时刻、任何机器都产出同一份字节。
//
// 确定性来自三处显式固定，缺一不可：
//
//  1. ModTime = Unix(0,0) —— gzip header 的 MTIME 域。**不能留零值 time.Time{}**：
//     Go 会把它写成 uint32(时间零点的 Unix 秒) = 0x8873…（一个 2042 年的假时间戳），
//     虽然字节仍然是确定的，却违反 RFC 1952「MTIME = 0 表示无时间戳」的约定，
//     解压器会读出「这个文件修改于 2042 年」。
//  2. OS = gzipOSUnknown —— 见上。
//  3. Name / Comment / Extra 一律不设 —— 三者会把文件名（含路径）与任意元数据
//     烙进产物字节。这里不设置也不允许将来加。
//
// 压缩级别用 BestCompression：构建是离线的一次性成本，用 CPU 换体积是稳赚的买卖，
// 而访问面每次传输都要按体积付费。
func GzipDeterministic(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("gzip 压缩器初始化失败: %w", err)
	}
	zw.ModTime = time.Unix(0, 0)
	zw.OS = gzipOSUnknown
	if _, err = zw.Write(data); err != nil {
		return nil, fmt.Errorf("gzip 压缩写入失败: %w", err)
	}
	if err = zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip 压缩收尾失败: %w", err)
	}
	return buf.Bytes(), nil
}

// PrecompressEntries 就地派生预压缩条目：对 entries 里每个文本类文件写一份 <name>.gz。
//
// 调用方（NewArtifactWithEntries）**不得**把派生结果登记进 Manifest.Files：
//   - .gz 的全部信息都来自明文，而明文哈希已经在 Manifest.Files 里 —— 登记它等于
//     把同一件事记两份；
//   - 更实际的理由是：登记会让产物 hash 依赖**压缩库实现**。Go 升一个小版本换了
//     flate 的输出，全站产物 hash 就跟着一起变，触发全量重建（不变量 5 的可用性一面）。
//     预压缩是优化，优化不该改变「这份产物是什么」。
//
// 由此推出一条运维结论：**.gz 缺失不算产物损坏**。它由明文确定性派生，丢了可以随时从
// 明文重算（或直接删掉整个 .gz 集合，访问面回落实时压缩）。完整性校验（store.go
// VerifyArtifact / GetArtifact）与悬空链接巡检（publication.go AuditActiveLinks）
// 因此都不看 .gz —— 前者只认 index.html + manifest.json，后者只认符号链接及其可达性。
func PrecompressEntries(entries map[string][]byte) error {
	// 先取 key 快照再写回：Go 允许在迭代 map 时插入新键，但**新键可能被本轮回合访问、
	// 也可能不被访问**（规范明确不作保证）。直接边迭代边写，会得到「有时把刚生成的
	// .gz 又压一遍」这种不可复现的行为。
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	for _, name := range names {
		data := entries[name]
		if !IsPrecompressible(name, len(data)) {
			continue
		}
		gz, err := GzipDeterministic(data)
		if err != nil {
			// 不吞错：这条路径正常不可达（bytes.Buffer 不会写失败），但静默 continue
			// 会让「将来某次改动引入真错误」表现为「预压缩悄悄失效」—— 访问面照常工作
			// （回落实时压缩），没有任何信号。
			return fmt.Errorf("预压缩 %s 失败: %w", name, err)
		}
		entries[name+PrecompressedSuffix] = gz
	}
	return nil
}

// AssetContentType 访问面直出文件的 Content-Type。
//
// **单源**：明文路径（routers.serveArtifactFile）与预压缩路径
// （middleware/builtin 的 SiteGzipMiddleware）都走这里。两份判据漂移的表现是
// 「同一份产物、响应体字节一致、Content-Type 不同」—— 走 .gz 时被浏览器当附件下载，
// 而两条路径各自的测试都是绿的。
func AssetContentType(path string) string {
	name := strings.ToLower(path)
	ctype := mime.TypeByExtension(filepath.Ext(name))
	if strings.HasSuffix(name, ".html") {
		// mime.TypeByExtension 对 .html 返回的串取决于系统 mime.types，不保证带
		// charset；而构建期产物恒为 UTF-8，字符集必须确定。
		ctype = "text/html; charset=utf-8"
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	return ctype
}
