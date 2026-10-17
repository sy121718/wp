// product_new_page_template_scope_test.go —— 「新建页右侧模板卡片区」的工程作用域（P172）。
//
// 被钉住的缺陷形态：EntityTemplatePort 的两个方法没有工程形参，适配器把 ListReq 的工程留空
// -> 内容模板侧 resolveProjectID 在「工程数 != 1」时回 ErrProjectRequired -> 商品页读模型
// 按**降级**处理（空清单）。
//
// 这一档的失败比 500 更隐蔽：新建页照常 200、不报错、日志干净，卡片区只是显示
// 「暂无商品详情模板，将使用默认结构发布」——「配置读不到」被说成「这一屏没有候选」，
// 用户据此以为模板没配。单工程夹具照不出它（兜底解析出的那一个恰好就是唯一的那个）。
//
// 本文件在 detailFixture 之上再建**第二个工程**，并断言两件事：
//
//	· 本工程的模板真的出现在卡片区（不是空清单 / 不是「暂无」提示）；
//	· 另一个工程的模板**不**出现在这张卡片区（写成「扇出全部工程」也是一种错法）。
package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

// seedProductDetailTemplateName 工程 A 自带的类型默认模板名（迁移 085 的 seed）：
// detailFixture 的顺序是「建工程 -> RunSeeds」，seed 会落在第一个工程上。
const seedProductDetailTemplateName = "商品详情页"

// newNewPageTemplateEngine 装配双工程引擎：只挂新建页入口，内容模板依赖走**与线上同一接线**
// （NewProductPageHandle + SetDetailTemplateDeps，后者把 EntityTemplatePort 转交给 service 读模型）。
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
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(base.products, base.products, base.projects)
	handle.SetDetailTemplateDeps(base.templates, base.pres)
	engine.GET("/admin/products/new", handle.ProductNewPage)
	return engine, base, other.ID
}

// newPageTemplate 在指定工程下建一条商品详情模板（名字互不相同，便于在页面里分辨归属）。
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

// getNewPage 渲染新建整页（显式带工程）。
func getNewPage(t *testing.T, engine *gin.Engine, projectID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/new?project="+projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("新建页应 200，实际 %d：%s", rec.Code, oneLine(rec.Body.String()))
	}
	return rec.Body.String()
}

// TestProductNewPageTemplateCardsCarryProjectScope 卡片区必须列出**本工程**的模板。
//
// 漏传工程时的真实表现（变红实验把适配器的工程实参改回 ""）：卡片区只有
// 「暂无商品详情模板，将使用默认结构发布。」—— 而本工程的模板就在库里。
func TestProductNewPageTemplateCardsCarryProjectScope(t *testing.T) {
	engine, f, otherProject := newNewPageTemplateEngine(t)
	if engine == nil {
		return
	}
	// 两个工程各一条命名模板：谁能出现在卡片区可直接从标题上分辨。
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
	// seed 的类型默认模板也在本工程里，同一屏应一起列出 —— 「只列出命名模板」同样是错法。
	if !strings.Contains(body, seedProductDetailTemplateName) {
		t.Fatalf("卡片区应同时列出本工程的类型默认模板 %q：%q", seedProductDetailTemplateName,
			tailAround(body, "详情页模板", 600))
	}
}
