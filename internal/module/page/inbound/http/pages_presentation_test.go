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

	"go_wp/internal/adminui"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"

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
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "siteSlotBindLabel", "translationOriginText", "reuseHint":
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
			// 视图是强类型了，字段名的存在性由编译期保证 —— 这里只需断言「绑定面板带上了事实」。
			if data.BindBound != bound {
				t.Fatal("panel lost Bound fact")
			}
			// t / tf / title / menu 不再手工塞：它们由 adminui.Prepare 注入。
			// 原先手工塞 4 个键的地方，恰恰是「页壳关切混进视图」的证据 ——
			// 漏塞一个的表现是渲染期 nil，整页中断。
			router := gin.New()
			router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
			router.GET("/t", adminui.PageSlot(SiteSlotsPageTemplate),
				func(c *gin.Context) { pageview.Render(c, data) })
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t", nil))
			class := "btn btn-sm"
			if !bound {
				class += " btn-primary"
			}
			link := `<a class="` + class + `"` + "\n                           " + `href="/admin/site-slots?project=pr&amp;slot=cart#slot-bind-panel"` + "\n                           " + `aria-current="true">` + label + `</a>`
			title := "<strong>" + label + "：" + data.BindSlotNameFallback + "</strong>"
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
