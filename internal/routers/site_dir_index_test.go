package routers

// site_dir_index_test.go — 访问面「目录根请求」的映射（回归：首页与语言根整片 404）。
//
// 激活目录里每个页面的条目名就是它的 URL 路径，且是**无扩展名的目录符号链接**
// （pipeline.relActivePath："/" → "index"、"/about" → "about"），index.html 在目录里面。
// 而 http.FileServer 处理 "/site/" 这类目录根请求时找的是 <active>/index.html ——
// 找不到就 404。实测过的现象：/site/shop/ 正常、/site/ 恒 404，而 index 别名规则
// 又把 /site/index 301 到 "/"，正好落在同一条死链上。
//
// 这条用例钉三件事：
//   ① 根路径与任意「目录根」都映射到 <条目>/index/index.html；
//   ② 没有对应产物时**不能**变成 200（否则死链被搜索引擎当有效页收）；
//   ③ 改写通道不接受 ".."，不能成为目录穿越的新入口。

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/pipeline"
)

// seedSitePage 在激活根下建一个页面条目：<active>/<rel> → 指向含 index.html 的产物目录。
func seedSitePage(t *testing.T, active, rel, body string) {
	t.Helper()
	artifact := filepath.Join(filepath.Dir(active), "artifacts", strings.ReplaceAll(rel, "/", "_"))
	if rel == "" {
		artifact = filepath.Join(filepath.Dir(active), "artifacts", "home")
	}
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatalf("建产物目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "index.html"), []byte(body), 0o644); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	target := rel
	if target == "" {
		target = "index"
	}
	link := filepath.Join(active, filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("建条目父目录失败: %v", err)
	}
	rel2, err := filepath.Rel(filepath.Dir(link), artifact)
	if err != nil {
		t.Fatalf("算相对链接失败: %v", err)
	}
	if err := os.Symlink(rel2, link); err != nil {
		t.Fatalf("建条目链接失败: %v", err)
	}
}

func TestSiteFaceServesDirectoryRootIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	active := pipeline.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	seedSitePage(t, active, "", "<html><body>HOME</body></html>")
	seedSitePage(t, active, "about", "<html><body>ABOUT</body></html>")
	seedSitePage(t, active, "en", "<html><body>EN ROOT</body></html>")

	router := newSiteFaceRouter()
	cases := []struct {
		path string
		want string
		desc string
	}{
		{"/site/", "HOME", "站点根（/ 的激活条目名是 index）"},
		{"/site/about/", "ABOUT", "普通页面目录根"},
		{"/site/en/", "EN ROOT", "语言根：与首页同一套映射语义，不是特例"},
	}
	for _, tc := range cases {
		rec := doGet(router, tc.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s（%s）状态码期望 200，实际 %d", tc.path, tc.desc, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s（%s）响应内容不符: %q", tc.path, tc.desc, rec.Body.String())
		}
	}
}

// TestSiteFaceDirectoryRootWithoutArtifactIs404 没有产物时必须是 404，不能软 404。
func TestSiteFaceDirectoryRootWithoutArtifactIs404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	active := pipeline.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	router := newSiteFaceRouter()
	if rec := doGet(router, "/site/"); rec.Code != http.StatusNotFound {
		t.Fatalf("/site/ 无产物时期望 404，实际 %d body=%q", rec.Code, rec.Body.String())
	}
	if rec := doGet(router, "/site/ghost/"); rec.Code != http.StatusNotFound {
		t.Fatalf("/site/ghost/ 期望 404，实际 %d", rec.Code)
	}
}

// TestSiteFaceDirectoryRootRejectsTraversal 改写通道不接受 ".."。
func TestSiteFaceDirectoryRootRejectsTraversal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	t.Setenv("GO_WP_ARTIFACT_ROOT", root)
	active := pipeline.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	// 激活目录外面放一份不该被读到的文件。
	secret := filepath.Join(root, "secret", "index", "index.html")
	if err := os.MkdirAll(filepath.Dir(secret), 0o755); err != nil {
		t.Fatalf("建越界文件失败: %v", err)
	}
	if err := os.WriteFile(secret, []byte("SECRET"), 0o644); err != nil {
		t.Fatalf("写越界文件失败: %v", err)
	}

	router := newSiteFaceRouter()
	for _, path := range []string{"/site/../secret/", "/site/%2e%2e/secret/"} {
		rec := doGet(router, path)
		if strings.Contains(rec.Body.String(), "SECRET") {
			t.Fatalf("%s 读到了激活目录外的文件", path)
		}
	}
}
