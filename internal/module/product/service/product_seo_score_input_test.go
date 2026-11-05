package productservice

import (
	"context"
	"errors"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
	"testing"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/seo/scoring"
)

const seoTestProjectID = "proj-1"

type seoReadStub struct {
	t          *testing.T
	get        func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error)
	list       func(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error)
	getCat     func(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error)
	listCats   func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error)
	getBrand   func(context.Context, *productdto.GetBrandReq) (*productdto.BrandResp, error)
	listBrands func(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error)
}

func (s *seoReadStub) unset(method, arg string) {
	s.t.Helper()
	s.t.Fatalf("fake 未设定读口 %s（被调用时参数 %s）：取数走错了分支，或这个读口本不该被调用", method, arg)
}

func (s *seoReadStub) Get(ctx context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
	if s.get == nil {
		s.unset("Get", "id="+req.ID)
		return nil, nil
	}
	return s.get(ctx, req)
}

func (s *seoReadStub) List(ctx context.Context, req *productdto.ListReq) ([]*productdto.ProductResp, error) {
	if s.list == nil {
		s.unset("List", "projectId="+req.ProjectID)
		return nil, nil
	}
	return s.list(ctx, req)
}

func (s *seoReadStub) GetCategory(ctx context.Context, req *productdto.GetCategoryReq) (*productdto.CategoryResp, error) {
	if s.getCat == nil {
		s.unset("GetCategory", "id="+req.ID)
		return nil, nil
	}
	return s.getCat(ctx, req)
}

func (s *seoReadStub) ListCategories(ctx context.Context, req *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
	if s.listCats == nil {
		s.unset("ListCategories", "projectId="+req.ProjectID)
		return nil, nil
	}
	return s.listCats(ctx, req)
}

func (s *seoReadStub) GetBrand(ctx context.Context, req *productdto.GetBrandReq) (*productdto.BrandResp, error) {
	if s.getBrand == nil {
		s.unset("GetBrand", "id="+req.ID)
		return nil, nil
	}
	return s.getBrand(ctx, req)
}

func (s *seoReadStub) ListBrands(ctx context.Context, req *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	if s.listBrands == nil {
		s.unset("ListBrands", "projectId="+req.ProjectID)
		return nil, nil
	}
	return s.listBrands(ctx, req)
}

type seoInstanceStub struct {
	inst  *presentationcontract.InstanceResp
	err   error
	calls []presentationcontract.GetByEntityReq
}

func (s *seoInstanceStub) GetByEntity(_ context.Context, req *presentationcontract.GetByEntityReq) (*presentationcontract.InstanceResp, error) {
	s.calls = append(s.calls, *req)
	return s.inst, s.err
}

func seoTestProduct() *productdto.ProductResp {
	return &productdto.ProductResp{
		ID:          "p1",
		ProjectID:   seoTestProjectID,
		Name:        "净水器",
		Subtitle:    "副标题即 meta 描述",
		Description: json.RawMessage(`{"html":"<p>正文 <b>加粗</b></p>"}`),
		Slug:        "water",
		Images:      []string{"a.jpg", "b.jpg"},
		ImageAlts:   []string{"图一", ""},
		Attributes: []*productdto.AttributeResp{
			{ID: "attr1", Name: "容量", IsVariation: true},
			{ID: "attr2", Name: "材质"},
			{ID: "attr3", Name: "  ", IsVariation: true},
		},
		Categories: []*productdto.CategoryResp{{ID: "c9", Name: "厨房电器"}, {ID: "c8", Name: "  "}},
	}
}

func seoTestCategory() *productdto.CategoryResp {
	return &productdto.CategoryResp{
		ID:          "c1",
		ProjectID:   seoTestProjectID,
		Name:        "厨房",
		Slug:        "kitchen",
		Description: "<p>分类描述</p>",
		Image:       "cat.png",
	}
}

func seoTestBrand() *productdto.BrandResp {
	return &productdto.BrandResp{
		ID:          "b1",
		ProjectID:   seoTestProjectID,
		Name:        "净水品牌",
		Slug:        "brand-x",
		Description: "<i>品牌描述</i>",
		Logo:        "logo.png",
	}
}

func TestSeoScoreInputOfReadFailureIsNotMissing(t *testing.T) {
	boom := errors.New("db down")
	cases := []struct {
		kind scoring.PageKind
		stub *seoReadStub
	}{
		{scoring.KindProduct, &seoReadStub{t: t, get: func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return nil, boom }}},
		{scoring.KindCategory, &seoReadStub{t: t, getCat: func(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error) { return nil, boom }}},
		{scoring.KindBrand, &seoReadStub{t: t, getBrand: func(context.Context, *productdto.GetBrandReq) (*productdto.BrandResp, error) { return nil, boom }}},
	}
	for _, tc := range cases {
		res := SeoScoreInputOf(context.Background(), tc.stub, nil, tc.kind, "e1", seoTestProjectID, "zh-CN")
		if res.Input != nil || !errors.Is(res.Err, boom) || res.SelfID != "" {
			t.Fatalf("%s 的读取失败必须原样传播，实际 %+v", tc.kind, res)
		}
	}
}

func TestSeoScoreInputOfMissingIsAnError(t *testing.T) {
	for _, kind := range []scoring.PageKind{scoring.KindProduct, scoring.KindCategory, scoring.KindBrand} {
		missing := SeoScoreInputOf(context.Background(), &seoReadStub{t: t}, nil, kind, "", seoTestProjectID, "zh-CN")
		if missing.Input != nil || missing.Err == nil {
			t.Fatalf("缺 ID 必须在读库前返回参数错误：%+v", missing)
		}
		absent := SeoScoreInputOf(context.Background(), seoStubReturningNil(kind, t), nil, kind, "e1", seoTestProjectID, "zh-CN")
		status, _ := facing.StatusOf(absent.Err)
		if absent.Input != nil || status != 404 {
			t.Fatalf("不存在必须声明404：%+v status=%d", absent, status)
		}
	}
}

func seoStubReturningNil(kind scoring.PageKind, t *testing.T) *seoReadStub {
	s := &seoReadStub{t: t}
	switch kind {
	case scoring.KindCategory:
		s.getCat = func(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error) { return nil, nil }
	case scoring.KindBrand:
		s.getBrand = func(context.Context, *productdto.GetBrandReq) (*productdto.BrandResp, error) { return nil, nil }
	default:
		s.get = func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return nil, nil }
	}
	return s
}

func TestSeoScoreInputOfFillsProductFacts(t *testing.T) {
	detail := seoTestProduct()
	insts := &seoInstanceStub{inst: &presentationcontract.InstanceResp{URLPath: "/products/water", Publications: []presentationdto.InstancePublicationResp{{ActivePath: "/products/water", ArtifactHash: "active-product"}}}}
	r := &seoReadStub{t: t, get: func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return detail, nil }}

	res := SeoScoreInputOf(context.Background(), r, insts, scoring.KindProduct, detail.ID, seoTestProjectID, "zh-CN")
	if res.Err != nil || res.Input == nil {
		t.Fatalf("正常档应给出 Input 且无 Err，实际 Input=%+v Err=%v", res.Input, res.Err)
	}
	in := res.Input
	if in.Kind != scoring.KindProduct {
		t.Fatalf("Kind 应落成 scoring.KindProduct，实际 %q", in.Kind)
	}
	if res.SelfID != detail.ID {
		t.Fatalf("SelfID 应是实体 id（title 冲突清单要靠它排除自己），实际 %q", res.SelfID)
	}
	if in.Name != detail.Name || in.Subtitle != detail.Subtitle || in.Slug != detail.Slug {
		t.Fatalf("名称/副标题/slug 应原样透传，实际 %q / %q / %q", in.Name, in.Subtitle, in.Slug)
	}
	if in.Description != "正文 加粗" {
		t.Fatalf("描述应去标签并压缩空白（标签算进字数会让 300 字看起来像 900），实际 %q", in.Description)
	}
	if in.SEOTitle != detail.Name || in.SEODescription != detail.Subtitle {
		t.Fatalf("评分标题必须与前台发布口径一致（商品名 / 副标题），实际 %q / %q", in.SEOTitle, in.SEODescription)
	}
	if in.URL != "/products/water" || !in.HasCanonical || !in.HasSchema {
		t.Fatalf("已发布实体的 URL 用实例路径且 canonical/JSON-LD 判真，实际 URL=%q canonical=%v schema=%v",
			in.URL, in.HasCanonical, in.HasSchema)
	}
	if in.Locale != "zh-CN" {
		t.Fatalf("Locale 应为请求语言，实际 %q", in.Locale)
	}
	if len(in.SpecNames) != 1 || in.SpecNames[0] != "容量" {
		t.Fatalf("只有参与变体的属性组才进 SpecNames（空白名不算），实际 %+v", in.SpecNames)
	}
	if len(in.ChildNames) != 1 || in.ChildNames[0] != "厨房电器" {
		t.Fatalf("ChildNames 应是挂载分类名（空白名不算），实际 %+v", in.ChildNames)
	}
	want := []scoring.Image{{Src: "a.jpg", Alt: "图一", Kind: "hero"}, {Src: "b.jpg", Kind: "content"}}
	if len(in.Images) != len(want) || in.Images[0] != want[0] || in.Images[1] != want[1] {
		t.Fatalf("首图 hero、其余 content 且 alt 逐位对应，实际 %+v", in.Images)
	}
	if len(insts.calls) != 1 || insts.calls[0].EntityType != productcontract.EntityTypeProduct || insts.calls[0].EntityID != detail.ID {
		t.Fatalf("商品分支必须按 EntityTypeProduct 问实例，实际 %+v", insts.calls)
	}
}

func TestSeoScoreInputOfFillsCategoryFacts(t *testing.T) {
	cat := seoTestCategory()
	r := &seoReadStub{
		t:      t,
		getCat: func(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error) { return cat, nil },
		listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
			return []*productdto.CategoryResp{
				{ID: cat.ID, Name: cat.Name, Slug: cat.Slug},
				{ID: "c2", ParentID: cat.ID, Name: "子分类一"},
				{ID: "c3", ParentID: cat.ID, Name: "   "},
				{ID: "c4", ParentID: "other", Name: "别人的子级"},
			}, nil
		},
	}

	insts := &seoInstanceStub{}

	res := SeoScoreInputOf(context.Background(), r, insts, scoring.KindCategory, cat.ID, seoTestProjectID, "zh-CN")
	if res.Err != nil || res.Input == nil {
		t.Fatalf("正常档应给出 Input 且无 Err，实际 Input=%+v Err=%v", res.Input, res.Err)
	}
	in := res.Input
	if in.Kind != scoring.KindCategory {
		t.Fatalf("Kind 应落成 scoring.KindCategory，实际 %q", in.Kind)
	}
	if in.Name != cat.Name || in.SEOTitle != cat.Name || in.SEODescription != "分类描述" || in.Description != "分类描述" {
		t.Fatalf("分类名即标题、描述去标签后同时进正文与 meta，实际 %+v", in)
	}
	if in.URL != "/kitchen" || in.HasCanonical || in.HasSchema {
		t.Fatalf("未发布分类回落 slug 近似路径且 canonical/JSON-LD 判假，实际 URL=%q canonical=%v schema=%v",
			in.URL, in.HasCanonical, in.HasSchema)
	}
	if len(in.ChildNames) != 1 || in.ChildNames[0] != "子分类一" {
		t.Fatalf("ChildNames 只取直接子级且丢掉空白名，实际 %+v", in.ChildNames)
	}
	if len(in.Images) != 1 || in.Images[0] != (scoring.Image{Src: "cat.png", Kind: "hero"}) {
		t.Fatalf("分类图按单图 hero 进输入，实际 %+v", in.Images)
	}
	if res.SelfID != cat.ID {
		t.Fatalf("SelfID 应是分类 id，实际 %q", res.SelfID)
	}
	if len(insts.calls) != 1 || insts.calls[0].EntityType != productcontract.EntityTypeCategory {
		t.Fatalf("分类分支必须按 EntityTypeCategory 问实例，实际 %+v", insts.calls)
	}
}

func TestSeoScoreInputOfFillsBrandFacts(t *testing.T) {
	brand := seoTestBrand()
	insts := &seoInstanceStub{inst: &presentationcontract.InstanceResp{URLPath: "/brands/brand-x", Publications: []presentationdto.InstancePublicationResp{{ActivePath: "/brands/brand-x", ArtifactHash: "active-brand"}}}}

	r := &seoReadStub{t: t,
		getBrand: func(context.Context, *productdto.GetBrandReq) (*productdto.BrandResp, error) { return brand, nil }}

	res := SeoScoreInputOf(context.Background(), r, insts, scoring.KindBrand, brand.ID, seoTestProjectID, "zh-CN")
	if res.Err != nil || res.Input == nil {
		t.Fatalf("正常档应给出 Input 且无 Err，实际 Input=%+v Err=%v", res.Input, res.Err)
	}
	in := res.Input
	if in.Kind != scoring.KindBrand {
		t.Fatalf("Kind 应落成 scoring.KindBrand，实际 %q", in.Kind)
	}
	if in.Name != brand.Name || in.Description != "品牌描述" || in.Slug != brand.Slug {
		t.Fatalf("品牌名/描述（去标签）/slug 应透传，实际 %+v", in)
	}
	if in.URL != "/brands/brand-x" || !in.HasCanonical || !in.HasSchema {
		t.Fatalf("已发布品牌用实例路径且 canonical/JSON-LD 判真，实际 URL=%q", in.URL)
	}
	if len(in.ChildNames) != 0 {
		t.Fatalf("品牌没有子级输入，实际 %+v", in.ChildNames)
	}
	if len(in.Images) != 1 || in.Images[0] != (scoring.Image{Src: "logo.png", Kind: "hero"}) {
		t.Fatalf("logo 按单图 hero 进输入，实际 %+v", in.Images)
	}
	if res.SelfID != brand.ID || len(insts.calls) != 1 || insts.calls[0].EntityType != productcontract.EntityTypeBrand {
		t.Fatalf("SelfID 与实例实体类型都要对上品牌，实际 SelfID=%q calls=%+v", res.SelfID, insts.calls)
	}
}

func TestSeoScoreInputOfPassesLangThrough(t *testing.T) {
	detail := seoTestProduct()
	r := &seoReadStub{t: t, get: func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return detail, nil }}
	for _, lang := range []string{"zh-CN", "en", "JA-jp", ""} {
		t.Run("lang="+lang, func(t *testing.T) {
			res := SeoScoreInputOf(context.Background(), r, nil, scoring.KindProduct, detail.ID, seoTestProjectID, lang)
			if res.Input == nil {
				t.Fatalf("正常档应给出 Input，实际 Err=%v", res.Err)
			}
			if res.Input.Locale != lang {
				t.Fatalf("lang 必须原样进 Locale（不 trim、不改大小写），给 %q 得到 %q", lang, res.Input.Locale)
			}
		})
	}
}

func TestSeoScoreInputOfRejectsUnknownKind(t *testing.T) {
	res := SeoScoreInputOf(context.Background(), &seoReadStub{t: t}, nil, scoring.PageKind("landing"), "e1", seoTestProjectID, "zh-CN")
	if res.Input != nil || res.Err == nil {
		t.Fatalf("未知页型必须在读取前拒绝：%+v", res)
	}
}

func TestPageDraftTitleBoundaries(t *testing.T) {

	cases := []struct {
		name string
		doc  json.RawMessage
		want string
	}{
		{"nil 文档", nil, ""},
		{"空文档", json.RawMessage(``), ""},
		{"非法 JSON", json.RawMessage(`{"settings":`), ""},
		{"裸字符串", json.RawMessage(`"标题"`), ""},
		{"缺 seo 段", json.RawMessage(`{"settings":{}}`), ""},
		{"标题类型不符", json.RawMessage(`{"settings":{"seo":{"title":123}}}`), ""},
		{"正常（含首尾空白）", json.RawMessage(`{"settings":{"seo":{"title":"  页面标题  "}}}`), "页面标题"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PageDraftTitle(tc.doc); got != tc.want {
				t.Fatalf("PageDraftTitle(%s) = %q，期望 %q", string(tc.doc), got, tc.want)
			}
		})
	}
}

func TestEntityPlainTextBoundaries(t *testing.T) {

	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"nil", nil, ""},
		{"空", json.RawMessage(``), ""},
		{"非法 JSON", json.RawMessage(`{oops`), ""},
		{"富文本（两段紧邻）", json.RawMessage(`{"html":"<p>第一段</p><p>第二段</p>"}`), "第一段 第二段"},
		{"html 非字符串", json.RawMessage(`{"html":123}`), ""},
		{"纯文本字符串", json.RawMessage(`"<b>粗</b>体"`), "粗 体"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EntityPlainText(tc.raw); got != tc.want {
				t.Fatalf("EntityPlainText(%s) = %q，期望 %q", string(tc.raw), got, tc.want)
			}
		})
	}
}

func TestStripEntityTagsFoldsWhitespace(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"<p>只有一层标签</p>", "只有一层标签"},
		{"<a href=\"#\">链</a> 接", "链 接"},
		{"没有标签", "没有标签"},
	}
	for _, tc := range cases {
		if got := StripEntityTags(tc.in); got != tc.want {
			t.Fatalf("StripEntityTags(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestFlatCategoriesOfShortCircuitsEmptyProject(t *testing.T) {

	r := &seoReadStub{t: t}
	out, err := flatCategoriesOf(context.Background(), r, "")
	if err != nil {
		t.Fatalf("空工程不该报错，实际 %v", err)
	}
	if out == nil {
		t.Fatal("空工程应返回空切片而不是 nil（调用方按切片语义使用它）")
	}
	if len(out) != 0 {
		t.Fatalf("空工程应没有分类，实际 %d 条", len(out))
	}
}

func TestFlatCategoriesOfReturnsTreeInDFSOrder(t *testing.T) {
	r := &seoReadStub{t: t, listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
		return []*productdto.CategoryResp{
			{ID: "a", Name: "根", Children: []*productdto.CategoryResp{{ID: "a1", Name: "子"}, {ID: "a2", Name: "子二"}}},
			{ID: "b", Name: "根二"},
		}, nil
	}}
	out, err := flatCategoriesOf(context.Background(), r, seoTestProjectID)
	if err != nil {
		t.Fatalf("正常档不该报错，实际 %v", err)
	}
	got := ""
	for _, n := range out {
		got += n.ID + ","
	}
	if got != "a,a1,a2,b," {
		t.Fatalf("分类树应摊平成 DFS 前序（父在前、子紧随），实际 %q", got)
	}
}

func TestFlatCategoriesOfPropagatesReadFailure(t *testing.T) {

	boom := errors.New("cats down")
	r := &seoReadStub{t: t, listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
		return nil, boom
	}}
	out, err := flatCategoriesOf(context.Background(), r, seoTestProjectID)
	if !errors.Is(err, boom) {
		t.Fatalf("读失败必须原样带出，实际 %v", err)
	}
	if out != nil {
		t.Fatalf("读失败应返回 nil 而不是空切片，实际 %+v", out)
	}
}

func TestCategoryChildNamesBoundaries(t *testing.T) {
	boom := errors.New("cats down")
	fail := &seoReadStub{t: t, listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
		return nil, boom
	}}
	if got, err := CategoryChildNames(context.Background(), fail, seoTestProjectID, "c1"); got != nil || !errors.Is(err, boom) {
		t.Fatalf("分类树读失败必须传播，实际 got=%+v err=%v", got, err)
	}

	ok := &seoReadStub{t: t, listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
		return []*productdto.CategoryResp{
			{ID: "c2", ParentID: "c1", Name: "子一"},
			{ID: "c3", ParentID: "c1", Name: "   "},
			{ID: "c4", ParentID: "c9", Name: "别人的子级"},
			{ID: "c5", ParentID: "c1", Name: " 子二 "},
		}, nil
	}}
	got, err := CategoryChildNames(context.Background(), ok, seoTestProjectID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "子一" || got[1] != "子二" {
		t.Fatalf("只取直接子级、丢掉空白名并 trim，实际 %+v", got)
	}
}

func TestProductSeoListHelpersTolerateNilEntity(t *testing.T) {

	if got := ProductPageImages(nil); got != nil {
		t.Fatalf("ProductPageImages(nil) 应返回 nil，实际 %+v", got)
	}
	if got := VariationSpecNames(nil); got != nil {
		t.Fatalf("VariationSpecNames(nil) 应返回 nil，实际 %+v", got)
	}
	if got := ProductCategoryNames(nil); got != nil {
		t.Fatalf("ProductCategoryNames(nil) 应返回 nil，实际 %+v", got)
	}
}

func TestProductPageImagesSkipsBlankSources(t *testing.T) {

	p := &productdto.ProductResp{Images: []string{"", "b.jpg"}, ImageAlts: []string{"x", "y"}}
	got := ProductPageImages(p)
	if len(got) != 1 || got[0] != (scoring.Image{Src: "b.jpg", Alt: "y", Kind: "content"}) {
		t.Fatalf("实际行为：首图为空时唯一的图落成 content，实际 %+v", got)
	}

	p2 := &productdto.ProductResp{Images: []string{"a.jpg", " b.jpg "}, ImageAlts: []string{"alt-a"}}
	got2 := ProductPageImages(p2)
	if len(got2) != 2 || got2[0] != (scoring.Image{Src: "a.jpg", Alt: "alt-a", Kind: "hero"}) || got2[1].Alt != "" {
		t.Fatalf("图址要 trim、alt 缺失位补空串，实际 %+v", got2)
	}
}

func TestFirstNonEmptyTextAndSlashSlug(t *testing.T) {
	if got := firstNonEmpty("", "  ", " x "); got != "x" {
		t.Fatalf("取第一个非空值并 trim，实际 %q", got)
	}
	if got := firstNonEmpty("", "  "); got != "" {
		t.Fatalf("全空时返回空串，实际 %q", got)
	}
	if got := slashSlug("  water "); got != "/water" {
		t.Fatalf("slug 要 trim 后加前导斜杠，实际 %q", got)
	}
	if got := slashSlug("   "); got != "" {
		t.Fatalf("空 slug 返回空串（不能产出裸斜杠），实际 %q", got)
	}
}

func TestSeoScoreInputOfProductScopeReachesReadPort(t *testing.T) {
	detail := seoTestProduct()
	var got *productdto.GetReq
	r := &seoReadStub{t: t, get: func(_ context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
		got = req
		return detail, nil
	}}

	res := SeoScoreInputOf(context.Background(), r, nil, scoring.KindProduct, detail.ID, seoTestProjectID, "zh-CN")
	if res.Input == nil {
		t.Fatalf("正常档应给出 Input，实际 Err=%v", res.Err)
	}
	if got == nil {
		t.Fatal("商品读口一次都没被调用")
	}
	if got.ProjectID != seoTestProjectID {
		t.Fatalf("取商品收到的工程 id = %q，want %q —— 漏传时多工程部署下 resolveProjectID 回 ErrInvalidParam，"+
			"评分会错误读取其它工程或拒绝请求", got.ProjectID, seoTestProjectID)
	}
}

func TestSeoScoreInputOfCategoryScopeReachesReadPort(t *testing.T) {
	node := seoTestCategory()
	var got *productdto.GetCategoryReq
	r := &seoReadStub{t: t,
		getCat: func(_ context.Context, req *productdto.GetCategoryReq) (*productdto.CategoryResp, error) {
			got = req
			return node, nil
		},

		listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
			return nil, nil
		},
	}

	res := SeoScoreInputOf(context.Background(), r, nil, scoring.KindCategory, node.ID, seoTestProjectID, "zh-CN")
	if res.Input == nil {
		t.Fatalf("正常档应给出 Input，实际 Err=%v", res.Err)
	}
	if got == nil {
		t.Fatal("分类读口一次都没被调用")
	}
	if got.ProjectID != seoTestProjectID {
		t.Fatalf("取分类收到的工程 id = %q，want %q —— 漏传时多工程部署下 resolveProjectID 回 ErrInvalidParam，"+
			"评分会错误读取其它工程或拒绝请求", got.ProjectID, seoTestProjectID)
	}
}

func TestSeoScoreInputOfBrandScopeReachesReadPort(t *testing.T) {
	brand := seoTestBrand()
	var got *productdto.GetBrandReq
	r := &seoReadStub{t: t, getBrand: func(_ context.Context, req *productdto.GetBrandReq) (*productdto.BrandResp, error) {
		got = req
		return brand, nil
	}}

	res := SeoScoreInputOf(context.Background(), r, nil, scoring.KindBrand, brand.ID, seoTestProjectID, "zh-CN")
	if res.Input == nil {
		t.Fatalf("正常档应给出 Input，实际 Err=%v", res.Err)
	}
	if got == nil {
		t.Fatal("品牌读口一次都没被调用")
	}
	if got.ProjectID != seoTestProjectID {
		t.Fatalf("取品牌收到的工程 id = %q，want %q —— 漏传时多工程部署下 resolveProjectID 回 ErrInvalidParam，"+
			"评分会错误读取其它工程或拒绝请求", got.ProjectID, seoTestProjectID)
	}
}
