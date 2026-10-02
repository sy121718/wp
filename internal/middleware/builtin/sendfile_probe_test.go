package builtin

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// 零拷贝（sendfile）回归守卫。
//
// 判据链（完整论证见 sendfile.go）：
//
//	http.ServeContent → io.CopyN(w, content, n)   // 源被 *io.LimitedReader 包住
//	→ dst.(io.ReaderFrom)                         // 条件①：writer 链上得有人实现它
//	→ *http.response.ReadFrom → w.conn.rwc.(io.ReaderFrom) → net.TCPConn.ReadFrom
//	→ internal/poll.sendFile **剥开 *io.LimitedReader**
//	→ 内层必须是 *os.File                        // 条件②：源侧没有被包装
//	→ syscall.Sendfile
//
// 所以内核零拷贝成立 ⟺ 两件事同时为真：传输层 conn 的 ReadFrom 被调用、
// 且剥开 LimitedReader 后内层动态类型是 *os.File。这里直接观测传输层，
// 绕开所有中间件包装细节 —— 包装层的对错由这两个事实裁决，不由「读代码觉得对」。

// sfProbeConn 记录一次连接上的零拷贝判定事实。
type sfProbeConn struct {
	net.Conn

	mu            sync.Mutex
	readFromCalls int
	innerTypes    []string
}

func (c *sfProbeConn) ReadFrom(r io.Reader) (int64, error) {
	// 与 internal/poll.sendFile 完全一致的剥开逻辑：sendfile 真正会看到的源就是它。
	inner := r
	if lr, ok := r.(*io.LimitedReader); ok {
		inner = lr.R
	}
	c.mu.Lock()
	c.readFromCalls++
	c.innerTypes = append(c.innerTypes, fmt.Sprintf("%T", inner))
	c.mu.Unlock()
	// 不调 syscall.Sendfile（测试进程里没有意义），只把字节搬运做完。
	// 用只暴露 Write 的壳，避免自己再命中 io.ReaderFrom 造成递归。
	return io.Copy(sfWriterOnly{c.Conn}, r)
}

func (c *sfProbeConn) facts() (int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readFromCalls, append([]string(nil), c.innerTypes...)
}

// sfProbeListener 把每条接受的连接替换为可观测版本。
type sfProbeListener struct {
	net.Listener

	mu    sync.Mutex
	conns []*sfProbeConn
}

func (l *sfProbeListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	pc := &sfProbeConn{Conn: c}
	l.mu.Lock()
	l.conns = append(l.conns, pc)
	l.mu.Unlock()
	return pc, nil
}

func (l *sfProbeListener) aggregated() (int, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := 0
	var types []string
	for _, c := range l.conns {
		n, ts := c.facts()
		total += n
		types = append(types, ts...)
	}
	return total, types
}

// sfProbeServe 起真实 TCP server 请求 urlPath，不带额外请求头。
func sfProbeServe(t *testing.T, engine http.Handler, urlPath string) (int, []string) {
	t.Helper()
	return sfProbeServeHeaders(t, engine, urlPath, nil)
}

// sfProbeServeHeaders 同 sfProbeServe，但可指定请求头（预压缩路径要显式要 gzip）。
func sfProbeServeHeaders(t *testing.T, engine http.Handler, urlPath string, headers map[string]string) (int, []string) {
	t.Helper()

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	ln := &sfProbeListener{Listener: raw}
	srv := &http.Server{Handler: engine}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			// 不自动加 Accept-Encoding: gzip：本测试测的是**不被压缩的那条路**，
			// 压缩路径本来就不该走 sendfile（字节要进 gzip.Writer）。
			// 需要 gzip 的用例用 headers 显式要。
			DisableCompression: true,
		},
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+raw.Addr().String()+urlPath, nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", urlPath, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s 期望 200，实际 %d（body=%q）", urlPath, resp.StatusCode, string(body))
	}

	// 响应体已读完，但写侧的 ReadFrom 计数可能还在登记，给它一个短窗口。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n, _ := ln.aggregated(); n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ln.aggregated()
}

// sfProbeStatus 只要状态码：不观测传输层，走 httptest 即可。
func sfProbeStatus(t *testing.T, engine http.Handler, urlPath string) int {
	t.Helper()

	srv := httptest.NewServer(engine)
	defer srv.Close()

	resp, err := http.Get(srv.URL + urlPath)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", urlPath, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// sfProbeRoot 造一份足够大的静态资源。
//
// 用 .jpg 而不是 .txt：文本类型会被 StaticGzipMiddleware 压缩，压缩路径的字节
// 必须经过 gzip.Writer，**本来就不该**走零拷贝 —— 拿它测零拷贝只会得到
// 「测试写错了」的结论。图片是静态面里最典型的零拷贝对象。
func sfProbeRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	payload := bytes.Repeat([]byte("\xff\xd8\xff\xe0only-bytes-matter-here\n"), 4096)
	if err := os.WriteFile(filepath.Join(dir, "big.jpg"), payload, 0o644); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	// 指纹形状命中的变体名，用来验证 /storage 那条「会包一层 writer」的链路。
	if err := os.WriteFile(filepath.Join(dir, "123_thumb-1-abcdef12.jpg"), payload, 0o644); err != nil {
		t.Fatalf("写变体失败: %v", err)
	}
	// 小于标准库 sniffLen（512）的小文件：专门用来证明 WriteHeaderNow 不是可选优化。
	// 不补它，(*http.response).ReadFrom 会走「先拷 512 字节做 Content-Type 嗅探」的
	// 分支，因为不足 512 而提前返回，零拷贝静默失效。
	if err := os.WriteFile(filepath.Join(dir, "small.jpg"), bytes.Repeat([]byte("x"), 200), 0o644); err != nil {
		t.Fatalf("写小文件失败: %v", err)
	}
	return dir
}

// TestGinDirLosesWriterTo 钉住「为什么不能用 gin.Dir」这个事实本身。
//
// gin 的 OnlyFilesFS.Open 对**文件**也包装成 neutralizedReaddirFile{f}（值类型 +
// 嵌入 http.File 接口）→ *os.File 的 WriteTo 被抹掉。这条测试断言的是 gin 的行为，
// 它永远绿：将来 gin 改了实现，这里会红，提醒我们这层自定义 FS 可以删掉。
func TestGinDirLosesWriterTo(t *testing.T) {
	dir := sfProbeRoot(t)

	opened, err := gin.Dir(dir, false).Open("big.jpg")
	if err != nil {
		t.Fatalf("gin.Dir 打开文件失败: %v", err)
	}
	defer func() { _ = opened.Close() }()

	_, isOSFile := opened.(*os.File)
	_, isWriterTo := opened.(io.WriterTo)
	t.Logf("gin.Dir 对普通文件返回 %T（*os.File=%v, io.WriterTo=%v）", opened, isOSFile, isWriterTo)

	if isOSFile || isWriterTo {
		t.Errorf("gin.Dir 竟然保留了 *os.File/io.WriterTo —— 自定义 FS 的必要性已消失，请复核 static_fs.go")
	}
}

// TestNoDirListFSPreservesOSFile 单测源侧：普通文件必须原样透出（sendfile 条件②）。
func TestNoDirListFSPreservesOSFile(t *testing.T) {
	dir := sfProbeRoot(t)

	fs := NoDirListFS(dir)

	f, err := fs.Open("big.jpg")
	if err != nil {
		t.Fatalf("打开文件失败: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, ok := f.(*os.File); !ok {
		t.Errorf("普通文件被包装成 %T，应为 *os.File（WriteTo 被抹掉 → sendfile 失效）", f)
	}
	if _, ok := f.(io.WriterTo); !ok {
		t.Errorf("普通文件 %T 不实现 io.WriterTo", f)
	}
}

// TestNoDirListFSMatchesGinDirOnDirectories 钉住目录语义，防止「禁目录列表」被
// 退化成「200 + 空列表页」。
//
// 为什么这条测试是必需的：gin 的 StaticFS 靠 fs.(*gin.OnlyFilesFS) 这个**类型断言**
// 预设 404 来兜底目录请求；我们换掉了 FS 类型，那条兜底不再触发。而 http.FileServer
// 对「目录且没有 index.html」的出口是 dirList —— 它把空 Readdir 渲染成 200 空页。
// 所以这里把两种 FS 放在**同一条链路**上跑同一批请求，逐条比对状态码。
func TestNoDirListFSMatchesGinDirOnDirectories(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	dir := sfProbeRoot(t)
	if err := os.MkdirAll(filepath.Join(dir, "page"), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page", "index.html"), []byte("<h1>x</h1>"), 0o644); err != nil {
		t.Fatalf("写 index.html 失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bare"), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bare", "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	// 覆盖三种目录形态：有 index.html、无 index.html、以及站点根。
	paths := []string{"/s/page/", "/s/bare/", "/s/", "/s/big.jpg"}

	results := map[string][]int{}
	for _, impl := range []struct {
		name string
		fs   http.FileSystem
	}{
		{"NoDirListFS", NoDirListFS(dir)},
		{"gin.Dir", gin.Dir(dir, false)},
	} {
		var codes []int
		for _, p := range paths {
			engine := gin.New()
			engine.Group("/s", StaticGzipMiddleware()).StaticFS("/", impl.fs)
			codes = append(codes, sfProbeStatus(t, engine, p))
		}
		results[impl.name] = codes
		t.Logf("%s: %v（顺序 %v）", impl.name, codes, paths)
	}

	for i, p := range paths {
		want := results["gin.Dir"][i]
		if got := results["NoDirListFS"][i]; got != want {
			t.Errorf("%s：NoDirListFS 返回 %d，gin.Dir 返回 %d —— 换 FS 不应改变可观测行为", p, got, want)
		}
	}
	// 顺带钉住「无 index.html 的目录不是 200」这条本身，避免两个实现一起漂移到空列表页。
	if got := results["NoDirListFS"][1]; got == http.StatusOK {
		t.Errorf("/s/bare/ 返回了 200 —— 目录列表被渲染成空页，等于把不可浏览实现成了空页面")
	}
}

// TestSiteFaceZeroCopy 是生产路径的主判据：/site 链（含 gzip 层 + SiteCache 同款
// 的 writer 包装）必须把零拷贝一路放行到传输层。
func TestSiteFaceZeroCopy(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	dir := sfProbeRoot(t)
	engine := gin.New()
	engine.Group("/site", StaticGzipMiddleware()).StaticFS("/", NoDirListFS(dir))

	calls, innerTypes := sfProbeServe(t, engine, "/site/big.jpg")
	t.Logf("/site 链：ReadFrom 调用=%d 剥开后类型=%v", calls, innerTypes)

	if calls == 0 {
		t.Errorf("零拷贝未发生：传输层 ReadFrom 从未被调用 → writer 链上有一层吃掉了 io.ReaderFrom")
	}
	for _, ty := range innerTypes {
		if ty != "*os.File" {
			t.Errorf("零拷贝未发生：剥开 LimitedReader 后源是 %s，不是 *os.File → 源侧被包装", ty)
		}
	}
}

// TestStaticFaceZeroCopy 覆盖 /static 的真实链形状（StaticGzip → StaticCache）。
//
// StaticCacheMiddleware 只 defer 落头、**不包装 writer**，所以这条链的最外层就是
// gzipResponseWriter —— 本用例实际判的是 gzip 层的穿透。
func TestStaticFaceZeroCopy(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	dir := sfProbeRoot(t)
	engine := gin.New()
	engine.Group("/static", StaticGzipMiddleware(), StaticCacheMiddleware()).
		StaticFS("/", NoDirListFS(dir))

	calls, innerTypes := sfProbeServe(t, engine, "/static/big.jpg")
	t.Logf("/static 链：ReadFrom 调用=%d 剥开后类型=%v", calls, innerTypes)

	if calls == 0 {
		t.Errorf("零拷贝未发生：/static 链上有一层吃掉了 io.ReaderFrom")
	}
	for _, ty := range innerTypes {
		if ty != "*os.File" {
			t.Errorf("零拷贝未发生：剥开 LimitedReader 后源是 %s，不是 *os.File", ty)
		}
	}
}

// TestStorageFaceZeroCopyThroughCacheLayer 覆盖 storageCacheWriter 的 ReadFrom。
//
// 必须用**指纹形状命中**的文件名，否则 StorageCacheMiddleware 走「不包装 Writer」
// 那条零成本分支，本用例会静默退化成又一次 gzip 层测试 —— 判定条件与本文件其他
// 用例相同，但被观测的层次不同。
func TestStorageFaceZeroCopyThroughCacheLayer(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	dir := sfProbeRoot(t)
	engine := gin.New()
	engine.Group("/storage", StorageCacheMiddleware()).StaticFS("/", NoDirListFS(dir))

	calls, innerTypes := sfProbeServe(t, engine, "/storage/123_thumb-1-abcdef12.jpg")
	t.Logf("/storage 链：ReadFrom 调用=%d 剥开后类型=%v", calls, innerTypes)

	if calls == 0 {
		t.Errorf("零拷贝未发生：storageCacheWriter 吃掉了 io.ReaderFrom")
	}
	for _, ty := range innerTypes {
		if ty != "*os.File" {
			t.Errorf("零拷贝未发生：剥开 LimitedReader 后源是 %s，不是 *os.File", ty)
		}
	}
}

// TestSmallFileStillUsesZeroCopy 钉住 sfCommitHeader 的必要性。
//
// (*http.response).ReadFrom 在 w.cw.wroteHeader 为 false 时会先拷 sniffLen(512)
// 字节做 Content-Type 嗅探，且**不足 512 就提前返回** —— 零拷贝那一段根本走不到。
// 各层 ReadFrom 都先调 sfCommitHeader 把响应头真正落到连接上，正是为了让那个分支
// 不成立。这条测试用 200 字节的文件把这个性质钉死：去掉 sfCommitHeader，
// 大文件（>512）依旧绿，只有它会红。
func TestSmallFileStillUsesZeroCopy(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	dir := sfProbeRoot(t)
	engine := gin.New()
	engine.Group("/site", StaticGzipMiddleware()).StaticFS("/", NoDirListFS(dir))

	calls, innerTypes := sfProbeServe(t, engine, "/site/small.jpg")
	t.Logf("200 字节小文件：ReadFrom 调用=%d 剥开后类型=%v", calls, innerTypes)

	if calls == 0 {
		t.Errorf("小文件未走零拷贝：响应头没有真正落到连接上，标准库的 sniff 分支提前返回了")
	}
}

// TestPrecompressedAssetZeroCopy 预压缩命中时也必须走内核零拷贝。
//
// 这条路径与其它三条的成因不同：PrecompressedAssetMiddleware 排在 StaticGzip
// **之前**，它调 http.ServeContent 时看到的 c.Writer 还是裸的 *gin.responseWriter
// （不实现 io.ReaderFrom），而链尾再加中间件救不了它（后执行的才包在外面）。
// 所以 servePrecompressed 必须自己包一层 newSendfileWriter —— 见 gzip.go。
//
// 附带价值：.gz 通常只有几百字节，天然落在 sniffLen 以下，
// 于是这条用例同时也在检验小文件路径。
func TestPrecompressedAssetZeroCopy(t *testing.T) {
	f := setupPrecompressedFixture(t, "/about")

	if st, err := os.Stat(f.gzPath); err == nil {
		t.Logf("预压缩产物 %d 字节（sniffLen=512，不足 512 时必须靠 sfCommitHeader 落头才能走零拷贝）", st.Size())
	} else {
		t.Fatalf("读预压缩产物失败: %v", err)
	}

	calls, innerTypes := sfProbeServeHeaders(t, f.router, "/site/about",
		map[string]string{"Accept-Encoding": "gzip"})
	t.Logf("预压缩路径：ReadFrom 调用=%d 剥开后类型=%v", calls, innerTypes)

	if calls == 0 {
		t.Errorf("零拷贝未发生：预压缩路径的 writer 没有补 io.ReaderFrom（外层中间件补不了它）")
	}
	for _, ty := range innerTypes {
		if ty != "*os.File" {
			t.Errorf("零拷贝未发生：剥开 LimitedReader 后源是 %s，不是 *os.File", ty)
		}
	}
}
