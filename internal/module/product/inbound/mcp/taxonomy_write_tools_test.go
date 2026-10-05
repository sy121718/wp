package productmcp

// taxonomy_write_tools_test.go — 品牌 / 分类 / 标签的边界。
//
// 这一组要钉住三条只靠读代码看不出来的语义：
// ① UpdateCategoryReq.ParentID 是**三态**（不传=不改层级 / 空串=提升为顶级 / id=挂靠），
//    三种写法看起来都像「设父级」，混了就把分类整棵挪走；
// ② 回执要说清「这一步的影响面」—— 建品牌不会自动挂到商品上、删标签会让引用它的
//    自动化规则静默失效（规则不报错，只是永远不再命中）；
// ③ 清单工具必须带 id：写工具的 parentId / brandId / tagId 只能从这里拿。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	productdto "go_wp/internal/module/product/dto"
)

type stubTaxonomyWriter struct {
	brandCreateReq *productdto.CreateBrandReq
	brandCreateRes *productdto.BrandResp
	brandUpdateReq *productdto.UpdateBrandReq
	brandUpdateRes *productdto.BrandResp
	brandDeleteReq *productdto.DeleteBrandReq

	catCreateReq *productdto.CreateCategoryReq
	catCreateRes *productdto.CategoryResp
	catUpdateReq *productdto.UpdateCategoryReq
	catUpdateRes *productdto.CategoryResp
	catDeleteReq *productdto.DeleteCategoryReq

	tagCreateReq *productdto.CreateTagReq
	tagCreateRes *productdto.TagResp
	tagUpdateReq *productdto.UpdateTagReq
	tagUpdateRes *productdto.TagResp
	tagDeleteReq *productdto.DeleteTagReq
}

func (s *stubTaxonomyWriter) CreateBrand(_ context.Context, req *productdto.CreateBrandReq) (*productdto.BrandResp, error) {
	s.brandCreateReq = req
	return s.brandCreateRes, nil
}

func (s *stubTaxonomyWriter) UpdateBrand(_ context.Context, req *productdto.UpdateBrandReq) (*productdto.BrandResp, error) {
	s.brandUpdateReq = req
	return s.brandUpdateRes, nil
}

func (s *stubTaxonomyWriter) DeleteBrand(_ context.Context, req *productdto.DeleteBrandReq) error {
	s.brandDeleteReq = req
	return nil
}

func (s *stubTaxonomyWriter) CreateCategory(_ context.Context, req *productdto.CreateCategoryReq) (*productdto.CategoryResp, error) {
	s.catCreateReq = req
	return s.catCreateRes, nil
}

func (s *stubTaxonomyWriter) UpdateCategory(_ context.Context, req *productdto.UpdateCategoryReq) (*productdto.CategoryResp, error) {
	s.catUpdateReq = req
	return s.catUpdateRes, nil
}

func (s *stubTaxonomyWriter) DeleteCategory(_ context.Context, req *productdto.DeleteCategoryReq) error {
	s.catDeleteReq = req
	return nil
}

func (s *stubTaxonomyWriter) CreateTag(_ context.Context, req *productdto.CreateTagReq) (*productdto.TagResp, error) {
	s.tagCreateReq = req
	return s.tagCreateRes, nil
}

func (s *stubTaxonomyWriter) UpdateTag(_ context.Context, req *productdto.UpdateTagReq) (*productdto.TagResp, error) {
	s.tagUpdateReq = req
	return s.tagUpdateRes, nil
}

func (s *stubTaxonomyWriter) DeleteTag(_ context.Context, req *productdto.DeleteTagReq) error {
	s.tagDeleteReq = req
	return nil
}

type stubTaxonomyReader struct {
	brands     []*productdto.BrandResp
	categories []*productdto.CategoryResp
	tags       []*productdto.TagResp
}

func (s *stubTaxonomyReader) ListBrands(_ context.Context, _ *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	return s.brands, nil
}

func (s *stubTaxonomyReader) ListCategories(_ context.Context, _ *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
	return s.categories, nil
}

func (s *stubTaxonomyReader) ListTags(_ context.Context, _ *productdto.ListTagReq) ([]*productdto.TagResp, error) {
	return s.tags, nil
}

func taxonomyTool(t *testing.T, name string, w *stubTaxonomyWriter) mcp.Tool {
	t.Helper()
	tools, err := TaxonomyWriteTools(w)
	if err != nil {
		t.Fatalf("装配分类维度写工具失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有工具 %q", name)
	return mcp.Tool{}
}

func taxonomyListTool(t *testing.T, name string, r *stubTaxonomyReader) mcp.Tool {
	t.Helper()
	tools, err := TaxonomyTools(r)
	if err != nil {
		t.Fatalf("装配分类维度清单工具失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有清单工具 %q", name)
	return mcp.Tool{}
}

func invokeTaxonomy(t *testing.T, tool mcp.Tool, args map[string]any) (string, error) {
	t.Helper()
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	res, err := tool.Invoke(mcp.WithUserID(context.Background(), 9), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func TestTaxonomyRejectsNilDeps(t *testing.T) {
	if _, err := TaxonomyWriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
	if _, err := TaxonomyTools(nil); err == nil {
		t.Fatal("reader 为 nil 应报错")
	}
}

func TestTaxonomyToolNames(t *testing.T) {
	w, err := TaxonomyWriteTools(&stubTaxonomyWriter{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(w) != 9 {
		t.Fatalf("应有 9 个写工具，实得 %d", len(w))
	}
	r, err := TaxonomyTools(&stubTaxonomyReader{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(r) != 3 {
		t.Fatalf("应有 3 个清单工具，实得 %d", len(r))
	}
}

// 三态 ParentID：不传 / 空串 / 具体 id 必须是三种不同的结果。
func TestCategoryUpdateParentIDIsTriState(t *testing.T) {
	// ① 不传 → nil（service 不改层级）
	stub := &stubTaxonomyWriter{catUpdateRes: &productdto.CategoryResp{ID: "c1", Name: "电子烟"}}
	if _, err := invokeTaxonomy(t, taxonomyTool(t, "category_update", stub), map[string]any{
		"id": "c1", "name": "电子烟",
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.catUpdateReq.ParentID != nil {
		t.Errorf("不传 parentId 时应为 nil（不改层级），实得 %v", *stub.catUpdateReq.ParentID)
	}

	// ② 空串 → 指针指向空串（提升为顶级）
	stub2 := &stubTaxonomyWriter{catUpdateRes: &productdto.CategoryResp{ID: "c1", Name: "电子烟"}}
	if _, err := invokeTaxonomy(t, taxonomyTool(t, "category_update", stub2), map[string]any{
		"id": "c1", "parentId": "",
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub2.catUpdateReq.ParentID == nil {
		t.Error("传空串 parentId 应提升为顶级（指针指向空串），实得 nil —— 那等于不改层级")
	}

	// ③ 具体 id → 挂到那个分类下
	stub3 := &stubTaxonomyWriter{catUpdateRes: &productdto.CategoryResp{ID: "c1", Name: "电子烟", ParentID: "p9"}}
	if _, err := invokeTaxonomy(t, taxonomyTool(t, "category_update", stub3), map[string]any{
		"id": "c1", "parentId": "p9",
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub3.catUpdateReq.ParentID == nil || *stub3.catUpdateReq.ParentID != "p9" {
		t.Errorf("应挂到 p9，实得 %v", stub3.catUpdateReq.ParentID)
	}
}

// 三态这件事必须写在描述里 —— 模型看不到 dto 的注释。
func TestCategoryUpdateDescriptionExplainsTriState(t *testing.T) {
	desc := taxonomyTool(t, "category_update", &stubTaxonomyWriter{}).Description()
	if !strings.Contains(desc, "三态") {
		t.Errorf("描述必须点明 parentId 是三态：\n%s", desc)
	}
	if !strings.Contains(desc, "空串") {
		t.Errorf("描述必须写明「传空串 = 提升为顶级」：\n%s", desc)
	}
}

func TestCategoryCreateTextNamesLevel(t *testing.T) {
	top := &stubTaxonomyWriter{catCreateRes: &productdto.CategoryResp{ID: "c1", Name: "电子烟", Slug: "vape"}}
	text, err := invokeTaxonomy(t, taxonomyTool(t, "category_create", top), map[string]any{"name": "电子烟"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "顶级分类") {
		t.Errorf("顶级分类应说明层级，实得：%s", text)
	}

	child := &stubTaxonomyWriter{catCreateRes: &productdto.CategoryResp{ID: "c2", Name: "一次性", ParentID: "c1"}}
	text2, err := invokeTaxonomy(t, taxonomyTool(t, "category_create", child), map[string]any{"name": "一次性", "parentId": "c1"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text2, "c1") {
		t.Errorf("子分类应报出父分类 id（挂错层级是这步最大的风险），实得：%s", text2)
	}
}

// 建品牌不等于挂到商品上 —— 用户最容易以为这一步就完事了。
func TestBrandCreateTextSaysNeedsProductLink(t *testing.T) {
	stub := &stubTaxonomyWriter{brandCreateRes: &productdto.BrandResp{ID: "b1", Name: "Nike", Slug: "nike"}}
	text, err := invokeTaxonomy(t, taxonomyTool(t, "brand_create", stub), map[string]any{"name": "Nike"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "brandId") {
		t.Errorf("回执必须指明还要把商品的 brandId 指过来，实得：%s", text)
	}
}

func TestTagCreateTextSaysManual(t *testing.T) {
	stub := &stubTaxonomyWriter{tagCreateRes: &productdto.TagResp{ID: "t1", Name: "热销", Kind: "manual"}}
	text, err := invokeTaxonomy(t, taxonomyTool(t, "tag_create", stub), map[string]any{"name": "热销"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "手工标签") {
		t.Errorf("回执应说明这是手工标签，实得：%s", text)
	}
	if !strings.Contains(text, "不会影响任何商品") {
		t.Errorf("回执应说明新建标签不影响商品，实得：%s", text)
	}
	// 只建手工标签：kind 必须留空（落 manual），不能传 rule。
	if stub.tagCreateReq.Kind != "" {
		t.Errorf("本工具只建手工标签，kind 应为空，实得 %q", stub.tagCreateReq.Kind)
	}
}

// 删标签会让引用它的自动化规则**静默失效**（规则不报错，只是永远不再命中）。
func TestTagDeleteTextWarnsAboutSilentRuleBreakage(t *testing.T) {
	stub := &stubTaxonomyWriter{}
	text, err := invokeTaxonomy(t, taxonomyTool(t, "tag_delete", stub), map[string]any{"id": "t1"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "自动化") {
		t.Errorf("删除标签的回执应提醒自动化规则会失效，实得：%s", text)
	}
	if stub.tagDeleteReq == nil || stub.tagDeleteReq.ID != "t1" {
		t.Fatalf("标签 id 没传到: %+v", stub.tagDeleteReq)
	}
}

func TestBrandDeleteTextSaysProductsStay(t *testing.T) {
	stub := &stubTaxonomyWriter{}
	text, err := invokeTaxonomy(t, taxonomyTool(t, "brand_delete", stub), map[string]any{"id": "b1"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "不会消失") {
		t.Errorf("删除品牌的回执应说明商品还在，实得：%s", text)
	}
}

func TestTaxonomyUpdateRejectsEmptyPatch(t *testing.T) {
	for _, name := range []string{"brand_update", "category_update", "tag_update"} {
		stub := &stubTaxonomyWriter{}
		if _, err := invokeTaxonomy(t, taxonomyTool(t, name, stub), map[string]any{"id": "x"}); err == nil {
			t.Errorf("%s 没有任何要改的字段时应报错", name)
		}
	}
}

// 清单工具必须带 id：写工具的 parentId / brandId / tagId 只能从这里拿。
func TestTaxonomyListsCarryIDs(t *testing.T) {
	r := &stubTaxonomyReader{
		brands:     []*productdto.BrandResp{{ID: "b1", Name: "Nike", Slug: "nike"}},
		categories: []*productdto.CategoryResp{{ID: "c1", Name: "电子烟", Depth: 1}, {ID: "c2", Name: "一次性", Depth: 2}},
		tags:       []*productdto.TagResp{{ID: "t1", Name: "热销", Kind: "manual"}, {ID: "t2", Name: "30天未售", Kind: "rule"}},
	}
	for _, c := range []struct{ tool, want string }{
		{"brand_list", "id=b1"},
		{"category_list", "id=c1"},
		{"tag_list", "id=t1"},
	} {
		var tool mcp.Tool
		if c.tool == "brand_list" {
			tool = taxonomyListTool(t, c.tool, r)
		} else if c.tool == "category_list" {
			tool = taxonomyListTool(t, c.tool, r)
		} else {
			tool = taxonomyListTool(t, c.tool, r)
		}
		raw, _ := json.Marshal(map[string]any{})
		res, err := tool.Invoke(context.Background(), raw)
		if err != nil {
			t.Fatalf("%s 调用失败: %v", c.tool, err)
		}
		if !strings.Contains(res.Text, c.want) {
			t.Errorf("%s 的输出必须带 %s，实得：%s", c.tool, c.want, res.Text)
		}
	}
}

// tag_list 要能区分手工标签与规则标签 —— 前者删了就没了，后者会自己长回来。
func TestTagListDistinguishesRuleTags(t *testing.T) {
	r := &stubTaxonomyReader{tags: []*productdto.TagResp{
		{ID: "t1", Name: "热销", Kind: "manual"},
		{ID: "t2", Name: "30天未售", Kind: "rule"},
	}}
	tool := taxonomyListTool(t, "tag_list", r)
	raw, _ := json.Marshal(map[string]any{})
	res, err := tool.Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(res.Text, "规则标签") {
		t.Errorf("清单应标注规则标签，实得：%s", res.Text)
	}
	if !strings.Contains(res.Text, "手工标签") {
		t.Errorf("清单应标注手工标签，实得：%s", res.Text)
	}
}

func TestTaxonomyListsGuideWhenEmpty(t *testing.T) {
	for _, c := range []struct{ tool, want string }{
		{"brand_list", "brand_create"},
		{"category_list", "category_create"},
		{"tag_list", "tag_create"},
	} {
		tool := taxonomyListTool(t, c.tool, &stubTaxonomyReader{})
		raw, _ := json.Marshal(map[string]any{})
		res, err := tool.Invoke(context.Background(), raw)
		if err != nil {
			t.Fatalf("%s 调用失败: %v", c.tool, err)
		}
		if !strings.Contains(res.Text, c.want) {
			t.Errorf("%s 空结果应指向 %s，实得：%s", c.tool, c.want, res.Text)
		}
	}
}
