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

type sfProbeConn struct {
	net.Conn

	mu            sync.Mutex
	readFromCalls int
	innerTypes    []string
}

func (c *sfProbeConn) ReadFrom(r io.Reader) (int64, error) {

	inner := r
	if lr, ok := r.(*io.LimitedReader); ok {
		inner = lr.R
	}
	c.mu.Lock()
	c.readFromCalls++
	c.innerTypes = append(c.innerTypes, fmt.Sprintf("%T", inner))
	c.mu.Unlock()

	return io.Copy(sfWriterOnly{c.Conn}, r)
}

func (c *sfProbeConn) facts() (int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readFromCalls, append([]string(nil), c.innerTypes...)
}

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

func sfProbeServe(t *testing.T, engine http.Handler, urlPath string) (int, []string) {
	t.Helper()
	return sfProbeServeHeaders(t, engine, urlPath, nil)
}

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

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n, _ := ln.aggregated(); n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ln.aggregated()
}

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

func sfProbeRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	payload := bytes.Repeat([]byte("\xff\xd8\xff\xe0only-bytes-matter-here\n"), 4096)
	if err := os.WriteFile(filepath.Join(dir, "big.jpg"), payload, 0o644); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "123_thumb-1-abcdef12.jpg"), payload, 0o644); err != nil {
		t.Fatalf("写变体失败: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "small.jpg"), bytes.Repeat([]byte("x"), 200), 0o644); err != nil {
		t.Fatalf("写小文件失败: %v", err)
	}
	return dir
}

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

	if got := results["NoDirListFS"][1]; got == http.StatusOK {
		t.Errorf("/s/bare/ 返回了 200 —— 目录列表被渲染成空页，等于把不可浏览实现成了空页面")
	}
}

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
