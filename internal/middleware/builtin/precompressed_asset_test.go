package builtin

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

type precompressedFixture struct {
	router *gin.Engine
	plain  []byte

	gzPath string
}

func setupPrecompressedFixture(t *testing.T, docURL string) *precompressedFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())

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

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return data
}

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

	if got := res.Header.Get("Content-Length"); got != strconv.Itoa(len(gzOnDisk)) {
		t.Errorf("Content-Length = %q，期望 %d（预压缩路径长度已知，不该退化成 chunked）",
			got, len(gzOnDisk))
	}

	if got := res.Header.Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Errorf("Vary = %q，期望含 Accept-Encoding", got)
	}

	if got := res.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q，期望 text/html; charset=utf-8", got)
	}
	if got := mustGunzipBytes(t, body); !bytes.Equal(got, fx.plain) {
		t.Error("响应体解压后与入口 HTML 不一致")
	}
}

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

func TestPrecompressedAssetFallsBackToRuntimeGzip(t *testing.T) {
	fx := setupPrecompressedFixture(t, "/about")

	_, precompressed := getWithHeaders(t, fx.router, "/site/about/",
		map[string]string{"Accept-Encoding": "gzip"})

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

func TestPrecompressedAssetDoesNotMaterializeFile(t *testing.T) {
	fx := setupPrecompressedFixture(t, "/about")

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

func TestPrecompressedAssetNotOnStaticFace(t *testing.T) {
	setupPrecompressedFixture(t, "/about")

	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.Group("/static", StaticGzipMiddleware()).StaticFS("/", gin.Dir(t.TempDir(), false))

	res, _ := getWithHeaders(t, r, "/static/about/", map[string]string{"Accept-Encoding": "gzip"})
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("/static/about/ 状态码 = %d，期望 404（/static 面不该认识站点产物）", res.StatusCode)
	}
}

func TestPrecompressMinSizeAlignedWithGzipMiddleware(t *testing.T) {
	if pipeline.PrecompressMinSize != gzipMinSize {
		t.Fatalf("构建期阈值 %d ≠ 实时压缩阈值 %d", pipeline.PrecompressMinSize, gzipMinSize)
	}
}

func TestAssetContentTypeCompressibleByGzipMiddleware(t *testing.T) {

	for _, name := range []string{"index.html", "style.css", "app.js", "sitemap.xml", "icon.svg", "robots.txt"} {
		ctype := pipeline.AssetContentType(name)
		if !isGzipType(ctype) {
			t.Errorf("%s：AssetContentType = %q，但实时压缩层判定为不可压缩 —— 两条路径的判据分叉了",
				name, ctype)
		}
	}

	for _, name := range []string{"logo.png", "font.woff2", "photo.jpg"} {
		if pipeline.IsPrecompressible(name, pipeline.PrecompressMinSize) {
			t.Errorf("%s 不该在构建期预压缩白名单里", name)
		}
	}
}
