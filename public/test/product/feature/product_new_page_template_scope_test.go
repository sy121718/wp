package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"
)

const seedProductDetailTemplateName = "商品详情页"

func newNewPageTemplateEngine(t *testing.T) (*gin.Engine, *detailFixture, string) {
	t.Helper()
	base := newDetailFixture(t)
	if base == nil {
		return nil, nil, ""
	}
	other, err := base.projects.Create(context.Background(), &projectdto.CreateReq{Name: "第二站点工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	if other.ID == base.projectID {
		t.Fatalf("第二个工程 id 与第一个相同：%s", other.ID)
	}
	if list, lerr := base.projects.List(context.Background()); lerr != nil || len(list) != 2 {
		t.Fatalf("本文件的判据要求多工程夹具，实际 err=%v 工程数=%d", lerr, len(list))
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(base.products, base.products)
	handle.SetDetailTemplateDeps(base.templates, base.pres)
	engine.GET("/admin/products/new", pageview.UseRenderer(pageview.RenderPage), handle.ProductNewPage)
	return engine, base, other.ID
}

func (f *detailFixture) newPageTemplate(t *testing.T, projectID, name string) string {
	t.Helper()
	res, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: "product", Name: name, ProjectID: projectID,
		DraftDocument: json.RawMessage(detailDocWithMarker(name)),
	})
	if err != nil {
		t.Fatalf("在工程 %s 下建模板 %s 失败: %v", projectID, name, err)
	}
	return res.ID
}

func getNewPage(t *testing.T, engine *gin.Engine, projectID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/new?project="+projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("新建页应 200，实际 %d：%s", rec.Code, oneLine(rec.Body.String()))
	}
	return rec.Body.String()
}

func TestProductNewPageTemplateCardsCarryProjectScope(t *testing.T) {
	engine, f, otherProject := newNewPageTemplateEngine(t)
	if engine == nil {
		return
	}

	mine := f.newPageTemplate(t, f.projectID, "本工程命名详情模板")
	foreign := f.newPageTemplate(t, otherProject, "别站命名详情模板")

	body := getNewPage(t, engine, f.projectID)
	if !strings.Contains(body, "本工程命名详情模板") {
		t.Fatalf("卡片区应列出本工程的模板（漏传工程时内容模板侧回 ErrProjectRequired，"+
			"调用方降级成空清单，卡片区显示「暂无商品详情模板」）：[诊断] 本工程模板id=%v 空态提示=%v "+
			"seed模板=%v 别站模板=%v 卡片区=%q",
			mine, strings.Contains(body, "暂无商品详情模板"), strings.Contains(body, seedProductDetailTemplateName),
			strings.Contains(body, "别站命名详情模板"), tailAround(body, "详情页模板", 600))
	}
	if strings.Contains(body, "别站命名详情模板") {
		t.Fatalf("卡片区不得列出别的工程的模板（工程隔离）：[诊断] 别站模板id=%v", foreign)
	}

	if !strings.Contains(body, seedProductDetailTemplateName) {
		t.Fatalf("卡片区应同时列出本工程的类型默认模板 %q：%q", seedProductDetailTemplateName,
			tailAround(body, "详情页模板", 600))
	}
}
