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
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
)

// TestProductTagShortKeysRenderRealLanguages 验证标签短词条的真实双语命中、页面与规则分支，不以默认文案代替词库命中。
func TestProductTagShortKeysRenderRealLanguages(t *testing.T) {
	engine, f := newTagPageEngine(t)
	if f == nil {
		t.Fatal("标签词条验收必须使用真实 PostgreSQL")
	}
	keys := map[string]bool{"tagHitCount": false, "tagHitInfo": false, "tagRuleUnknown": false}
	oldKeys := map[string]bool{
		"admin.product_tags.list.hitCountTail": true,
		"admin.product_tags.list.hitLead":      true,
		"admin.product_tags.rule.unknown":      true,
	}
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == "721-product-tag-short-keys-i18n" {
			for _, match := range regexp.MustCompile(`\('([^']+)', '([^']+)', (?:true|false)\)`).FindAllStringSubmatch(seed.SQL, -1) {
				oldKeys[match[1]] = true
				keys[match[2]] = false
			}
		}
	}
	if len(keys) != 68 {
		t.Fatalf("65 个改名和三个完整句的范围改变: %d", len(keys))
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
	manual, err := f.svc.CreateTag(t.Context(), &productdto.CreateTagReq{ProjectID: f.projectID, Name: "<Manual>", Slug: "manual", Kind: productenums.TagKindManual})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateTag(t.Context(), &productdto.CreateTagReq{ProjectID: f.projectID, Name: "<Empty>", Slug: "empty", Kind: productenums.TagKindManual}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateTag(t.Context(), &productdto.CreateTagReq{ProjectID: f.projectID, Name: "<Rule>", Slug: "rule", Kind: productenums.TagKindRule, RuleType: productenums.TagRuleNewArrival, RuleParams: []byte(`{"days":30}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "Hit", TagIDs: []string{manual.ID}}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, rule, key string
		values          productdto.RuleValuesResp
		params          map[string]string
	}{
		{"new", "new_arrival", "tagNewDesc", productdto.RuleValuesResp{HasDays: true, Days: 30}, map[string]string{"days": "30"}},
		{"new-invalid", "new_arrival", "tagRuleArgsBad", productdto.RuleValuesResp{}, nil},
		{"price-both", "price_range", "tagPriceBoth", productdto.RuleValuesResp{HasMinPrice: true, MinPrice: 10, HasMaxPrice: true, MaxPrice: 20}, map[string]string{"min": "10", "max": "20"}},
		{"price-min", "price_range", "tagPriceMin", productdto.RuleValuesResp{HasMinPrice: true, MinPrice: 0}, map[string]string{"min": "0"}},
		{"price-max", "price_range", "tagPriceMax", productdto.RuleValuesResp{HasMaxPrice: true, MaxPrice: 20}, map[string]string{"max": "20"}},
		{"price-invalid", "price_range", "tagRuleArgsBad", productdto.RuleValuesResp{}, nil},
		{"sale", "on_sale", "tagSaleDesc", productdto.RuleValuesResp{}, nil},
		{"unknown", `<script>alert("x")</script>`, "tagRuleUnknown", productdto.RuleValuesResp{}, map[string]string{"type": `<script>alert("x")</script>`}},
	}
	for _, tc := range cases {
		engine.GET("/test/tag-rule/"+tc.name, func(c *gin.Context) {
			// 只传业务事实，验证局部模板直接使用请求级取词作用域。
			c.HTML(http.StatusOK, "admin/product/tag_rule_text.html", gin.H{"rt": tc.rule, "rv": tc.values})
		})
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		t.Run(lang, func(t *testing.T) {
			request := func(path string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("Accept-Language", lang)
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
					t.Fatalf("真实模板渲染失败: %s: %d %s", path, rec.Code, rec.Body.String())
				}
				return rec
			}
			out := request("/admin/product-tags?project=" + f.projectID).Body.String()
			for _, want := range []string{"</html>", "</form>", "&lt;Manual&gt;", "&lt;Empty&gt;", "&lt;Rule&gt;", `name="days"`, `name="id"`} {
				if !strings.Contains(out, want) {
					t.Fatalf("页面或新建编辑表单缺少 %q", want)
				}
			}
			for _, raw := range []string{"<Manual>", "<Empty>", "<Rule>"} {
				if strings.Contains(out, raw) {
					t.Fatalf("业务名称未转义: %s", raw)
				}
			}
			for _, key := range []string{"tagTitle", "tagNewSubmit", "tagListSave", "tagNewName", "tagListHitTitle", "pagerPrev", "pagerNext"} {
				if !strings.Contains(out, html.EscapeString(i18n.GetText(key, lang))) {
					t.Fatalf("页面未展示指定语言译文: %s/%s", key, lang)
				}
			}
			for _, count := range []string{"0", "1"} {
				want := html.EscapeString(strings.ReplaceAll(i18n.GetText("tagHitCount", lang), "{count}", count))
				if !strings.Contains(out, ">"+want+"</") {
					t.Fatalf("标签计数不是完整参数句: %s/%s", lang, count)
				}
			}
			for _, attr := range []struct{ name, key string }{{"data-count-text", "tagHitCount"}, {"data-hit-text", "tagHitInfo"}} {
				if !strings.Contains(out, attr.name+`="`+html.EscapeString(i18n.GetText(attr.key, lang))+`"`) {
					t.Fatalf("命中面板缺少完整译文: %s/%s", attr.key, lang)
				}
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					want := i18n.GetText(tc.key, lang)
					for name, value := range tc.params {
						want = strings.ReplaceAll(want, "{"+name+"}", value)
					}
					if got := strings.TrimSpace(request("/test/tag-rule/" + tc.name).Body.String()); got != html.EscapeString(want) {
						t.Fatalf("规则句或转义错误: got %q want %q", got, html.EscapeString(want))
					}
				})
			}
		})
	}
}
