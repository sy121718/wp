package routers

import (
	publish "go_wp/pkg/publish"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

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
	active := publish.ActiveRoot()
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

func TestSiteFaceDirectoryRootWithoutArtifactIs404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	active := publish.ActiveRoot()
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

func TestSiteFaceDirectoryRootRejectsTraversal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	t.Setenv("GO_WP_ARTIFACT_ROOT", root)
	active := publish.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}

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
