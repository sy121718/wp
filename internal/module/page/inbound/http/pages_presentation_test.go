package pagehttp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"net/http"
	"net/http/httptest"
	"strings"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/utils"

	"github.com/gin-gonic/gin"
)

func TestPageRowUpdatedAtCarriesTimeFact(t *testing.T) {
	field, ok := reflect.TypeOf(pagedto.PageRowResp{}).FieldByName("UpdatedAt")
	if !ok || field.Type != reflect.TypeOf(utils.JSONTime{}) {
		t.Fatalf("UpdatedAt must carry JSONTime, got %v", field.Type)
	}
}

// Scan the real source: adding an unused presentation helper is also a regression.
func TestPagePresentationHasNoBackendUIHelpers(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "pages_handle.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		ident, ok := lit.Type.(*ast.Ident)
		if !ok || ident.Name != "pagedto.ScheduleResp" {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || (key.Name != "Action" && key.Name != "Status") {
				continue
			}
			sel, ok := kv.Value.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != key.Name {
				t.Errorf("schedule %s must carry item fact", key.Name)
			}
		}
		return true
	})
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "siteSlotBindLabel", "scheduleActionText", "scheduleStatusText", "translationOriginText", "reuseHint":
			t.Errorf("UI helper remains in backend: %s", fn.Name.Name)
		}
	}
}

func TestSiteSlotBindLabelsFromFacts(t *testing.T) {
	for _, bound := range []bool{false, true} {
		name, label := "unbound", "绑定"
		if bound {
			name, label = "bound", "换绑"
		}
		t.Run(name, func(t *testing.T) {
			candidates := []pagecontract.PageResp{{ID: "pg", DraftPath: "/a"}}
			slots := &pagecontract.SiteSlotListResp{Total: 1, Items: []pagecontract.SiteSlotItemResp{{Slot: "cart", Bound: bound, PageID: "pg"}}}
			data := siteSlotPageData([]projectcontract.ProjectResp{{ID: "pr", Name: "site"}}, "pr", candidates, slots, pageMsg{})
			siteSlotApplyBindPanel(data, candidates, slots, "cart")
			if _, ok := data["BindLabel"]; ok {
				t.Fatal("panel must not carry BindLabel")
			}
			if data["BindBound"] != bound {
				t.Fatal("panel lost Bound fact")
			}
			for _, row := range data["Rows"].([]gin.H) {
				if _, ok := row["BindLabel"]; ok {
					t.Fatal("row must not carry BindLabel")
				}
			}
			// tf 与 t 一起补：模板两处都要（tr 取词、tf 取带 %s 参数的词），
			// 只补 t 会让模板在 tf 那一行拿到 nil —— 渲染期报错，整页中断。
			data["t"] = func(_, fallback string) string { return fallback }
			data["tf"] = func(_, fallback string, _ []string) string { return fallback }
			data["title"], data["menu"] = "slots", "pages"
			router := gin.New()
			router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
			router.GET("/t", func(c *gin.Context) { c.HTML(http.StatusOK, "admin/page/site_slots", data) })
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t", nil))
			class := "btn btn-sm"
			if !bound {
				class += " btn-primary"
			}
			link := `<a class="` + class + `"` + "\n                           " + `href="/admin/site-slots?project=pr&amp;slot=cart#slot-bind-panel"` + "\n                           " + `aria-current="true">` + label + `</a>`
			title := "<strong>" + label + "：" + data["BindSlotNameFallback"].(string) + "</strong>"
			for _, element := range []string{link, title} {
				if strings.Count(rec.Body.String(), element) != 1 {
					t.Fatalf("bind element must equal %q", element)
				}
			}
			want := `<button type="submit" class="btn btn-primary">` + label + "</button>"
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) || !strings.HasSuffix(strings.TrimSpace(rec.Body.String()), "</html>") {
				t.Fatalf("bind button must equal %q; status=%d", want, rec.Code)
			}
		})
	}
}

func TestTranslationProjectionCarriesFacts(t *testing.T) {
	typ := reflect.TypeOf(pagedto.TranslationRowResp{})
	if _, ok := typ.FieldByName("ReuseHint"); ok {
		t.Fatal("ReuseHint is UI text")
	}
	field, ok := typ.FieldByName("ReusePaths")
	if !ok || field.Type != reflect.TypeOf([]string{}) {
		t.Fatal("ReusePaths must carry path facts")
	}
	for _, origin := range []string{pageenums.TranslationOriginHeader, pageenums.TranslationOriginFooter, pageenums.TranslationOriginBlock} {
		if strings.Contains(origin, ".") {
			t.Fatalf("Origin must be a domain fact, got UI key %q", origin)
		}
	}
}
