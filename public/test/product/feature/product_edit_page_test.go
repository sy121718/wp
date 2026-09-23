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
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// editSaveLocation 提交编辑表单并返回 302 的 Location（PRG 回编辑页）。
func editSaveLocation(t *testing.T, engine *gin.Engine, form url.Values) string {
	t.Helper()
	rec := postForm(engine, "/admin/products/update", form)
	if rec.Code != http.StatusFound {
		t.Fatalf("保存应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Location")
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
		"projectId":      {f.projectID},
		"id":             {created.ID},
		"name":           {"编辑后"},
		"slug":           {"after-edit"},
		"subtitle":       {"副标题"},
		"status":         {productenums.StatusPublished},
		"unit":           {"件"},
		"weight":         {"0.5"},
		"defaultPrice":   {"88.50"},
		"seoTitle":       {"SEO 标题"},
		"seoDescription": {"SEO 描述"},
		"images":         {"https://cdn.example.com/a.jpg\nhttps://cdn.example.com/b.jpg"},
		"imageAlts":      {"图一\n图二"},
	})
	// PRG：回编辑页（而不是回列表）并带成功回执（?done= 已过读侧白名单）。
	if !strings.HasPrefix(loc, "/admin/products/edit?") || !strings.Contains(loc, "product="+created.ID) {
		t.Fatalf("保存成功应 302 回编辑页，实际 %s", loc)
	}
	if !strings.Contains(loc, "done=") {
		t.Fatalf("保存成功应带回执（?done=），实际 %s", loc)
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
	if got.SEOTitle != "SEO 标题" || got.SEODescription != "SEO 描述" {
		t.Fatalf("SEO 两栏未落库：%q / %q", got.SEOTitle, got.SEODescription)
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
	if rec.Code != http.StatusFound {
		t.Fatalf("校验失败应 302 回页面，实际 %d：%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/admin/products/edit?") || !strings.Contains(loc, "err=") {
		t.Fatalf("空名称应回编辑页并带错误原因，实际 %s", loc)
	}
	// 错误文案必须过读侧白名单：裸 key 或内部报错都不算。
	if strings.Contains(loc, productenums.ErrNameRequired) {
		t.Fatalf("错误回显不该是裸 key：%s", loc)
	}
	got, gerr := f.svc.Get(ctx, &productdto.GetReq{ID: created.ID, ProjectID: f.projectID})
	if gerr != nil {
		t.Fatalf("读回商品失败: %v", gerr)
	}
	if got.Name != "原名" {
		t.Fatalf("名称不该被空值覆盖，实际 %q", got.Name)
	}
}

// tailOfPage 取页面尾部若干字符（渲染中断时用来看断在哪一段）。
func tailOfPage(page string, n int) string {
	if len(page) <= n {
		return page
	}
	return "…" + page[len(page)-n:]
}
