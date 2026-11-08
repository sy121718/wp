package feature

import (
	mw "go_wp/pkg/middleware"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/i18n"
)

// TestAdminI18nRealBrowser 使用真实页面、会话、CSRF、权限、词库和 HTMX 验证词条交互。
func TestAdminI18nRealBrowser(t *testing.T) {
	if os.Getenv("GOWP_HTML_UI_BROWSER") != "1" {
		t.Skip("设置 GOWP_HTML_UI_BROWSER=1 验证真实词条页面")
	}
	fix := newI18nWriteFixture(t)
	fix.allow(t)
	if _, err := pkgcasbin.GetEnforcer().AddPolicy(fix.userID, "/api/i18n/list", http.MethodGet, "i18n:view"); err != nil {
		t.Fatal(err)
	}
	pages := fix.engine.Group("/admin", builtin.SessionAuthMiddleware(), func(c *gin.Context) {
		c.Set("button_set", map[string]bool{"i18n.manage": true})
		c.Next()
	})
	dict := sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(fix.db), nil)
	adminhttp.SetupAdminPages(mw.NewRegistry(), pages, nil, nil, nil, nil, nil, nil, nil, dict)
	fix.engine.Static("/static", "../../../../internal/templates/static")
	for _, width := range []string{"1440", "768", "375"} {
		key := "browser.i18n." + width + ".<&\"'+?/#"
		for _, lang := range []string{"zh-CN", "ja-JP"} {
			if err := i18n.SaveEntry(context.Background(), i18n.Entry{Key: key, Lang: lang, Value: "保留其它语言 " + lang, Category: "browser", Remark: "原备注"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 保持请求在途以验证 drop 与禁用恢复；不替换业务处理或响应。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/i18n/save" {
			select {
			case <-time.After(100 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		fix.engine.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "../../../../internal/templates/testdata/html-ui/admin-i18n.cjs")
	cmd.Env = append(os.Environ(), "GOWP_HTML_UI_BASE_URL="+server.URL, "GOWP_HTML_UI_COOKIE="+fix.sess.Cookie)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实词条浏览器验收失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)
	for _, width := range []string{"1440", "768", "375"} {
		for _, lang := range []string{"zh-CN", "en-US", "ja-JP"} {
			if _, exists := fix.entry(t, "browser.i18n."+width+".<&\"'+?/#", lang); exists {
				t.Fatalf("浏览器删除后词条仍在：%s/%s", width, lang)
			}
		}
	}
}
