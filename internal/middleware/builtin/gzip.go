package builtin

// StaticGzipMiddleware 静态资源传输压缩中间件（手写实现，无第三方依赖）。
//
// 挂载对象：/static 后台资源与 /site 静态访问面（文本类产物）。
// 特性：
//   - 仅对 GET 且客户端 Accept-Encoding 含 gzip 的请求生效；
//   - 按 Content-Type 白名单压缩（文本类，跳过已压缩的二进制：png/jpg/woff2 等）；
//   - 已知 Content-Length 且小于阈值（1KB）时不压缩——小文件压缩负收益；
//   - gzip.Writer 走 sync.Pool 复用，避免每请求分配；
//   - 压缩时移除 Content-Length（改 chunked）、写 Content-Encoding: gzip 与 Vary 头。
//
// 实现要点：Gin 的 render（如 Data）在 WriteHeader 之后才设置 Content-Type，
// 因此压缩决策延迟到第一次 Write（此时 Header 尚未真正发送，可安全改写）。
//
// 不变量说明：仅作用于传输层编码，响应语义不变；访问面「只读静态直出」
// 不受影响（文件系统不变，只是字节流经 gzip 传输）。
//
// 本文件有两个入口，按挂载点选：
//   - StaticGzipMiddleware —— 纯实时压缩，挂 /static（后台静态资源，无构建期产物）；
//   - PrecompressedAssetMiddleware —— 访问面的预压缩层，命中构建期生成的 <文件>.gz 就
//     直接出，未命中**完全透明**（把响应原样留给链上后一个 StaticGzipMiddleware 实时压缩）。
//     见本文件末尾「访问面预压缩直出」一节。

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"go_wp/internal/pipeline"
)

// gzipMinSize 小于该字节数不压缩（gzip 头尾 + 小文件无收益）。
const gzipMinSize = 1024

// gzipPool 复用 gzip.Writer（Reset 到每次响应的目标 writer）。
var gzipPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

// StaticGzipMiddleware 见文件头注释。
func StaticGzipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		w := &gzipResponseWriter{
			ResponseWriter: c.Writer,
			accepts:        c.Request.Method == http.MethodGet && acceptsGzip(c.Request.Header.Get("Accept-Encoding")),
		}
		c.Writer = w
		defer w.close()
		c.Next()
	}
}

// gzipResponseWriter 延迟压缩包装：
// WriteHeader 只记录状态码（不转发），第一次 Write/Flush 时依据已就绪的
// Content-Type 决定是否压缩，再转发 WriteHeader + 数据。
type gzipResponseWriter struct {
	gin.ResponseWriter
	zw         *gzip.Writer
	status     int
	started    bool
	compressed bool
	accepts    bool
}

// WriteHeader 记录状态码，延迟转发（Header 在首次 Write 时真正发送）。
func (w *gzipResponseWriter) WriteHeader(code int) {
	if !w.started {
		w.status = code
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

// start 首次写前决定压缩并转发 WriteHeader（幂等）。
//
// 仅 200 压缩：206 Partial Content 的 body 是字节范围的原始切片（Range 请求，
// 视频拖动/断点下载），压缩会破坏 Content-Range 语义；304/204 无 body。
func (w *gzipResponseWriter) start() {
	if w.started {
		return
	}
	w.started = true
	if w.status == 0 {
		w.status = http.StatusOK
	}
	// identity、HEAD、304 同样参与编码协商；只在 gzip 分支加 Vary 会污染共享缓存。
	addVaryAcceptEncoding(w.Header())
	if w.accepts && w.status == http.StatusOK && shouldCompressGzip(w.Header()) {
		w.compressed = true
		zw := gzipPool.Get().(*gzip.Writer)
		zw.Reset(w.ResponseWriter)
		w.zw = zw
		h := w.Header()
		h.Set("Content-Encoding", "gzip")
		// 压缩后长度未知：移除 Content-Length，走 chunked。
		h.Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(w.status)
}

// Write 压缩路径下写入 gzip 流，否则直写。
func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	w.start()
	if w.compressed {
		return w.zw.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// WriteString 同 Write（gin 接口成员）。
func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	w.start()
	if w.compressed {
		return w.zw.Write([]byte(s))
	}
	return w.ResponseWriter.WriteString(s)
}

// WriteHeaderNow 无 body 状态码路径（204/304 等）：补一次启动转发。
func (w *gzipResponseWriter) WriteHeaderNow() {
	w.start()
}

// Flush 转发；静态文件服务（http.ServeContent）不依赖 Flush 语义。
func (w *gzipResponseWriter) Flush() {
	w.start()
	if w.compressed {
		_ = w.zw.Flush()
	}
	w.ResponseWriter.Flush()
}

// close 补转发未启动的响应（纯状态码），结束 gzip 流并归还池。
func (w *gzipResponseWriter) close() {
	w.start()
	if w.compressed && w.zw != nil {
		_ = w.zw.Close()
		gzipPool.Put(w.zw)
		w.zw = nil
	}
}

// Unwrap 让零拷贝穿透继续向下（见 sendfile.go 的 sfZeroCopyTarget）。
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ReadFrom 覆盖 http.ServeContent 的零拷贝分支。
//
// 【为什么必须由本层实现，不能让外层直接对底层走零拷贝】
// 本层是**延迟压缩**包装：要不要压缩、Content-Length 删不删、Vary 加不加、
// Content-Encoding 设不设，全部推迟到第一次写数据前的 start() 里决定。外层若绕过
// 本层直通底层，这些决定一件都不会发生 —— 结果是「响应体是明文却带着
// Content-Encoding: gzip」或者反之。所以接缝必须在本层：先完成决策，再按决策分流。
//
// 【为什么两种分支的处理方式不同】
//   - 压缩分支：字节要进 gzip.Writer，零拷贝在语义上就不成立，走普通拷贝。
//   - 非压缩分支：字节可以原样进内核，此时才转交下层去吃 sendfile。
//
// 【为什么必须先 sfCommitHeader 再转交】
// start() 只是把状态码转发给 gin 的 writer，而 gin 的 WriteHeader **只记录不写**。
// 不把它真正落到连接上，直通底层的 (*http.response).ReadFrom 会用「header 出去了吗」
// 这个标志做判断，两个后果同时发生：按**隐式 200** 写头（206 Partial Content 会变成
// 200，与 Content-Range 自相矛盾），以及小于 512 字节的响应被嗅探分支提前返回、
// 零拷贝静默失效。完整论证见 sendfile.go 的 sfCommitHeader。
// 注意要下钻到内层 —— 本层自己的 WriteHeaderNow 也只是调 start()。
//
// 判定 `w.compressed` 与 Write 完全同源（同一个 start() 决定），
// 否则同一份产物在「整体走 ReadFrom」与「分块走 Write」两条路径上会得到不同的头。
func (w *gzipResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	w.start()
	if w.compressed {
		return io.Copy(sfWriterOnly{w}, r)
	}
	sfCommitHeader(w.ResponseWriter)
	return sfForward(w.ResponseWriter, r)
}

// acceptsGzip 客户端是否接受 gzip 编码。
func acceptsGzip(header string) bool {
	wildcard := false
	for _, part := range strings.Split(header, ",") {
		enc := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if !strings.EqualFold(enc, "gzip") && enc != "*" {
			continue
		}
		_, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		quality := 1.0
		if q, ok := params["q"]; ok {
			quality, err = strconv.ParseFloat(q, 64)
		}
		accepted := err == nil && quality > 0 && quality <= 1
		if strings.EqualFold(enc, "gzip") {
			return accepted // 显式 gzip（包括 q=0）优先于通配符。
		}
		wildcard = accepted
	}
	return wildcard
}

func addVaryAcceptEncoding(h http.Header) {
	for _, value := range h.Values("Vary") {
		for _, field := range strings.Split(value, ",") {
			if field = strings.TrimSpace(field); field == "*" || strings.EqualFold(field, "Accept-Encoding") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}

// shouldCompressGzip 响应头满足压缩条件：未设置过编码、文本类 Content-Type、
// 且（无 Content-Length 或长度达到阈值）。
func shouldCompressGzip(h http.Header) bool {
	if h.Get("Content-Encoding") != "" {
		return false
	}
	ct := h.Get("Content-Type")
	if ct == "" || !isGzipType(ct) {
		return false
	}
	if len := h.Get("Content-Length"); len != "" {
		n, err := strconv.Atoi(len)
		if err != nil || n < gzipMinSize {
			return false
		}
	}
	return true
}

// isGzipType Content-Type 是否属于可压缩文本类。
func isGzipType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mt = strings.ToLower(mt)
	switch {
	case strings.HasPrefix(mt, "text/"):
		return true
	case strings.HasPrefix(mt, "application/javascript"),
		strings.HasPrefix(mt, "application/x-javascript"):
		return true
	case mt == "application/json",
		mt == "application/xml",
		mt == "application/wasm",
		mt == "image/svg+xml",
		mt == "font/ttf",
		mt == "font/otf",
		mt == "application/vnd.ms-fontobject":
		return true
	}
	return false
}

// ===========================================================================
// 访问面预压缩直出（PIPE-GZ）
// ===========================================================================
//
// 上面的 StaticGzipMiddleware 是**纯实时压缩**：每个请求现场跑一遍 gzip。对 HTML 这类
// 构建期就已经定型的字节，这份 CPU 是纯浪费 —— 产物落盘时压一次，访问面只剩一次
// open/read。预压缩产物的生成见 internal/pipeline/precompress.go。

// PrecompressedAssetMiddleware 访问面（/site 与站点独占域名根）的预压缩层：
// **优先直出构建期预压缩产物 `<实际文件>.gz`**。
//
// 它是链上 StaticGzipMiddleware **之前**的一层，而不是替代它：命中就 Abort（后面的
// 实时压缩与静态处理都不跑），未命中**完全透明** —— 不包装 c.Writer、不改任何头、
// 不读字节，把响应原样交给后一层。两层职责因此各只有一句：
// 「有没有现成的 .gz」在预压缩层，「没有时怎么压」在实时压缩层。
//
// 为什么不与 StaticGzipMiddleware 合并成一个构造：预压缩查找需要「URL → 实际文件」
// 这条映射，而它只在访问面成立。若共用一个构造，挂在 /static 面的那个实例会把
// /static/about 这类本来 404 的路径解析成站点产物并直出 —— 404 变 200。
//
// prefix 是访问面挂载前缀（站点独占域名根为空串，控制台相容入口为 "/site"），
// 与 SiteRedirectMiddleware / AccessGuardMiddleware / siteDirIndexServeMiddleware 同参。
//
// **未命中是功能保证，不是异常**：.gz 会因为阈值（明文字节 < pipeline.PrecompressMinSize）、
// 类型白名单（非文本类）与历史产物（先于本特性发布，而 PutArtifact 的幂等分支不会补写）
// 而缺失。这三类都不影响正确性 —— 实时压缩接管，响应语义完全一致。
func PrecompressedAssetMiddleware(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			return
		}
		// 命中时响应已写完并 Abort（gin 的链由最外层那一次 Next 的循环驱动，Abort 把
		// index 置到 abortIndex，后续中间件与静态处理器都不会执行）；未命中时本函数
		// 什么都不做就返回，后面的 StaticGzipMiddleware 照常接管。
		if servePrecompressed(c, prefix) {
			return
		}
	}
}

// servePrecompressed 尝试直出预压缩产物；命中返回 true（响应已写完并 Abort）。
func servePrecompressed(c *gin.Context, prefix string) bool {
	// 协商不满足：原样交回后面的链路（明文或实时压缩）。这是「访问面只读静态直出」的
	// 底线 —— 任何时候都不能把 gzip 字节发给一个没说要 gzip 的客户端。
	if !acceptsGzip(c.Request.Header.Get("Accept-Encoding")) {
		return false
	}
	// Range 请求不走预压缩：206 的 Content-Range 描述的是**所选表示**的字节范围，
	// 用 .gz 的字节去满足一个本意针对明文的 Range，分片语义就与明文对不上了。
	// 交回原路径（明文 + 206，不压缩）与改造前逐字节一致。
	if c.Request.Header.Get("Range") != "" {
		return false
	}
	file, ok := siteAssetFile(prefix, c.Request.URL.Path)
	if !ok {
		return false
	}
	f, err := os.Open(file + pipeline.PrecompressedSuffix)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return false
	}

	h := c.Writer.Header()
	h.Set("Content-Encoding", "gzip")
	// Content-Type 取**明文**文件的类型：gzip 是内容编码层，不改媒体类型。
	// 判据与明文路径同源（pipeline.AssetContentType），两条路径不会分叉。
	h.Set("Content-Type", pipeline.AssetContentType(file))
	// Vary 无条件加：identity 与 gzip 两个分支都要加，否则共享缓存会拿 gzip 的响应
	// 去满足一个不支持 gzip 的客户端。与实时压缩路径同一函数。
	addVaryAcceptEncoding(h)
	// Content-Length 显式给出：http.ServeContent 在 Content-Encoding 非空时**不会**
	// 设这个头（它不假定编码不改变长度）。而预压缩路径的长度恰好是已知的 ——
	// 这正是它比实时压缩好的一点：后者只能删掉该头改走 chunked 传输。
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	// http.ServeContent 负责剩下的部分：按 .gz 文件大小与 mtime 出内容，白送
	// Last-Modified 与 304 协商（与明文路径同等能力；命中 304 时它会自行删掉
	// Content-Encoding / Content-Length / Content-Type）。
	//
	// 必须自己包一层 writer 才是零拷贝：本中间件排在 StaticGzip 之前，
	// c.Next() 之前替换的 c.Writer 才是这一层看到的最终值 —— 此刻它仍是裸的
	// *gin.responseWriter，不实现 io.ReaderFrom。而 .gz 是 os.Open 出来的
	// *os.File（条件②成立），只差一个能把 ReadFrom 传下去的目标（条件①）。
	// 包装后这条路径比实时压缩路径还干净：它连 gzip 层都不需要经过。
	http.ServeContent(newSendfileWriter(c.Writer), c.Request, filepath.Base(file), st.ModTime(), f)
	c.Abort()
	return true
}

// siteAssetFile 访问面 URL → 本次将被直出的物理文件（ActiveRoot 下的真实路径）。
//
// 判据与访问面两条服务通道同源，不另写一套映射：
//   - pipeline.CleanSiteRel（归一，唯一实现）
//   - pipeline.ResolveActiveEntry（URL → 激活条目名，唯一实现）
//   - ② 与 routers.siteFileServeMiddleware 的「产物内普通文件」分支逐条对应
//
// 为什么必须同源：这条映射分叉的后果写在 pipeline.ResolveActiveEntry 的注释里 ——
// 守卫判 /about 受限，而访问面对 /about/index.html 原样直出。预压缩查找若自己再做
// 一份简化版，就是给那条绕过再开一个入口。
//
// 解析不出文件时返回 ok=false：调用方回落实时压缩。
func siteAssetFile(prefix, urlPath string) (string, bool) {
	rel, inFace := SiteFaceRel(prefix, urlPath)
	if !inFace {
		return "", false
	}
	clean, ok := pipeline.CleanSiteRel(rel)
	if !ok {
		return "", false
	}
	root := pipeline.ActiveRoot()
	// ① URL 路径 → 页面产物（<条目>/index.html）。
	// 这一支同时覆盖「目录根请求」（/、/about/、/en/）与无扩展名路径（/about）两类：
	// siteDirIndexServeMiddleware 处理的正是前者 —— 预压缩查找若不覆盖它，首页
	// （访问量最大的一条 URL）永远拿不到预压缩字节。
	if entry, hit := pipeline.ResolveActiveEntry(root, clean); hit {
		return filepath.Join(root, filepath.FromSlash(entry), "index.html"), true
	}
	// ② 产物内的普通文件（产物自带 sitemap.xml / robots.txt / favicon 等）。
	target := filepath.Join(root, filepath.FromSlash(clean))
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		return target, true
	}
	return "", false
}
