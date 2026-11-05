package feature

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/seo/scoreview"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
)

// TestProductSEOShortKeysRenderRealLanguages 验证实际 SEO 模板的短 key、真实词库命中和完整参数句，不靠中文 fallback 通过英文验收。
func TestProductSEOShortKeysRenderRealLanguages(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		t.Fatal("SEO 词条验收必须使用真实 PostgreSQL")
	}
	keys := map[string]bool{}
	renames := map[string]string{}
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == "719-product-seo-short-keys-i18n" {
			for _, match := range regexp.MustCompile(`\('([^']+)', '([^']+)', (?:true|false)\)`).FindAllStringSubmatch(seed.SQL, -1) {
				keys[match[2]] = false
				renames[match[1]] = match[2]
			}
		}
	}
	if len(keys) != 104 {
		t.Fatalf("SEO 短词条范围改变: %d", len(keys))
	}
	paths := []string{"admin/product/product_seo_panel.html", "fragments/seo_score.html", "fragments/seo_grade.html", "fragments/seo_section_name.html", "fragments/seo_issue_line.html", "fragments/seo_dup_kind.html"}
	var checks []string
	for _, path := range paths {
		source, err := os.ReadFile(filepath.Join(attrTemplateRoot(), path))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range regexp.MustCompile(`\b(?:t|tr|tf|tn)\("([^"]+)"`).FindAllStringSubmatch(string(source), -1) {
			if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,14}$`).MatchString(match[1]) {
				t.Fatalf("SEO 模板仍使用非完整短 key: %s: %s", path, match[1])
			}
			if _, ok := keys[match[1]]; ok {
				keys[match[1]] = true
			}
		}
		if path == "fragments/seo_issue_line.html" {
			for _, match := range regexp.MustCompile(`if k == "([^"]+)"`).FindAllStringSubmatch(string(source), -1) {
				checks = append(checks, match[1])
			}
		}
	}
	if len(checks) != 25 {
		t.Fatalf("检查项分支范围改变: %d", len(checks))
	}
	for key, used := range keys {
		if !used {
			t.Fatalf("迁移词条没有真实模板消费者: %s", key)
		}
		for _, lang := range []string{"zh-CN", "en-US"} {
			var want string
			if err := f.db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ? AND status = 1", key, lang).Scan(&want).Error; err != nil {
				t.Fatal(err)
			}
			if got := i18n.Get(key, lang); want == "" || got.Lang != lang || got.Value != want {
				t.Fatalf("SEO 未命中指定语言真实译文: %s/%s: %+v", key, lang, got)
			}
		}
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		t.Run(lang, func(t *testing.T) {
			engine := gin.New()
			engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
			engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			var template string
			var data any
			engine.GET("/seo", func(c *gin.Context) {
				if values, ok := data.(map[string]any); ok {
					text.Inject(c, values)
					values["tr"] = text.Scope(c).Translate
				}
				c.HTML(http.StatusOK, template, data)
			})
			render := func(path string, values any) string {
				t.Helper()
				template, data = path, values
				req := httptest.NewRequest(http.MethodGet, "/seo", nil)
				req.Header.Set("Accept-Language", lang)
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || rec.Body.Len() == 0 {
					t.Fatalf("真实 SEO 模板渲染失败: %s: %d %s", path, rec.Code, rec.Body.String())
				}
				return rec.Body.String()
			}
			for _, check := range checks {
				out := render("fragments/seo_issue_line.html", map[string]any{"key": check, "actual": "2/5"})
				for _, suffix := range []string{"", ".benchmark", ".hint"} {
					// 仅在测试中定位迁移对；生产模板不得由检查项拼出词条 key。
					old := "admin.seo.check." + check + suffix
					key := renames[old]
					if key == "" || !strings.Contains(out, html.EscapeString(i18n.GetText(key, lang))) {
						t.Fatalf("检查项未展示指定语言译文: %s/%s: %s", check, suffix, out)
					}
				}
				if !strings.Contains(out, "2/5") {
					t.Fatalf("检查项丢失实测值: %s", out)
				}
			}
			facts := &productservice.SeoScoreFacts{Title: "<Title>", Duplicates: []productservice.SeoTitleDuplicate{{Kind: "product", Title: "<Other>"}}}
			out := render("admin/product/product_seo_panel.html", facts)
			want := strings.NewReplacer("{title}", "<Title>", "{count}", "1").Replace(i18n.GetText("seoDupLine", lang))
			if !strings.Contains(out, html.EscapeString(want)) || !strings.Contains(out, "</div>") || strings.Contains(out, "<Title>") {
				t.Fatalf("商品重复标题不是转义的完整参数句: %s", out)
			}
			shared := scoreview.View{OK: true, Title: "<Title>", Duplicates: []scoreview.Duplicate{{Kind: "product", Page: "/other"}}}
			out = render("fragments/seo_score.html", map[string]any{"Score": shared})
			if !strings.Contains(out, html.EscapeString(want)) || strings.Count(out, html.EscapeString(want)) != 1 || !strings.Contains(out, "</div>") {
				t.Fatalf("共享评分未复用完整参数句或重复展示: %s", out)
			}
		})
	}
}
