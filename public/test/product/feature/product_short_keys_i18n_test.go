package feature

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/templates"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/migrations"
)

// TestProductShortKeysResolveMigratedLanguagesAtHTTPOutlets 验证迁移库中的真实译文经过缓存和两个 HTTP 出口，英文不能靠中文兜底通过。
func TestProductShortKeysResolveMigratedLanguagesAtHTTPOutlets(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		t.Fatal("短词条语言链路必须使用真实 PostgreSQL")
	}
	var keys []string
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == "718-product-short-keys-i18n" {
			for _, match := range regexp.MustCompile(`\('[^']+', '([^']+)', (?:true|false)\)`).FindAllStringSubmatch(seed.SQL, -1) {
				keys = append(keys, match[1])
			}
		}
	}
	if len(keys) != 149 {
		t.Fatalf("商品短词条范围改变: %d", len(keys))
	}

	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure), templates.FailedRenderMiddleware(pageview.Fail))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	errorFact := func(key string) error {
		return fmt.Errorf("private SQLSTATE detail: %w", facing.WithStatus(facing.New(key), http.StatusConflict))
	}
	engine.GET("/words/html/:key", pageview.HTMLFragment, func(c *gin.Context) {
		pageview.Fail(c, errorFact(c.Param("key")))
	})
	engine.GET("/words/json/:key", func(c *gin.Context) {
		response.ErrorStatusAuto(c, "product-short-keys", errorFact(c.Param("key")))
	})

	for _, key := range keys {
		for _, lang := range []string{"zh-CN", "en-US"} {
			var want string
			if err := f.db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ? AND status = 1", key, lang).Scan(&want).Error; err != nil {
				t.Fatal(err)
			}
			if want == "" || want == key {
				t.Fatalf("真实词库缺少译文: %s/%s", key, lang)
			}
			if got := i18n.Get(key, lang); got.Lang != lang || got.Value != want {
				t.Fatalf("词库缓存未命中指定语言: %s/%s: %+v", key, lang, got)
			}
			for _, outlet := range []string{"html", "json"} {
				req := httptest.NewRequest(http.MethodGet, "/words/"+outlet+"/"+key, nil)
				req.Header.Set("Accept-Language", lang)
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				body := rec.Body.String()
				if rec.Code != http.StatusConflict || strings.Contains(body, "SQLSTATE") || strings.Contains(body, "private") {
					t.Fatalf("出口状态或诊断隔离错误: %s/%s/%s: %d %s", key, lang, outlet, rec.Code, body)
				}
				if outlet == "html" {
					if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, "data-ui-error") || !strings.Contains(body, html.EscapeString(want)) {
						t.Fatalf("Jet 未渲染真实译文: %s/%s: %s", key, lang, body)
					}
				} else {
					var result struct{ Message string }
					if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Message != want {
						t.Fatalf("JSON 未输出真实译文: %s/%s: %s (%v)", key, lang, body, err)
					}
				}
			}
		}
	}
}
