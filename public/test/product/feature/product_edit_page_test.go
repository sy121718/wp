package feature

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
)

func TestProductEditPageRoundTrip(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	price := 99.0

	coverImage := "/storage/img/product-cover.jpg"
	created, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "编辑前", Slug: "before-edit", DefaultPrice: &price,
		DefaultImage: coverImage,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	page := getProductEditPage(engine, f.projectID, created.ID)
	for _, want := range []string{
		`value="编辑前"`, `value="before-edit"`,
		`name="status"`, `name="defaultPrice"`, `name="images"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("编辑页缺少回填内容 %q；页面尾部：%s", want, tailOfPage(page, 700))
		}
	}

	const imgMarker = `name="defaultImage" value="`
	imgIdx := strings.Index(page, imgMarker)
	if imgIdx < 0 {
		t.Fatalf("编辑页没有主图控件；页面尾部：%s", tailOfPage(page, 700))
	}
	imgVal := page[imgIdx+len(imgMarker):]
	if end := strings.IndexByte(imgVal, '"'); end >= 0 {
		imgVal = imgVal[:end]
	}
	if !strings.Contains(imgVal, "product-cover.jpg") {
		t.Fatalf("编辑页未回显主图（隐藏域值 = %q）；页面尾部：%s", imgVal, tailOfPage(page, 700))
	}

	newName, newSlug, subtitle := "编辑后", "after-edit", "副标题"
	status, unit := productenums.StatusPublished, "件"
	weight, newPrice := 0.5, 88.5
	if _, uerr := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: created.ID, ProjectID: f.projectID,
		Name: &newName, Slug: &newSlug, Subtitle: &subtitle, Status: &status, Unit: &unit,
		Weight: &weight, DefaultPrice: &newPrice,
		Images:    []string{"https://cdn.example.com/a.jpg", "https://cdn.example.com/b.jpg"},
		ImageAlts: []string{"图一", "图二"},
	}); uerr != nil {
		t.Fatalf("保存失败: %v", uerr)
	}

	got, gerr := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if gerr != nil {
		t.Fatalf("读回商品失败: %v", gerr)
	}
	if got.Name != "编辑后" || got.Slug != "after-edit" || got.Status != productenums.StatusPublished {
		t.Fatalf("名称 / URL 段 / 状态未落库：%s / %s / %s", got.Name, got.Slug, got.Status)
	}
	if got.Subtitle != "副标题" || got.Unit != "件" {
		t.Fatalf("副标题 / 单位未落库：%q / %q", got.Subtitle, got.Unit)
	}
	if got.Weight == nil || *got.Weight != 0.5 {
		t.Fatalf("重量未落库：%v", got.Weight)
	}
	if got.DefaultPrice == nil || *got.DefaultPrice != 88.5 {
		t.Fatalf("默认价格未落库：%v", got.DefaultPrice)
	}

	if got.SEOTitle != "" || got.SEODescription != "" {
		t.Fatalf("SEO 两栏不该由表单写入：%q / %q", got.SEOTitle, got.SEODescription)
	}
	if len(got.Images) != 2 {
		t.Fatalf("图集应按行切成 2 条，实际 %v", got.Images)
	}

	attr, aerr := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color",
	})
	if aerr != nil {
		t.Fatalf("创建属性组失败: %v", aerr)
	}
	saveAttrs := func(ids []string) {
		t.Helper()
		nm := "编辑后"
		if _, uerr := f.svc.Update(ctx, &productdto.UpdateReq{
			ID: created.ID, ProjectID: f.projectID, Name: &nm, AttributeIDs: ids,
		}); uerr != nil {
			t.Fatalf("保存失败: %v", uerr)
		}
	}
	saveAttrs([]string{attr.ID})
	attached, gerr2 := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if gerr2 != nil {
		t.Fatalf("读回商品失败: %v", gerr2)
	}
	if len(attached.AttributeIDs) != 1 || attached.AttributeIDs[0] != attr.ID {
		t.Fatalf("属性组应已挂上，实际 %v", attached.AttributeIDs)
	}

	saveAttrs([]string{})
	detached, gerr3 := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if gerr3 != nil {
		t.Fatalf("读回商品失败: %v", gerr3)
	}
	if len(detached.AttributeIDs) != 0 {
		t.Fatalf("一个都不勾应解绑全部，实际 %v", detached.AttributeIDs)
	}
}

func TestProductEditPageSavesDefaultImage(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	created, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "主图保存", Slug: "default-image-save",
		DefaultPrice: &price, DefaultImage: "/storage/img/product-cover.jpg",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	readImage := func(stage string) string {
		got, gerr := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
		if gerr != nil {
			t.Fatalf("%s：读回商品失败: %v", stage, gerr)
		}
		return got.DefaultImage
	}

	save := func(image *string) {
		t.Helper()
		nm := "主图保存"
		if _, uerr := f.svc.Update(ctx, &productdto.UpdateReq{
			ID: created.ID, ProjectID: f.projectID, Name: &nm, DefaultImage: image,
		}); uerr != nil {
			t.Fatalf("保存失败: %v", uerr)
		}
	}
	baseVal := readImage("建后")
	if !strings.Contains(baseVal, "product-cover.jpg") {
		t.Fatalf("建商品时的主图没落库：%q", baseVal)
	}

	save(nil)
	if keep := readImage("未提交主图"); keep != baseVal {
		t.Fatalf("没提交主图不该改动：期望 %q，实际 %q", baseVal, keep)
	}

	newImage := "/storage/img/product-cover-new.png"
	save(&newImage)
	if got := readImage("提交新主图"); !strings.Contains(got, "product-cover-new.png") {
		t.Fatalf("主图未落库：实际 %q", got)
	}

	emptyImage := ""
	save(&emptyImage)
	if got := readImage("提交空主图"); got != "" {
		t.Fatalf("提交空串应清空主图，实际 %q", got)
	}
}

func TestProductEditPageRejectsEmptyName(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	created, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "原名", Slug: "keep-name",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	blank := "   "
	_, uerr := f.svc.Update(ctx, &productdto.UpdateReq{ID: created.ID, ProjectID: f.projectID, Name: &blank})
	if uerr == nil || uerr.Error() != productenums.ErrNameRequired {
		t.Fatalf("空名称应被拒绝并给 ErrNameRequired，实际 %v", uerr)
	}
	got, gerr := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if gerr != nil {
		t.Fatalf("读回商品失败: %v", gerr)
	}
	if got.Name != "原名" {
		t.Fatalf("名称不该被空值覆盖，实际 %q", got.Name)
	}
}

type failedAttributeListService struct {
	productcontract.ProductService
	calls     int
	projectID string
}

func (s *failedAttributeListService) CatalogAttributes(_ context.Context, projectID string) ([]*productdto.AttributeResp, error) {
	s.calls++
	s.projectID = projectID
	return nil, errors.New("injected attribute list failure: internal detail")
}

func TestProductEditPageAttributeListFailure(t *testing.T) {
	_, f := newVariantPageEngine(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	attr, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{ProjectID: f.projectID, Name: "颜色", Key: "color"})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	created, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "原名", Slug: "original", AttributeIDs: []string{attr.ID}})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	failing := &failedAttributeListService{ProductService: f.svc}
	handle := producthttp.NewProductPageHandle(failing, f.svc)
	engine.GET("/admin/products/edit", pageview.UseRenderer(adminui.RenderPage), handle.ProductEditPage)

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet,
		"/admin/products/edit?project="+f.projectID+"&product="+created.ID, nil))
	if failing.calls != 1 || failing.projectID != f.projectID {
		t.Fatalf("属性查询故障未在当前工程注入：calls=%d project=%q", failing.calls, failing.projectID)
	}
	if page.Code != http.StatusInternalServerError || !strings.HasPrefix(page.Header().Get("Content-Type"), "application/json") ||
		!strings.Contains(page.Body.String(), "内部错误") || strings.Contains(page.Body.String(), "<html") ||
		strings.Contains(page.Body.String(), "injected attribute list failure") || strings.Contains(page.Body.String(), "internal detail") {
		t.Fatalf("属性取数失败应返回 500 JSON 且不泄露内部原文：%d %s", page.Code, page.Body.String())
	}

}

func tailOfPage(page string, n int) string {
	if len(page) <= n {
		return page
	}
	return "…" + page[len(page)-n:]
}
