package feature

// product_edit_page_test.go — 商品编辑页（GET /admin/products/edit + POST /admin/products/update）。
//
// 本页补的是「商品自身字段」的编辑入口：在这之前，名称 / URL 段 / SKU / 状态 / 价格 /
// 单位 / 重量 / SEO / 图集只有创建时能填，建完之后再也没有入口（详情页只管子资源）。
// 链路覆盖三件事，每件都是判据式的，不是「渲染出来就算」：
//  1. 回填：GET 的控件里是当前值（空表单等于把已有数据擦掉）；
//  2. 保存：POST 真的落库，并 302 回编辑页（PRG，带 ?done= 回执）；
//  3. 整体替换语义：多选字段「一个都不勾」= 解绑全部，而不是「本次不改」——
//     浏览器在没有任何勾选时根本不提交该字段，透传 nil 会把解绑变成静默无操作。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
)

// editSaveLocation 提交编辑表单并返回提示页的回跳地址（成功走 shell.RenderJump，不再 302）。
func editSaveLocation(t *testing.T, engine *gin.Engine, form url.Values) string {
	t.Helper()
	rec := postForm(engine, "/admin/products/update", form)
	assertJumpOK(t, rec)
	return jumpBackHref(t, rec)
}

// TestProductEditPageRoundTrip 回填 → 保存 → 落库 → 多选字段整体替换。
func TestProductEditPageRoundTrip(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	created, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "编辑前", Slug: "before-edit", DefaultPrice: &price,
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

	// 2. 保存：一次提交把各类字段都改一遍。
	loc := editSaveLocation(t, engine, url.Values{
		"projectId":    {f.projectID},
		"id":           {created.ID},
		"name":         {"编辑后"},
		"slug":         {"after-edit"},
		"subtitle":     {"副标题"},
		"status":       {productenums.StatusPublished},
		"unit":         {"件"},
		"weight":       {"0.5"},
		"defaultPrice": {"88.50"},
		"images":       {"https://cdn.example.com/a.jpg\nhttps://cdn.example.com/b.jpg"},
		"imageAlts":    {"图一\n图二"},
	})
	// 提示页回跳落点是编辑页（而不是列表）并带上商品 id；成功回执走响应体，不再带 ?done=。
	if !strings.HasPrefix(loc, "/admin/products/edit?") || !strings.Contains(loc, "product="+created.ID) {
		t.Fatalf("保存成功应回编辑页，实际 %s", loc)
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

	// 3. 多选字段的整体替换：先挂一个属性组，再用「完全不提交 attributeIds」保存一次。
	attr, aerr := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color",
	})
	if aerr != nil {
		t.Fatalf("创建属性组失败: %v", aerr)
	}
	editSaveLocation(t, engine, url.Values{
		"projectId":    {f.projectID},
		"id":           {created.ID},
		"name":         {"编辑后"},
		"attributeIds": {attr.ID},
	})
	attached, gerr2 := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if gerr2 != nil {
		t.Fatalf("读回商品失败: %v", gerr2)
	}
	if len(attached.AttributeIDs) != 1 || attached.AttributeIDs[0] != attr.ID {
		t.Fatalf("属性组应已挂上，实际 %v", attached.AttributeIDs)
	}
	// 全部取消勾选 → 浏览器不提交 attributeIds → 服务端必须当成「解绑全部」。
	editSaveLocation(t, engine, url.Values{
		"projectId": {f.projectID},
		"id":        {created.ID},
		"name":      {"编辑后"},
	})
	detached, gerr3 := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if gerr3 != nil {
		t.Fatalf("读回商品失败: %v", gerr3)
	}
	if len(detached.AttributeIDs) != 0 {
		t.Fatalf("一个都不勾应解绑全部，实际 %v", detached.AttributeIDs)
	}
}

// TestProductEditPageRejectsEmptyName 名称为空：服务端拒绝（不信任前端的 required），
// 回编辑页并带可读原因，库里保持原值。
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
	rec := postForm(engine, "/admin/products/update", url.Values{
		"projectId": {f.projectID},
		"id":        {created.ID},
		"name":      {"   "},
	})
	body := assertJumpErr(t, rec)
	if back := jumpBackHref(t, rec); !strings.Contains(back, "/admin/products/edit?") {
		t.Fatalf("空名称应回编辑页，实际回跳 %s", back)
	}
	// 错误文案必须过白名单：裸 key 或内部报错都不算。
	if strings.Contains(body, productenums.ErrNameRequired) {
		t.Fatalf("提示页不该出现裸 key：%s", body)
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
}

func (s failedAttributeListService) ListAttributes(context.Context, *productdto.ListAttributeReq) ([]*productdto.AttributeResp, error) {
	return nil, errors.New("injected attribute list failure: internal detail")
}

func TestProductEditPageRejectsInvalidNumbersWithoutPartialWrite(t *testing.T) {
	for _, tc := range []struct {
		name, field, invalid string
	}{
		{name: "defaultPrice", field: "defaultPrice", invalid: "not-a-price"},
		{name: "weight", field: "weight", invalid: "not-a-weight"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, f := newVariantPageEngine(t)
			if engine == nil {
				return
			}
			ctx := context.Background()
			price, weight := 49.0, 2.0
			created, err := f.svc.Create(ctx, &productdto.CreateReq{
				ProjectID: f.projectID, Name: "原名", Slug: "original", DefaultPrice: &price, Weight: &weight,
			})
			if err != nil {
				t.Fatalf("创建商品失败: %v", err)
			}
			attr, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
				ProjectID: f.projectID, Name: "颜色", Key: "color",
			})
			if err != nil {
				t.Fatalf("创建属性组失败: %v", err)
			}
			form := url.Values{
				"projectId": {f.projectID}, "id": {created.ID}, "name": {"用户刚填的名称"},
				"defaultPrice": {"51.25"}, "weight": {"3.5"}, "subtitle": {"用户刚填的副标题"},
				"attributeIds": {attr.ID}, "images": {"https://example.com/user.jpg"},
			}
			form.Set(tc.field, tc.invalid)
			rec := postForm(engine, "/admin/products/update", form)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `role="alert"`) ||
				!strings.Contains(rec.Body.String(), `value="`+tc.invalid+`"`) ||
				!strings.Contains(rec.Body.String(), `value="用户刚填的名称"`) ||
				!strings.Contains(rec.Body.String(), `value="`+attr.ID+`" checked`) ||
				!strings.Contains(rec.Body.String(), "https://example.com/user.jpg") ||
				!strings.Contains(rec.Body.String(), "格式无效") {
				t.Fatalf("非法 %s 应原地展示错误并回填提交值，状态=%d，Location=%q，页面尾部=%s",
					tc.field, rec.Code, rec.Header().Get("Location"), tailOfPage(rec.Body.String(), 900))
			}
			got, err := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
			if err != nil {
				t.Fatalf("读回商品失败: %v", err)
			}
			if got.Name != "原名" || got.Subtitle != "" || got.DefaultPrice == nil || *got.DefaultPrice != price || got.Weight == nil || *got.Weight != weight {
				t.Fatalf("非法 %s 不得产生部分更新，实际：name=%q subtitle=%q price=%v weight=%v", tc.field, got.Name, got.Subtitle, got.DefaultPrice, got.Weight)
			}
		})
	}
}

func TestProductEditPageAttributeListFailureBlocksWrite(t *testing.T) {
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
	handle := producthttp.NewProductPageHandle(failedAttributeListService{ProductService: f.svc}, f.projects)
	engine.GET("/admin/products/edit", handle.ProductEditPage)
	engine.POST("/admin/products/update", handle.ProductsUpdate)

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet,
		"/admin/products/edit?project="+f.projectID+"&product="+created.ID, nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `role="alert"`) ||
		!strings.Contains(page.Body.String(), "系统内部错误") ||
		strings.Contains(page.Body.String(), `id="product-edit-main"`) ||
		strings.Contains(page.Body.String(), `action="/admin/products/variant/save"`) ||
		strings.Contains(page.Body.String(), `action="/admin/products/rating/add"`) ||
		strings.Contains(page.Body.String(), "injected attribute list failure") {
		t.Fatalf("属性取数失败应展示归口错误且不渲染危险表单：状态=%d，页面尾部=%s", page.Code, tailOfPage(page.Body.String(), 900))
	}
	// 即使绕开缺失的选择器直接 POST，服务端仍须拒绝本次整体替换。
	rec := postForm(engine, "/admin/products/update", url.Values{
		"projectId": {f.projectID}, "id": {created.ID}, "name": {"误写名称"},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `role="alert"`) ||
		!strings.Contains(rec.Body.String(), "系统内部错误") ||
		!strings.Contains(rec.Body.String(), `value="误写名称"`) ||
		strings.Contains(rec.Body.String(), `id="product-edit-main"`) ||
		strings.Contains(rec.Body.String(), "injected attribute list failure") {
		t.Fatalf("属性列表失败的 POST 应原地保留提交值并封锁写入，状态=%d，页面尾部=%s", rec.Code, tailOfPage(rec.Body.String(), 900))
	}
	got, err := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("读回商品失败: %v", err)
	}
	if got.Name != "原名" || len(got.AttributeIDs) != 1 || got.AttributeIDs[0] != attr.ID {
		t.Fatalf("属性列表故障不可改变商品和关联：name=%q attrs=%v", got.Name, got.AttributeIDs)
	}
}

// tailOfPage 取页面尾部若干字符（渲染中断时用来看断在哪一段）。
func tailOfPage(page string, n int) string {
	if len(page) <= n {
		return page
	}
	return "…" + page[len(page)-n:]
}
