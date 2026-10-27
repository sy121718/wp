package feature

// product_edit_page_test.go — 商品编辑页（GET /admin/products/edit）。
//
// 本页补的是「商品自身字段」的编辑入口：在这之前，名称 / URL 段 / SKU / 状态 / 价格 /
// 单位 / 重量 / SEO / 图集只有创建时能填，建完之后再也没有入口（详情页只管子资源）。
// 保存链路已迁到 JSON 写基座（internal/templates/static/js/ui/write-base.js 直调
// /api/product/update），页面那条 POST 不存在了。所以本文件守两件事：
//  1. 回填：GET 的控件里是当前值（空表单等于把已有数据擦掉）；
//  2. UpdateReq 的替换语义：多选字段给空数组 = 解绑全部（nil 才是「本次不改」）、
//     主图传空串 = 清除（不传 = 不改）—— 基座按这两条语义发身体，这里直调 service 钉住。
// 表单字段名与端点的逐字对齐由 internal/templates 的声明断言守（data-write-* 那一族）。

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
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

// TestProductEditPageRoundTrip 回填 → 保存 → 落库 → 多选字段整体替换。
func TestProductEditPageRoundTrip(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	// 主图参建：编辑页必须把它回填进媒体控件的隐藏域 —— ProductRowResp 漏赋值时控件恒空，
	// 而保存路径会读那个隐藏域（三种形态见 TestProductEditPageSavesDefaultImage）。
	// 注意归一：/img/x 会被 upload.StorageURL 变成 /storage/img/x，所以只比对文件名片段。
	coverImage := "/img/product-cover.jpg"
	created, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "编辑前", Slug: "before-edit", DefaultPrice: &price,
		DefaultImage: coverImage,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	// 1. 回填：控件里必须是当前值。
	page := getProductEditPage(engine, f.projectID, created.ID)
	for _, want := range []string{
		`value="编辑前"`, `value="before-edit"`,
		`name="status"`, `name="defaultPrice"`, `name="images"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("编辑页缺少回填内容 %q；页面尾部：%s", want, tailOfPage(page, 700))
		}
	}
	// 主图回显单独判：空态渲染出来也是 name="defaultImage" value=""（前缀一模一样），
	// 所以只查「字段名出现」会假绿 —— 要判的是**值非空、且是建商品时写的那张**。
	// URL 会被 upload.StorageURL 归一（可能带 media base 前缀），因此只比对文件名片段。
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

	// 2. 保存：一次把各类字段都改一遍。基座把整张表单发成一份 UpdateReq（文字字段照发、
	// 点名的数值字段转小数、图集两个隐藏域按行切成数组），这里构造同一份。
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
	// SEO 标题 / 描述已与商品名 / 副标题合并（2026-09-30）：表单里没有这两栏，
	// 提交里塞了也不会被采纳 —— 这两列保持原值（这里是新建商品，两列为空）。
	if got.SEOTitle != "" || got.SEODescription != "" {
		t.Fatalf("SEO 两栏不该由表单写入：%q / %q", got.SEOTitle, got.SEODescription)
	}
	if len(got.Images) != 2 {
		t.Fatalf("图集应按行切成 2 条，实际 %v", got.Images)
	}

	// 3. 多选字段的整体替换：先挂一个属性组，再用**空数组**保存一次（基座对「一个都不勾」
	//    发的就是 []，不是不发 —— UpdateReq 里 nil 才是「本次不改」）。
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
	// 全部取消勾选 → 基座发空数组 → 服务端必须当成「解绑全部」。
	saveAttrs([]string{})
	detached, gerr3 := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if gerr3 != nil {
		t.Fatalf("读回商品失败: %v", gerr3)
	}
	if len(detached.AttributeIDs) != 0 {
		t.Fatalf("一个都不勾应解绑全部，实际 %v", detached.AttributeIDs)
	}
}

// TestProductEditPageSavesDefaultImage 主图保存：隐藏域**恒在表单里**，
// 所以「提交空串」= 用户点了移除，「字段缺失」= 本次不改。三种形态都要判 ——
// 处理器漏读它的失效模式是「编辑页能选、保存后又回到旧图」，不报错也不崩。
func TestProductEditPageSavesDefaultImage(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	created, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "主图保存", Slug: "default-image-save",
		DefaultPrice: &price, DefaultImage: "/img/product-cover.jpg",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	// 读回的值可能带 media base 前缀（upload.StorageURL 归一），所以只比对文件名片段。
	readImage := func(stage string) string {
		got, gerr := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
		if gerr != nil {
			t.Fatalf("%s：读回商品失败: %v", stage, gerr)
		}
		return got.DefaultImage
	}
	// 基座整表提交时主图隐藏域**恒在表单里**（空态也发空串），所以这里按同一形状构造：
	// 传 nil 代表「表单里没有这个字段」（不提交），传指针代表「发出去的值」。
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

	// 1. 字段缺失 → 本次不改（不能因为「表单没带」就把图擦掉）。
	save(nil)
	if keep := readImage("未提交主图"); keep != baseVal {
		t.Fatalf("没提交主图不该改动：期望 %q，实际 %q", baseVal, keep)
	}

	// 2. 提交新值 → 落库（漏读这个字段的现场就在这里）。
	newImage := "/img/product-cover-new.png"
	save(&newImage)
	if got := readImage("提交新主图"); !strings.Contains(got, "product-cover-new.png") {
		t.Fatalf("主图未落库：实际 %q", got)
	}

	// 3. 提交空串 → 明确清空（媒体控件的「移除」按钮就是把隐藏域置空后提交）。
	emptyImage := ""
	save(&emptyImage)
	if got := readImage("提交空主图"); got != "" {
		t.Fatalf("提交空串应清空主图，实际 %q", got)
	}
}

// TestProductEditPageRejectsEmptyName 名称为空：服务端拒绝（不信任前端的 required），
// 库里保持原值。
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
	// 基座把 name 原样发（文本框恒在表单里），service 不信任前端 required —— 空白即拒绝。
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

// failedAttributeListService 制造「属性组清单读失败」这一档。
//
// 为什么 GET 那一半改成在读模型的**产出**上注入：取数编排已下沉（service.ProductEditPage 内部
// 调 s.ListAttributes），而 Go 的嵌入方法不经过外层包装 —— 覆盖 ListAttributes 对读模型不再生效，
// 断言会变成「页面 200、属性组还在」的假绿。现在 ProductEditPage 不再走嵌入的 ProductService，
// 而是显式委托给真读模型，再覆盖成故障档：与真实故障在视图层同形（AttributeOptionsErr 非空、
// 候选为空、整页仍 200）。
//
// 本文件只守 GET 这一半：保存链路已迁到 /api/product/update 的 JSON 基座，页面 POST 不存在了。
type failedAttributeListService struct {
	productcontract.ProductService
	reader productcontract.ProductPageReader
}

func (s failedAttributeListService) ListAttributes(context.Context, *productdto.ListAttributeReq) ([]*productdto.AttributeResp, error) {
	return nil, errors.New("injected attribute list failure: internal detail")
}

func (s failedAttributeListService) ProductEditPage(ctx context.Context, req *productdto.ProductEditPageReq) (*productdto.ProductEditPageResp, error) {
	view, err := s.reader.ProductEditPage(ctx, req)
	if view != nil {
		view.AttributeOptionsErr = errors.New("injected attribute list failure: internal detail")
		view.AttributeOptions = nil
	}
	return view, err
}

// 读模型接口是一个整体：本桩只覆盖 ProductEditPage 一条，其余几条也必须在 —— 少一条就编译不过
// （reader 是构造实参的静态类型，不再有运行期断言 → 500 兜底页这条路径）。
func (s failedAttributeListService) ProductNewPage(ctx context.Context, req *productdto.ProductNewPageReq) (*productdto.ProductNewPageResp, error) {
	return s.reader.ProductNewPage(ctx, req)
}

func (s failedAttributeListService) ProductListPage(ctx context.Context, req *productdto.ProductListPageReq) (*productdto.ProductListPageResp, error) {
	return s.reader.ProductListPage(ctx, req)
}

func (s failedAttributeListService) ProductDetailPage(ctx context.Context, req *productdto.ProductDetailPageReq) (*productdto.ProductDetailPageResp, error) {
	return s.reader.ProductDetailPage(ctx, req)
}

func (s failedAttributeListService) ProductCreateForm(ctx context.Context, req *productdto.ProductCreateFormReq) (*productdto.ProductCreateFormResp, error) {
	return s.reader.ProductCreateForm(ctx, req)
}

func (s failedAttributeListService) ProductPricingPage(ctx context.Context, req *productdto.ProductPricingPageReq) (*productdto.ProductPricingPageResp, error) {
	return s.reader.ProductPricingPage(ctx, req)
}

func (s failedAttributeListService) ProductDetailTemplatePage(context.Context, *productdto.ProductDetailTemplatePageReq) (*productdto.ProductDetailTemplatePageResp, error) {
	return nil, nil
}

func (s failedAttributeListService) ProductPublishRecordPage(ctx context.Context, req *productdto.ProductPublishRecordPageReq) (*productdto.ProductPublishRecordPageResp, error) {
	return s.reader.ProductPublishRecordPage(ctx, req)
}

func (s failedAttributeListService) ProductCategoriesPage(ctx context.Context, req *productdto.ProductCategoriesPageReq) (*productdto.ProductCategoriesPageResp, error) {
	return s.reader.ProductCategoriesPage(ctx, req)
}

func (s failedAttributeListService) ProductBrandsPage(ctx context.Context, req *productdto.ProductBrandsPageReq) (*productdto.ProductBrandsPageResp, error) {
	return s.reader.ProductBrandsPage(ctx, req)
}

func (s failedAttributeListService) ProductTagsPage(ctx context.Context, req *productdto.ProductTagsPageReq) (*productdto.ProductTagsPageResp, error) {
	return s.reader.ProductTagsPage(ctx, req)
}

func (s failedAttributeListService) CategoryParentOptions(ctx context.Context, req *productdto.ProductCategoryParentsReq) (productdto.CategoryParentOptionsResp, error) {
	return s.reader.CategoryParentOptions(ctx, req)
}

func (s failedAttributeListService) ProductAttributesPage(ctx context.Context, req *productdto.ProductAttributesPageReq) (*productdto.ProductAttributesPageResp, error) {
	return s.reader.ProductAttributesPage(ctx, req)
}

func (s failedAttributeListService) ProductPublishAttempts(ctx context.Context, req *productdto.PublishAttemptListReq) (*productdto.PublishAttemptListResp, error) {
	return s.reader.ProductPublishAttempts(ctx, req)
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
	handle := producthttp.NewProductPageHandle(failedAttributeListService{ProductService: f.svc, reader: f.svc}, failedAttributeListService{ProductService: f.svc, reader: f.svc}, f.projects)
	engine.GET("/admin/products/edit", adminui.PageSlot(producthttp.ProductEditPageTemplate), handle.ProductEditPage)

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet,
		"/admin/products/edit?project="+f.projectID+"&product="+created.ID, nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `role="alert"`) ||
		!strings.Contains(page.Body.String(), "内部错误，请稍后重试") ||
		strings.Contains(page.Body.String(), `id="product-edit-main"`) ||
		strings.Contains(page.Body.String(), `data-write-url="/api/product/variant/save"`) ||
		strings.Contains(page.Body.String(), `data-write-url="/api/product/rating/add"`) ||
		strings.Contains(page.Body.String(), "injected attribute list failure") {
		t.Fatalf("属性取数失败应展示归口错误且不渲染危险表单：状态=%d，页面尾部=%s", page.Code, tailOfPage(page.Body.String(), 900))
	}
}

// tailOfPage 取页面尾部若干字符（渲染中断时用来看断在哪一段）。
func tailOfPage(page string, n int) string {
	if len(page) <= n {
		return page
	}
	return "…" + page[len(page)-n:]
}
