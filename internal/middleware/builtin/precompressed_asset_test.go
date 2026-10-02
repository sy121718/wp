package builtin

// 访问面预压缩直出（PIPE-GZ）的验收：构建期生成的 <文件>.gz 命中时不再走实时压缩，
// 未命中时回落，协商不满足时明文语义不变。
//
// 夹具走**真实链路**：NewArtifact → LocalStore.PutArtifact → LocalPublicationStore.Activate
// → StaticFS 直出。只有走真实链路，测的才不是「我以为产物落盘时会带上 .gz」，
// 而是「产物落盘时确实带上了 .gz 且访问面确实读得到」。

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/pipeline"
)

// precompressedFixture 一份落盘并激活的产物夹具。
type precompressedFixture struct {
	router *gin.Engine
	plain  []byte
	// gzPath 访问面视角下入口 HTML 的预压缩文件（穿过 active 符号链接）。
	gzPath string
}

// setupPrecompressedFixture 落一份足够大的页面产物，激活到 / 与 /about 两个条目。
//
// 两个条目都要：目录根请求（/site/）走的是 siteDirIndexServe 那条映射
// （"/" → 条目名 "index"），只测 /about 会漏掉访问量最大的首页。
func setupPrecompressedFixture(t *testing.T, docURL string) *precompressedFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())

	// 明文字节必须 ≥ pipeline.PrecompressMinSize，否则构建期按阈值跳过、不会派生 .gz。
	plain := []byte("<!doctype html>\n<html><body>" +
		strings.Repeat("<p>预压缩验收正文 paragraph</p>\n", 200) +
		"</body></html>")

	store := &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()}
	pub := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}

	a, err := pipeline.NewArtifact(plain, &pipeline.Manifest{
		ManifestSchemaVersion:     pipeline.ManifestSchemaVersion,
		PageDocumentSchemaVersion: 1,
		SourceID:                  "page-precompressed",
		SourceType:                pipeline.SourceTypePage,
		CanonicalPath:             docURL,
		SourceHash:                "h",
		BuildInputHash:            "h",
	})
	if err != nil {
		t.Fatalf("构造产物失败: %v", err)
	}
	loc, err := store.PutArtifact(a)
	if err != nil {
		t.Fatalf("落盘产物失败: %v", err)
	}
	if err = pub.Activate(docURL, loc); err != nil {
		t.Fatalf("激活 %s 失败: %v", docURL, err)
	}

	// .gz 的路径按访问面自己的映射算（pipeline.CleanSiteRel + ResolveActiveEntry），
	// 不另写一份「URL → 文件名」的测试专用拼法 —— 那份拼法一旦与实现分叉，
	// 测试会去读一个不存在的文件，报的却是「构建期没派生」。
	clean, ok := pipeline.CleanSiteRel(docURL)
	if !ok {
		t.Fatalf("夹具不成立：%s 不是合法站点路径", docURL)
	}
	entry, ok := pipeline.ResolveActiveEntry(pipeline.ActiveRoot(), clean)
	if !ok {
		t.Fatalf("夹具不成立：%s 解析不出激活条目", docURL)
	}

	return &precompressedFixture{
		router: precompressedAssetRouter(),
		plain:  plain,
		gzPath: filepath.Join(pipeline.ActiveRoot(), filepath.FromSlash(entry),
			"index.html"+pipeline.PrecompressedSuffix),
	}
}

// precompressedAssetRouter 访问面替身，中间件排位与 routes.siteFaceChain 一致：
// 预压缩层在实时压缩层之前，静态文件由 StaticFS 直出。
func precompressedAssetRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/site",
		SiteCacheMiddleware(),
		PrecompressedAssetMiddleware("/site"),
		StaticGzipMiddleware(),
	)
	g.StaticFS("/", gin.Dir(pipeline.ActiveRoot(), false))
	return r
}

// doGet 发一次请求并返回响应与响应体。
func getWithHeaders(t *testing.T, r *gin.Engine, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	res := w.Result()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	_ = res.Body.Close()
	return res, body
}

// mustReadFile 读文件（测试用）。
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return data
}

// mustGunzipBytes 解压（测试用）。
func mustGunzipBytes(t *testing.T, stream []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("响应体不是合法 gzip 流: %v", err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压响应体失败: %v", err)
	}
	return out
}

// TestPrecompressedAssetServedFromFile 证据 ②：.gz 存在时响应体就是那份文件，
// 不再走实时压缩。
//
// 判据是**逐字节等于磁盘上的 .gz**，不是「解压后等于明文」——后者实时压缩同样满足，
// 是一条抓不到东西的断言。而两者的字节本来就不同（构建期 BestCompression、
// 实时 DefaultCompression），所以「等于磁盘 .gz」严格区分了两条路径。
func TestPrecompressedAssetServedFromFile(t *testing.T) {
	fx := setupPrecompressedFixture(t, "/about")

	gzOnDisk := mustReadFile(t, fx.gzPath)
	if len(gzOnDisk) == 0 {
		t.Fatal("产物里没有 .gz：构建期派生没生效")
	}
	if len(gzOnDisk) >= len(fx.plain) {
		t.Fatalf("磁盘 .gz（%d 字节）不小于明文（%d 字节）：夹具不成立", len(gzOnDisk), len(fx.plain))
	}

	res, body := getWithHeaders(t, fx.router, "/site/about/", map[string]string{"Accept-Encoding": "gzip"})

	t.Logf("明文 %d B / 磁盘 .gz %d B / 响应体 %d B", len(fx.plain), len(gzOnDisk), len(body))
	t.Logf("status=%d Content-Encoding=%q Content-Length=%q Vary=%q Content-Type=%q",
		res.StatusCode, res.Header.Get("Content-Encoding"), res.Header.Get("Content-Length"),
		res.Header.Get("Vary"), res.Header.Get("Content-Type"))
	t.Logf("响应体逐字节等于磁盘 .gz：%v（响应体 sha256 %s）",
		bytes.Equal(body, gzOnDisk), pipeline.SHA256(body))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q，期望 gzip", got)
	}
	if !bytes.Equal(body, gzOnDisk) {
		t.Errorf("响应体与磁盘 .gz 不一致（响应 %d 字节 / 磁盘 %d 字节）——"+
			"说明走的是实时压缩而不是预压缩直出", len(body), len(gzOnDisk))
	}
	// Content-Length 有值且等于 gz 文件大小：预压缩路径长度已知，
	// 不必像实时压缩那样删掉该头改走 chunked。
	if got := res.Header.Get("Content-Length"); got != strconv.Itoa(len(gzOnDisk)) {
		t.Errorf("Content-Length = %q，期望 %d（预压缩路径长度已知，不该退化成 chunked）",
			got, len(gzOnDisk))
	}
	// Vary：identity 与 gzip 两个分支都必须带，否则共享缓存会拿 gzip 响应去满足
	// 不支持 gzip 的客户端。
	if got := res.Header.Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Errorf("Vary = %q，期望含 Accept-Encoding", got)
	}
	// Content-Type 取**明文**文件的类型：gzip 是内容编码层，不改媒体类型。
	if got := res.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q，期望 text/html; charset=utf-8", got)
	}
	if got := mustGunzipBytes(t, body); !bytes.Equal(got, fx.plain) {
		t.Error("响应体解压后与入口 HTML 不一致")
	}
}

// TestPrecompressedAssetCoversDirectoryRoot 目录根请求也命中预压缩。
//
// siteDirIndexServe 把 /site/ 映射到条目 "index"；预压缩查找若是只认
// 「<path>/index.html」的朴素拼法，首页（访问量最大的一条 URL）永远拿不到
// 预压缩字节 —— 这正是一条只在首页上表现、且没有任何报错路径的漏。
func TestPrecompressedAssetCoversDirectoryRoot(t *testing.T) {
	fx := setupPrecompressedFixture(t, "/")

	gzOnDisk := mustReadFile(t, fx.gzPath)
	for _, path := range []string{"/site/", "/site/index/"} {
		res, body := getWithHeaders(t, fx.router, path, map[string]string{"Accept-Encoding": "gzip"})
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s 状态码 = %d，期望 200", path, res.StatusCode)
			continue
		}
		if got := res.Header.Get("Content-Encoding"); got != "gzip" {
			t.Errorf("%s Content-Encoding = %q，期望 gzip", path, got)
		}
		if !bytes.Equal(body, gzOnDisk) {
			t.Errorf("%s 响应体与磁盘 .gz 不一致：目录根请求没有命中预压缩", path)
		}
	}
}

// TestPrecompressedAssetFallsBackToRuntimeGzip 证据 ③：.gz 缺失时回落实时压缩，
// 明文语义不变。
//
// 双向断言：两条路径都必须解压回同一份明文（功能等价），但字节**必须不同**
// （证明回落路径真的重新压了一遍，而不是把某个残留文件又发了一次）。
func TestPrecompressedAssetFallsBackToRuntimeGzip(t *testing.T) {
	fx := setupPrecompressedFixture(t, "/about")

	_, precompressed := getWithHeaders(t, fx.router, "/site/about/",
		map[string]string{"Accept-Encoding": "gzip"})

	// 模拟「.gz 因阈值 / 类型白名单 / 历史产物而缺失」：直接删掉它。
	if err := os.Remove(fx.gzPath); err != nil {
		t.Fatalf("删除 %s 失败: %v", fx.gzPath, err)
	}

	res, runtimeGzip := getWithHeaders(t, fx.router, "/site/about/",
		map[string]string{"Accept-Encoding": "gzip"})

	t.Logf("预压缩命中：%d B  sha256=%s", len(precompressed), pipeline.SHA256(precompressed))
	t.Logf("删掉 .gz 后：%d B  sha256=%s  Content-Encoding=%q Content-Length=%q",
		len(runtimeGzip), pipeline.SHA256(runtimeGzip),
		res.Header.Get("Content-Encoding"), res.Header.Get("Content-Length"))
	t.Logf("两条路径都解压回明文：%v；字节不同（确认是重新压的）：%v",
		bytes.Equal(mustGunzipBytes(t, runtimeGzip), fx.plain), !bytes.Equal(runtimeGzip, precompressed))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q，期望 gzip（实时压缩兜底）", got)
	}
	if got := mustGunzipBytes(t, runtimeGzip); !bytes.Equal(got, fx.plain) {
		t.Error("回落路径解压后与明文不一致：明文语义被改变")
	}
	if bytes.Equal(runtimeGzip, precompressed) {
		t.Error("回落路径的字节与预压缩完全相同——说明并没有真的回落到实时压缩，" +
			"这条测试没有覆盖它想覆盖的分支")
	}
}

// TestPrecompressedAssetRespectsNegotiation 证据 ③ 的另一半：协商不满足时
// 必须原样返回明文，绝不把 gzip 字节发给没说要 gzip 的客户端。
//
// 这是「访问面只读静态直出」的底线，也是共享缓存正确性的前提。
func TestPrecompressedAssetRespectsNegotiation(t *testing.T) {
	fx := setupPrecompressedFixture(t, "/about")

	cases := []struct {
		name           string
		acceptEncoding string
	}{
		{"不带 Accept-Encoding", ""},
		{"显式拒绝 gzip", "gzip;q=0"},
		{"只接受 br", "br"},
	}
	for _, tc := range cases {
		headers := map[string]string{}
		if tc.acceptEncoding != "" {
			headers["Accept-Encoding"] = tc.acceptEncoding
		}
		res, body := getWithHeaders(t, fx.router, "/site/about/", headers)
		if got := res.Header.Get("Content-Encoding"); got != "" {
			t.Errorf("%s：Content-Encoding = %q，期望空（协商不满足时必须出明文）", tc.name, got)
		}
		if !bytes.Equal(body, fx.plain) {
			t.Errorf("%s：响应体不是明文", tc.name)
		}
	}
}

// TestPrecompressedAssetIgnoresRangeRequests Range 请求不走预压缩。
//
// 206 的 Content-Range 描述的是**所选表示**的字节范围；用 .gz 的字节去满足一个
// 本意针对明文的 Range，分片语义就与明文对不上了（视频拖动 / 断点下载那一类）。
func TestPrecompressedAssetIgnoresRangeRequests(t *testing.T) {
	fx := setupPrecompressedFixture(t, "/about")
	gzOnDisk := mustReadFile(t, fx.gzPath)

	res, body := getWithHeaders(t, fx.router, "/site/about/", map[string]string{
		"Accept-Encoding": "gzip",
		"Range":           "bytes=0-99",
	})

	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("状态码 = %d，期望 206（Range 请求按原路径处理）", res.StatusCode)
	}
	if bytes.Equal(body, gzOnDisk) {
		t.Error("Range 请求拿到了 .gz 的字节：Content-Range 语义已被破坏")
	}
	if !bytes.Equal(body, fx.plain[:100]) {
		t.Error("Range 返回的不是明文的前 100 字节")
	}
}

// TestPrecompressedAssetDoesNotMaterializeFile 预压缩层不得把「本来不存在的文件」
// 变成一个 200。
//
// 构造：产物目录里只有 .gz，没有 index.html。路径解析（pipeline.ResolveActiveEntry）
// 按明文文件是否存在判定，因此这里必须解析失败 —— 若实现里写的是「先探 .gz 再说」，
// 这条就会返回 200，把一次 404 变成一次伪造的成功响应。
func TestPrecompressedAssetDoesNotMaterializeFile(t *testing.T) {
	fx := setupPrecompressedFixture(t, "/about")
	// 删掉明文，只留 .gz。
	if err := os.Remove(filepath.Join(filepath.Dir(fx.gzPath), "index.html")); err != nil {
		t.Fatalf("删除明文失败: %v", err)
	}

	res, body := getWithHeaders(t, fx.router, "/site/about/", map[string]string{"Accept-Encoding": "gzip"})
	if res.StatusCode == http.StatusOK && len(body) > 0 {
		t.Errorf("明文不存在却返回了 200：预压缩层把不存在的文件变出来了")
	}
	if got := res.Header.Get("Content-Encoding"); got == "gzip" && len(body) > 0 {
		t.Errorf("明文不存在却下发了 gzip 响应体")
	}
}

// TestPrecompressedAssetNotOnStaticFace 预压缩层不能被 /static 面共用。
//
// /static 面挂的是 StaticGzipMiddleware（无产物可查）。若把预压缩查找做进它的构造，
// /static/about 这类本来 404 的路径会被解析成站点产物并直出 —— 404 变 200。
func TestPrecompressedAssetNotOnStaticFace(t *testing.T) {
	setupPrecompressedFixture(t, "/about") // 夹具：产物里有 about 条目与它的 .gz

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// 与 assembly.go 的 /static 面一致：只挂实时压缩，不挂预压缩层。
	r.Group("/static", StaticGzipMiddleware()).StaticFS("/", gin.Dir(t.TempDir(), false))

	res, _ := getWithHeaders(t, r, "/static/about/", map[string]string{"Accept-Encoding": "gzip"})
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("/static/about/ 状态码 = %d，期望 404（/static 面不该认识站点产物）", res.StatusCode)
	}
}

// TestPrecompressMinSizeAlignedWithGzipMiddleware 构建期阈值与传输阈值必须同值。
//
// 不一致的后果：构建期生成了、实时压缩层却认为该重压（白费一次 CPU），或反过来
// 小文件也被派生（访问面白读一次文件）。两侧各自的测试都不会红。
func TestPrecompressMinSizeAlignedWithGzipMiddleware(t *testing.T) {
	if pipeline.PrecompressMinSize != gzipMinSize {
		t.Fatalf("构建期阈值 %d ≠ 实时压缩阈值 %d", pipeline.PrecompressMinSize, gzipMinSize)
	}
}

// TestAssetContentTypeCompressibleByGzipMiddleware 预压缩层的类型判据与实时压缩层
// 必须落在同一个集合里。
//
// 漂移的表现是「同一份产物，命中 .gz 时按 text/html 下发、回落时却不被压缩」——
// 客户端的 Accept-Encoding 决定了它拿到的字节量，而不是内容本身。
func TestAssetContentTypeCompressibleByGzipMiddleware(t *testing.T) {
	// 构建期白名单里的扩展名，其 Content-Type 必须被实时压缩层认作可压缩。
	for _, name := range []string{"index.html", "style.css", "app.js", "sitemap.xml", "icon.svg", "robots.txt"} {
		ctype := pipeline.AssetContentType(name)
		if !isGzipType(ctype) {
			t.Errorf("%s：AssetContentType = %q，但实时压缩层判定为不可压缩 —— 两条路径的判据分叉了",
				name, ctype)
		}
	}
	// 反向：已压缩格式既不在构建期白名单，也不该被实时压缩层压。
	for _, name := range []string{"logo.png", "font.woff2", "photo.jpg"} {
		if pipeline.IsPrecompressible(name, pipeline.PrecompressMinSize) {
			t.Errorf("%s 不该在构建期预压缩白名单里", name)
		}
	}
}
