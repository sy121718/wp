// product_category_parent_render_test.go — 父级候选片段真的渲染出候选按钮。
//
// 为什么单独一条：候选行的形状刚从 gin.H{"ID","Label"} 换成 dto.PickOptionResp（Value/Label），
// 模板读的是 option.Value。两者不一致时页面**不报错**——Jet 在具名 map 上取不存在的键会
// 渲染失败，而结构体上取不到字段直接编译不过；换而言之这条断言钉子钉的是「控制器交的形状
// 与模板读的字段是同一个」，而这正是最容易静默漂移的地方。
package producthttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
)

func TestProductCategoryParentsRendersPickedOptions(t *testing.T) {
	reader := &productPageReaderStub{parentOptions: productdto.CategoryParentOptionsResp{
		"Options": []productdto.PickOptionResp{
			{Value: "cat-1", Label: "上衣"},
			{Value: "cat-2", Label: "裤子"},
		},
	}}
	h := &ProductPageHandle{reader: reader, products: reader}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/pick", adminui.BareSlot(ProductCategoryParentsTemplate), h.ProductCategoryParents)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pick?project=p1&keyword=%E8%A1%A3&page=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("片段应 200，得到 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-category-parent-choice="cat-1"`,
		`data-category-parent-choice="cat-2"`,
		">上衣<",
		">裤子<",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("片段缺少 %q：%s", want, body)
		}
	}
	// 入参原样转交给读模型：键名 project/keyword/page 与模板链接一致。
	if len(reader.parentOptionReqs) != 1 {
		t.Fatalf("父级候选应只查一次，实际 %d 次", len(reader.parentOptionReqs))
	}
	got := reader.parentOptionReqs[0]
	if got.ProjectID != "p1" || got.Keyword != "衣" || got.Page != 2 {
		t.Fatalf("入参转发不对：%+v", *got)
	}
}
