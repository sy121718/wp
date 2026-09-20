package blockhttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
)

func blocksLayoutData(base gin.H) gin.H {
	lang := "zh-CN"
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/blocks"
	base["PermSet"] = map[string]any{"block:create": true, "block:delete": true}
	return base
}

func TestBlocksListRenderWithStaleImpactPages(t *testing.T) {
	v := viper.New()
	v.Set("log.base_dir", "/tmp/wp-tmp-logs2")
	if err := logger.Init(v); err != nil {
		t.Fatalf("logger init: %v", err)
	}
	data := gin.H{
		"title": "区块管理", "menu": "blocks",
		"Projects": []gin.H{{"ID": "prj", "Name": "站点"}}, "SelectedProjectID": "",
		"Headers": []gin.H{}, "Footers": []gin.H{}, "Blocks": []gin.H{},
		"Err": "", "Done": "",
		"StaleImpact": gin.H{
			"Available": true,
			"Pages":     []gin.H{{"ID": "pg1", "Path": "/blog/hello-world", "ProjectID": "prj", "ProjectName": "站点"}},
			"Total":     1, "Truncated": false, "Limit": 30, "Hint": "",
		},
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/page", func(c *gin.Context) { c.HTML(http.StatusOK, "admin/blocks.html", blocksLayoutData(data)) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	os.WriteFile("/tmp/wp-blocks-body.html", rec.Body.Bytes(), 0644)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin/blocks 渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"待重建影响面", "/blog/hello-world", "data-drawer-open"} {
		if !strings.Contains(body, want) {
			t.Errorf("待重建影响面存在时，区块列表页应完整渲染并包含 %q", want)
		}
	}
}
