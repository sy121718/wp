package productservice

// product_seo_score_input_test.go — C5「SEO 评分逻辑可独立测试」的判据（dd.txt 第九节）。
//
// 被测是 product_seo_score.go 的 SeoScoreInputOf（task-157 从 HTTP 层下沉）。下沉的判据只是
// 「本文件对框架包标识符零命中」，那不等于可独立测试：service 包此前**没有任何 ProductReadPort
// 的 fake**（HTTP 侧测试用的是真 Service + 真库），于是「读取失败」与「数据缺失」的区分一直
// 只能靠读注释。本文件就是给这条契约配一个会变红的判据。
//
// 三条语义（契约原文见 product_seo_score.go 的 SeoScoreInputResult 注释）：
//
//	Input == nil 且 Err != nil —— 读取失败（库 / 契约报错）；
//	Input == nil 且 Err == nil —— 数据缺失（没传 id，或实体不存在）；
//	Input != nil               —— 正常，字段按实体与 lang 原样填入。
//
// 前两者在 HTTP 出口上共用同一个空态视图，调用方只能靠 Err 区分：把「读取失败」分支里的
// res.Err = err 删掉，页面一模一样、渲染测试全绿，只有本文件的用例会红。
//
// 依赖有两个 fake：seoReadStub（ProductReadPort 六方法）与 seoInstanceStub
//（SeoInstancePathReader）。两者都按「未设定的读口被调用即失败」写 —— 取数走错分支
//（分类分支去读商品）用一个万能 stub 是测不出来的：那样它照样返回实体，用例反而绿。

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"testing"

	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/seo/scoring"
)

const seoTestProjectID = "proj-1"

// seoReadStub 是 ProductReadPort 的最小 fake：六个读口各一个函数字段，各自可设三档
// （返回 err / 返回 nil,nil / 返回正常实体）。
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

// seoInstanceStub 是 SeoInstancePathReader 的 fake（可空依赖，故零值即可用）。
//
// calls 记录每次问的实体：entityType 常量写错**不会**报编译错，只会让线上路径永远查不到、
// 评分如实把 canonical / JSON-LD 判成假（“页面明明发布了，编辑期却说没有”）。
type seoInstanceStub struct {
	inst  *presentationcontract.InstanceResp
	err   error
	calls []presentationcontract.GetByEntityReq
}

func (s *seoInstanceStub) GetByEntity(_ context.Context, req *presentationcontract.GetByEntityReq) (*presentationcontract.InstanceResp, error) {
	s.calls = append(s.calls, *req)
	return s.inst, s.err
}

// —— 固定装置：三个实体的「正常」档 ——

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

// ==========================================================================
// 判据 1（C5 的核心）：读取失败不能被当成数据缺失。
// ==========================================================================

func TestSeoScoreInputOfReadFailureIsNotMissing(t *testing.T) {
	// 变红实验：删掉 product_seo_score.go 里「读取失败」分支的 res.Err = err 那一行，
	// 本用例在 “Err == nil” 处失败并打印 Empty 内容（真实输出见交付报告）。
	boom := errors.New("db down")
	cases := []struct {
		name    string
		kind    scoring.PageKind
		wantKey string
		stub    *seoReadStub
	}{
		{"商品读取失败", scoring.KindProduct, productenums.SEOEmptyProductUnreadable, &seoReadStub{t: t,
			get: func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return nil, boom }}},
		{"分类读取失败", scoring.KindCategory, productenums.SEOEmptyCategoryUnreadable, &seoReadStub{t: t,
			getCat: func(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error) { return nil, boom }}},
		{"品牌读取失败", scoring.KindBrand, productenums.SEOEmptyBrandUnreadable, &seoReadStub{t: t,
			getBrand: func(context.Context, *productdto.GetBrandReq) (*productdto.BrandResp, error) { return nil, boom }}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := SeoScoreInputOf(context.Background(), tc.stub, nil, tc.kind, "e1", seoTestProjectID, "zh-CN")
			if res.Input != nil {
				t.Fatalf("读取失败时不该给出 Input，实际 %+v", res.Input)
			}
			if res.Err == nil {
				t.Fatalf("读取失败被吞成了数据缺失：Err == nil（Empty=%+v）—— 这正是 C5 要挡的回归", res.Empty)
			}
			if !errors.Is(res.Err, boom) {
				t.Fatalf("Err 应把读口的错误原样带出，实际 %v", res.Err)
			}
			if res.Empty.Key != tc.wantKey {
				t.Fatalf("空态 key 应为 %q，实际 %q", tc.wantKey, res.Empty.Key)
			}
			if res.Empty.Fallback == "" {
				t.Fatal("空态必须带中文兜底（取词失败时才不至于渲染成空白）")
			}
			if res.SelfID != "" {
				t.Fatalf("读取失败时没有实体 id 可回填，实际 %q", res.SelfID)
			}
		})
	}
}

// ==========================================================================
// 判据 2：数据缺失（缺 id / 实体不存在）必须是 Err == nil 且 Empty.Key 非空。
// ==========================================================================

func TestSeoScoreInputOfMissingIsNotFailure(t *testing.T) {
	kinds := []struct {
		name          string
		kind          scoring.PageKind
		missingKey    string
		unreadableKey string
	}{
		{"商品", scoring.KindProduct, productenums.SEOEmptyMissingProduct, productenums.SEOEmptyProductUnreadable},
		{"分类", scoring.KindCategory, productenums.SEOEmptyMissingCategory, productenums.SEOEmptyCategoryUnreadable},
		{"品牌", scoring.KindBrand, productenums.SEOEmptyMissingBrand, productenums.SEOEmptyBrandUnreadable},
	}
	for _, k := range kinds {
		t.Run("缺 id/"+k.name, func(t *testing.T) {
			// 读口一个都不设：缺 id 必须在取数之前就返回，任何读口被调用都是 bug。
			res := SeoScoreInputOf(context.Background(), &seoReadStub{t: t}, nil, k.kind, "", seoTestProjectID, "zh-CN")
			if res.Input != nil {
				t.Fatalf("数据缺失时 Input 必须是 nil，实际 %+v", res.Input)
			}
			if res.Err != nil {
				t.Fatalf("数据缺失不是读取失败：Err 必须是 nil，实际 %v", res.Err)
			}
			if res.Empty.Key != k.missingKey {
				t.Fatalf("缺 id 的空态 key 应为 %q，实际 %q", k.missingKey, res.Empty.Key)
			}
		})

		t.Run("实体不存在/"+k.name, func(t *testing.T) {
			res := SeoScoreInputOf(context.Background(), seoStubReturningNil(k.kind, t), nil, k.kind, "e1", seoTestProjectID, "zh-CN")
			if res.Input != nil {
				t.Fatalf("实体不存在时 Input 必须是 nil，实际 %+v", res.Input)
			}
			if res.Err != nil {
				t.Fatalf("实体不存在不是读取失败：Err 必须是 nil，实际 %v", res.Err)
			}
			if res.Empty.Key != k.unreadableKey {
				t.Fatalf("实体不存在的空态 key 应为 %q，实际 %q", k.unreadableKey, res.Empty.Key)
			}
		})

		t.Run("两类空态可区分/"+k.name, func(t *testing.T) {
			// 两者共用同一个视图出口，只有 key 能把「还没建」与「库挂了」分开：
			// 合并成一个 key 会让运维去查一个不存在的实体，而真正坏掉的读口没人看。
			if k.missingKey == k.unreadableKey {
				t.Fatalf("「数据缺失」与「读取失败」不能共用 key %q", k.missingKey)
			}
		})
	}
}

// seoStubReturningNil 造「实体不存在」那一档：读口无错、实体为 nil（真 Service 查不到时就是这个形状）。
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

// ==========================================================================
// 判据 3：正常档 —— 三个页型的关键字段（Kind / Name / URL / Locale / SelfID）。
// ==========================================================================

func TestSeoScoreInputOfFillsProductFacts(t *testing.T) {
	detail := seoTestProduct()
	insts := &seoInstanceStub{inst: &presentationcontract.InstanceResp{URLPath: "/products/water"}}
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
	// 未发布档：实例读口零值 → 返回 nil, nil（线上路径未知）。
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
	insts := &seoInstanceStub{inst: &presentationcontract.InstanceResp{URLPath: "/brands/brand-x"}}
	// listCats 刻意不设：品牌分支不该查分类树（一旦被调用，fake 会让用例失败）。
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

// ==========================================================================
// 判据 4：lang 原样进 Input.Locale（评分按语言取词与判定，见 product_seo_score.go:108-109）。
// ==========================================================================

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

// ==========================================================================
// 未知页型：取数与文案都按商品那一档回落（未知枚举取值不该造出一个新口径）。
// ==========================================================================

func TestSeoScoreInputOfUnknownKindFallsBackToProduct(t *testing.T) {
	detail := seoTestProduct()
	r := &seoReadStub{t: t, get: func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return detail, nil }}
	kind := scoring.PageKind("landing")

	res := SeoScoreInputOf(context.Background(), r, nil, kind, detail.ID, seoTestProjectID, "zh-CN")
	if res.Input == nil || res.Err != nil {
		t.Fatalf("未知页型走商品读口，应给出 Input，实际 Input=%+v Err=%v", res.Input, res.Err)
	}
	if res.Input.Kind != scoring.KindProduct {
		t.Fatalf("未知页型必须落成 scoring.KindProduct（不能让 scoring 收到没档案的页型），实际 %q", res.Input.Kind)
	}

	missing := SeoScoreInputOf(context.Background(), &seoReadStub{t: t}, nil, kind, "", seoTestProjectID, "zh-CN")
	if missing.Empty.Key != productenums.SEOEmptyMissingProduct {
		t.Fatalf("未知页型缺 id 时回落到「缺少商品」，实际 %q", missing.Empty.Key)
	}
}

// ==========================================================================
// 其余可单测入口：非法 JSON、空实体、读失败降级 —— 这三个边界此前没有直接判据。
// ==========================================================================

func TestPageDraftTitleBoundaries(t *testing.T) {
	// 页面草稿的 settings.seo.title。非法 / 形状不符一律空串：拿不到标题只该让
	// 「标题唯一性」少一条候选，不该让整个评分面板报错。
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
	// 商品描述 jsonb 的两种形态（富文本对象 / JSON 字符串纯文本）+ 非法入参。
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
	// 工程为空时不查库（读口未设定，被调用即失败），并返回**非 nil 的空切片**。
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
	// 读失败必须是 nil + err：调用方据此返回 nil（少一个小标题），
	// 而不是把空切片当成「这个工程没有分类」。
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
	if got := CategoryChildNames(context.Background(), fail, seoTestProjectID, "c1"); got != nil {
		t.Fatalf("分类树读失败应返回 nil（少个标题，不让评分整体失败），实际 %+v", got)
	}

	ok := &seoReadStub{t: t, listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
		return []*productdto.CategoryResp{
			{ID: "c2", ParentID: "c1", Name: "子一"},
			{ID: "c3", ParentID: "c1", Name: "   "},
			{ID: "c4", ParentID: "c9", Name: "别人的子级"},
			{ID: "c5", ParentID: "c1", Name: " 子二 "},
		}, nil
	}}
	got := CategoryChildNames(context.Background(), ok, seoTestProjectID, "c1")
	if len(got) != 2 || got[0] != "子一" || got[1] != "子二" {
		t.Fatalf("只取直接子级、丢掉空白名并 trim，实际 %+v", got)
	}
}

func TestProductSeoListHelpersTolerateNilEntity(t *testing.T) {
	// 三个取素材函数都可能拿到空实体（评分面板渲染空对象），nil 必须给 nil 而不是 panic。
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
	// 空白图址被丢掉；hero 只由**原始索引 0** 决定 —— 首图为空时整份图集没有 hero 项，
	// 第一张有效图会落成 content。这是当前实现的实际行为（见交付报告的清单外发现）。
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

// ==========================================================================
// 工程作用域：商品分支取商品那一跳必须带上调用方已经收到的 projectID。
//
// 多工程部署下 product 侧的 resolveProjectID 在「显式工程为空且工程数 != 1」时回
// ErrInvalidParam，而 SeoScoreInputOf 把它当成**读取失败**、回「读不到这个商品，无法评分」空态
// —— 商品其实就在调用方那个工程里（handler 已经把 projectId 传进来了）。
//
// 本用例断言的是**读口收到的 GetReq**：只断言「有 Input」照不出漏传 —— 单工程夹具下兜底
// 解析出的那一个恰好就是唯一的那个，传与不传行为等价。
// ==========================================================================

func TestSeoScoreInputOfProductScopeReachesReadPort(t *testing.T) {
	detail := seoTestProduct()
	var got *productdto.GetReq
	r := &seoReadStub{t: t, get: func(_ context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
		got = req
		return detail, nil
	}}

	res := SeoScoreInputOf(context.Background(), r, nil, scoring.KindProduct, detail.ID, seoTestProjectID, "zh-CN")
	if res.Input == nil {
		t.Fatalf("正常档应给出 Input，实际 Err=%v Empty=%+v", res.Err, res.Empty)
	}
	if got == nil {
		t.Fatal("商品读口一次都没被调用")
	}
	if got.ProjectID != seoTestProjectID {
		t.Fatalf("取商品收到的工程 id = %q，want %q —— 漏传时多工程部署下 resolveProjectID 回 ErrInvalidParam，"+
			"评分退化成「读不到这个商品，无法评分」空态", got.ProjectID, seoTestProjectID)
	}
}

// ==========================================================================
// 工程作用域：分类 / 品牌两个分支的读口也必须带上调用方已经收到的 projectID。
//
// 与商品那一条同源、同一档：product 侧 resolveProjectID 在「显式工程为空且工程数 != 1」时回
// ErrInvalidParam，SeoScoreInputOf 把它当成**读取失败**、回「读不到这个分类 / 品牌，无法评分」
// 空态 —— 实体其实就在调用方那个工程里（handler 已经把 projectId 传进来了）。
//
// 断言落在**读口收到的 req** 上：只断言「有 Input」照不出漏传 —— 单工程夹具下兜底解析出的
// 那一个恰好就是唯一的那个，传与不传行为等价。
// ==========================================================================

func TestSeoScoreInputOfCategoryScopeReachesReadPort(t *testing.T) {
	node := seoTestCategory()
	var got *productdto.GetCategoryReq
	r := &seoReadStub{t: t,
		getCat: func(_ context.Context, req *productdto.GetCategoryReq) (*productdto.CategoryResp, error) {
			got = req
			return node, nil
		},
		// 分类分支还会取子分类名（CategoryChildNames → ListCategories），也吃同一个 projectID。
		listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
			return nil, nil
		},
	}

	res := SeoScoreInputOf(context.Background(), r, nil, scoring.KindCategory, node.ID, seoTestProjectID, "zh-CN")
	if res.Input == nil {
		t.Fatalf("正常档应给出 Input，实际 Err=%v Empty=%+v", res.Err, res.Empty)
	}
	if got == nil {
		t.Fatal("分类读口一次都没被调用")
	}
	if got.ProjectID != seoTestProjectID {
		t.Fatalf("取分类收到的工程 id = %q，want %q —— 漏传时多工程部署下 resolveProjectID 回 ErrInvalidParam，"+
			"评分退化成「读不到这个分类，无法评分」空态", got.ProjectID, seoTestProjectID)
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
		t.Fatalf("正常档应给出 Input，实际 Err=%v Empty=%+v", res.Err, res.Empty)
	}
	if got == nil {
		t.Fatal("品牌读口一次都没被调用")
	}
	if got.ProjectID != seoTestProjectID {
		t.Fatalf("取品牌收到的工程 id = %q，want %q —— 漏传时多工程部署下 resolveProjectID 回 ErrInvalidParam，"+
			"评分退化成「读不到这个品牌，无法评分」空态", got.ProjectID, seoTestProjectID)
	}
}
