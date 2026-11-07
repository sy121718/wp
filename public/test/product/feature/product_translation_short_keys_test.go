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
	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
	view "go_wp/pkg/view"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	text "go_wp/pkg/view/jet"
	"go_wp/public/migrations"
)

// TestProductTranslationShortKeysRenderRealLanguages 验证翻译工作台真实双语、完整参数句与无目标语言分支。
func TestProductTranslationShortKeysRenderRealLanguages(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		t.Fatal("翻译词条验收必须使用真实 PostgreSQL")
	}
	keys := map[string]bool{"trScopeInfo": false, "trProgress": false, "trGroupInfo": false, "trNoTargetDesc": false, "trSourceOnly": false}
	oldKeys := map[string]bool{}
	for _, fragment := range []string{"scopeLead", "scopeTail", "doneLead", "doneTail", "noTargetLang.desc", "noTargetLang.descTail", "groupMetaLead", "groupMetaMid", "groupMetaTail"} {
		oldKeys["admin.product_translations."+fragment] = true
	}
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == "722-product-translation-short-keys-i18n" {
			for _, match := range regexp.MustCompile(`\('([^']+)', '([^']+)', (?:true|false)\)`).FindAllStringSubmatch(seed.SQL, -1) {
				oldKeys[match[1]] = true
				keys[match[2]] = false
			}
		}
	}
	if len(keys) != 35 {
		t.Fatalf("30 个改名和五个完整句的范围改变: %d", len(keys))
	}
	paths, err := filepath.Glob(filepath.Join(attrTemplateRoot(), "admin/product/*.html"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("找不到真实商品模板: %v", err)
	}
	call := regexp.MustCompile(`(?:\b(?:t|tr|tf|tn|gfTr|vfTr)|\.\["t"\])\s*\(\s*"([^"]+)"`)
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range call.FindAllStringSubmatch(string(source), -1) {
			if oldKeys[match[1]] {
				t.Fatalf("商品模板仍消费本批旧 key: %s: %s", path, match[1])
			}
			if _, ok := keys[match[1]]; ok {
				keys[match[1]] = true
			}
		}
	}
	for key, used := range keys {
		if !used {
			t.Fatalf("本批词条没有真实模板消费者: %s", key)
		}
		for _, lang := range []string{"zh-CN", "en-US"} {
			var want string
			if err := f.db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ? AND status = 1", key, lang).Scan(&want).Error; err != nil {
				t.Fatal(err)
			}
			if got := i18n.Get(key, lang); want == "" || got.Lang != lang || got.Value != want {
				t.Fatalf("未命中指定语言的真实译文: %s/%s: %+v", key, lang, got)
			}
		}
	}
	enabled := true
	if _, err := f.projects.SaveLocales(t.Context(), &projectdto.LocalesSaveReq{ProjectID: f.projectID, Locales: []projectdto.LocaleItem{
		{Lang: "zh-CN", SortOrder: 1, IsDefault: true, Enabled: &enabled}, {Lang: "en-US", SortOrder: 2, Enabled: &enabled},
	}}); err != nil {
		t.Fatal(err)
	}
	f.svc.SetContentStore(i18n.NewDBContentStore(f.db))
	product, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "<Source>", Slug: "source"})
	if err != nil {
		t.Fatal(err)
	}
	engine := trWorkbench(t, &trFixture{db: f.db, products: f.svc, projects: f.projects, projectID: f.projectID})
	// 没有源语言字段的空态是模板的合法可选数据分支，不注入取词函数到业务数据。
	empty := gin.New()
	empty.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	empty.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	empty.GET("/empty", func(c *gin.Context) {
		view.RenderPage(c, "admin/product/product_translations.html", gin.H{
			"Scope":        gin.H{"Projects": []any{}},
			"Translations": gin.H{"ProductID": "", "ProjectID": "", "Langs": []string{}},
		})
	})
	request := func(t *testing.T, e *gin.Engine, path, lang string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		// 页面 URL 的 lang 是目标译文语言；界面语言必须用 cookie，否则会被查询参数覆盖。
		req.AddCookie(&http.Cookie{Name: "lang", Value: lang})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "</html>") {
			t.Fatalf("真实页面渲染失败: %d %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		t.Run(lang, func(t *testing.T) {
			out := request(t, engine, "/admin/products/translations?project="+f.projectID+"&product="+product.ID+"&lang=en-US", lang)
			for _, key := range []string{"trTitle", "trScopeInfo", "trSaveAll", "trField", "trTargetCol", "trFieldProdName", "trStateMiss"} {
				if !strings.Contains(out, html.EscapeString(i18n.GetText(key, lang))) {
					t.Fatalf("页面缺少真实译文: %s/%s", key, lang)
				}
			}
			if strings.Contains(out, "<Source>") || !strings.Contains(out, "&lt;Source&gt;") || !strings.Contains(out, "</textarea>") || !strings.Contains(out, "</form>") {
				t.Fatal("业务原文未转义或保存表单未闭合")
			}
			progress := strings.NewReplacer("{done}", "0", "{total}", "1").Replace(i18n.GetText("trProgress", lang))
			group := strings.NewReplacer("{count}", "1", "{type}", "product").Replace(i18n.GetText("trGroupInfo", lang))
			for _, want := range []string{progress, group} {
				if !strings.Contains(out, html.EscapeString(want)) {
					t.Fatalf("页面未使用完整参数句: %q", want)
				}
			}
			if out := request(t, empty, "/empty", lang); !strings.Contains(out, html.EscapeString(i18n.GetText("trNoTargetDesc", lang))) {
				t.Fatal("缺少源语言事实时未展示完整空态")
			}
		})
	}
	if err := f.db.Exec("INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text, engine) VALUES (?, 'product.name', 'en-US', '<Source>', '<Target>', 'manual')", i18n.ContentHash("<Source>")).Error; err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		out := request(t, engine, "/admin/products/translations?project="+f.projectID+"&product="+product.ID+"&lang=en-US", lang)
		want := strings.NewReplacer("{done}", "1", "{total}", "1").Replace(i18n.GetText("trProgress", lang))
		if !strings.Contains(out, html.EscapeString(want)) || !strings.Contains(out, html.EscapeString(i18n.GetText("trStateDone", lang))) || !strings.Contains(out, "&lt;Target&gt;") || strings.Contains(out, "<Target>") {
			t.Fatal("已完成数量、状态或译文转义错误")
		}
	}
	if _, err := f.projects.SaveLocales(t.Context(), &projectdto.LocalesSaveReq{ProjectID: f.projectID, Locales: []projectdto.LocaleItem{{Lang: "zh-CN", SortOrder: 1, IsDefault: true, Enabled: &enabled}}}); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		out := request(t, engine, "/admin/products/translations?project="+f.projectID, lang)
		want := strings.ReplaceAll(i18n.GetText("trSourceOnly", lang), "{lang}", "zh-CN")
		if !strings.Contains(out, html.EscapeString(want)) || strings.Contains(out, `name="rowTarget"`) {
			t.Fatal("单语言工程未展示完整源语言说明或仍有保存输入")
		}
	}
	// 语言清单是进程级默认语言来源：收尾恢复双语言，避免影响同包其他用例。
	if _, err := f.projects.SaveLocales(t.Context(), &projectdto.LocalesSaveReq{ProjectID: f.projectID, Locales: []projectdto.LocaleItem{
		{Lang: "zh-CN", SortOrder: 1, IsDefault: true, Enabled: &enabled}, {Lang: "en-US", SortOrder: 2, Enabled: &enabled},
	}}); err != nil {
		t.Fatal(err)
	}
}
