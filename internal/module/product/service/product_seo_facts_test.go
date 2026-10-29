package productservice

import (
	"context"
	"errors"
	"fmt"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/seo/scoring"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
)

type seoPagesStub struct {
	pagecontract.PageService
	rows []pagedto.PageDraftResp
	err  error
}

func (s seoPagesStub) ListDrafts(context.Context) ([]pagedto.PageDraftResp, error) {
	return s.rows, s.err
}

type seoUnscopedContentStub struct{ contentcontract.ContentService }

func seoIndexStub(t *testing.T) *seoReadStub {
	return &seoReadStub{t: t,
		list: func(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error) { return nil, nil },
		listCats: func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
			return nil, nil
		},
		listBrands: func(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error) { return nil, nil },
	}
}

func TestSeoTitleIndexReadsBeyondFirstHundredAndExcludesOtherProjects(t *testing.T) {
	r := seoIndexStub(t)
	pages := []int{}
	r.list = func(_ context.Context, req *productdto.ListReq) ([]*productdto.ProductResp, error) {
		if req.ProjectID != seoTestProjectID || req.Status != productenums.StatusPublished {
			t.Fatalf("索引查询作用域错误：%+v", req)
		}
		pages = append(pages, req.Page)
		if req.Page == 1 {
			rows := make([]*productdto.ProductResp, req.Size)
			for i := range rows {
				rows[i] = &productdto.ProductResp{ID: fmt.Sprintf("p%d", i), ProjectID: seoTestProjectID, Name: "其它标题"}
			}
			return rows, nil
		}
		return []*productdto.ProductResp{
			{ID: "p101", ProjectID: seoTestProjectID, Name: "目标标题", Slug: "late"},
			{ID: "cross-product", ProjectID: "other", Name: "目标标题", Slug: "private"},
		}, nil
	}
	r.listCats = func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
		return []*productdto.CategoryResp{{ID: "c1", ProjectID: seoTestProjectID, Name: "目标标题"}, {ID: "cross-cat", ProjectID: "other", Name: "目标标题"}}, nil
	}
	r.listBrands = func(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
		return []*productdto.BrandResp{{ID: "b1", ProjectID: seoTestProjectID, Name: "目标标题"}, {ID: "cross-brand", ProjectID: "other", Name: "目标标题"}}, nil
	}
	extra := SeoTitleExtraSources{
		Pages: seoPagesStub{rows: []pagedto.PageDraftResp{
			{ID: "page1", ProjectID: seoTestProjectID, DraftDocument: json.RawMessage(`{"settings":{"seo":{"title":"目标标题"}}}`)},
			{ID: "cross-page", ProjectID: "other", DraftDocument: json.RawMessage(`{"settings":{"seo":{"title":"目标标题"}}}`)},
		}},
		// 内嵌 nil 接口的方法若被调用会 panic，确保无工程过滤的内容接口根本不读取。
		Contents: seoUnscopedContentStub{},
	}
	index, coverage, err := SeoTitleIndex(context.Background(), r, extra, seoTestProjectID)
	if err != nil {
		t.Fatal(err)
	}
	duplicates := scoring.DuplicateTitles("目标标题", index, "self")
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 || len(duplicates) != 4 {
		t.Fatalf("完整分页或工程隔离错误：pages=%v duplicates=%+v", pages, duplicates)
	}
	if len(coverage.Included) != 4 || len(coverage.Omitted) != 1 || coverage.Omitted[0].Source != "article" || coverage.Omitted[0].Reason != "project_filter_unavailable" {
		t.Fatalf("覆盖范围必须真实：%+v", coverage)
	}
}

func TestSeoTitleIndexPropagatesEveryConfiguredSourceFailure(t *testing.T) {
	boom := errors.New("index read failed")
	for _, source := range []string{"product", "category", "brand", "page"} {
		t.Run(source, func(t *testing.T) {
			r, extra := seoIndexStub(t), SeoTitleExtraSources{}
			switch source {
			case "product":
				r.list = func(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error) { return nil, boom }
			case "category":
				r.listCats = func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
					return nil, boom
				}
			case "brand":
				r.listBrands = func(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error) { return nil, boom }
			case "page":
				extra.Pages = seoPagesStub{err: boom}
			}
			_, _, err := SeoTitleIndex(context.Background(), r, extra, seoTestProjectID)
			if !errors.Is(err, boom) {
				t.Fatalf("%s 失败被吞：%v", source, err)
			}
		})
	}
}

func TestSeoScoreInstanceReadUsesProjectAndDistinguishesMissingFromFailure(t *testing.T) {
	detail := seoTestProduct()
	r := &seoReadStub{t: t, get: func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return detail, nil }}
	boom := errors.New("instance db failed")
	for _, tc := range []struct {
		name string
		err  error
		fail bool
	}{
		{"未绑定", facing.WithStatus(errors.New("instance absent"), 404), false},
		{"依赖失败", boom, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := &seoInstanceStub{err: tc.err}
			res := SeoScoreInputOf(context.Background(), r, inst, scoring.KindProduct, detail.ID, seoTestProjectID, "zh-CN")
			if len(inst.calls) != 1 || inst.calls[0].ProjectID != seoTestProjectID {
				t.Fatalf("实例读口未收到工程：%+v", inst.calls)
			}
			if tc.fail {
				if !errors.Is(res.Err, boom) || res.Input != nil {
					t.Fatalf("实例失败不能变成未发布：%+v", res)
				}
			} else if res.Err != nil || res.Input == nil || res.Input.HasCanonical {
				t.Fatalf("未绑定应保留正常未发布事实：%+v", res)
			}
		})
	}
}

func TestSeoScoreRejectsForeignEntityAndPropagatesChildReadFailure(t *testing.T) {
	foreign := seoTestProduct()
	foreign.ProjectID = "other"
	r := &seoReadStub{t: t, get: func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return foreign, nil }}
	res := SeoScoreInputOf(context.Background(), r, nil, scoring.KindProduct, foreign.ID, seoTestProjectID, "zh-CN")
	status, _ := facing.StatusOf(res.Err)
	if status != 404 || res.Input != nil {
		t.Fatalf("不得返回其它工程实体：%+v", res)
	}
	boom := errors.New("children unavailable")
	r.getCat = func(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error) {
		return seoTestCategory(), nil
	}
	r.listCats = func(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
		return nil, boom
	}
	res = SeoScoreInputOf(context.Background(), r, nil, scoring.KindCategory, "c1", seoTestProjectID, "zh-CN")
	if !errors.Is(res.Err, boom) || res.Input != nil {
		t.Fatalf("子分类读取失败不能降低分数后当成功返回：%+v", res)
	}
}

func TestSeoScorePreservesSharedAlgorithmWithoutPresentationFields(t *testing.T) {
	detail := seoTestProduct()
	r := seoIndexStub(t)
	r.get = func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return detail, nil }
	r.list = func(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error) {
		return []*productdto.ProductResp{{ID: "duplicate", ProjectID: seoTestProjectID, Name: detail.Name, Slug: "copy"}}, nil
	}
	req := &productdto.ScorePanelReq{ProjectID: seoTestProjectID, ID: detail.ID}
	out, err := SeoScore(context.Background(), r, nil, SeoTitleExtraSources{}, scoring.KindProduct, req, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	input := SeoScoreInputOf(context.Background(), r, nil, scoring.KindProduct, detail.ID, seoTestProjectID, "zh-CN")
	want := scoring.ScoreEntityPage(input.Input)
	if out.Total != want.Total || len(out.Sections) != len(want.Sections) || len(out.Duplicates) != 1 {
		t.Fatalf("共享评分或重复标题事实漂移：%+v", out)
	}
	for i, section := range want.Sections {
		got := out.Sections[i]
		if got.Key != section.Key || got.Score != section.Score || got.Max != section.Max || got.Weight != section.Weight || got.Percent != section.Percent || len(got.Checks) != len(section.Checks) {
			t.Fatalf("维度事实漂移：%+v vs %+v", got, section)
		}
		for j, check := range section.Checks {
			if got.Checks[j] != (productdto.SeoScoreCheckResp{Key: check.Key, Score: check.Score, Max: check.Max, Skipped: check.Skipped}) {
				t.Fatalf("检查项事实漂移：%+v vs %+v", got.Checks[j], check)
			}
		}
	}
}

var _ SeoInstancePathReader = (*seoInstanceStub)(nil)

// 创建实例会先保存预定路径，发布失败后仍可能保留该路径与旧镜像；只有真实激活记录才算发布。
func TestSeoScoreRequiresPublicationInsteadOfPlannedPathOrStatus(t *testing.T) {
	detail := seoTestProduct()
	r := &seoReadStub{t: t, get: func(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) { return detail, nil }}
	for _, tc := range []struct {
		name         string
		status       string
		artifactID   string
		publications []presentationdto.InstancePublicationResp
		published    bool
	}{
		{name: "只有拟定路径", status: "draft"},
		{name: "仅有活跃状态镜像", status: "active", artifactID: "stale-artifact"},
		{name: "激活记录缺产物哈希", publications: []presentationdto.InstancePublicationResp{{ActivePath: "/products/water"}}},
		{name: "激活记录缺路径", publications: []presentationdto.InstancePublicationResp{{ArtifactHash: "active-hash"}}},
		{name: "空白激活字段", publications: []presentationdto.InstancePublicationResp{{ActivePath: "  ", ArtifactHash: "active-hash"}}},
		{name: "真实激活不依赖状态镜像", status: "draft", publications: []presentationdto.InstancePublicationResp{{Lang: "zh-CN", ActivePath: "/products/water", ArtifactHash: "active-hash"}}, published: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := &seoInstanceStub{inst: &presentationcontract.InstanceResp{
				ProjectID: seoTestProjectID, URLPath: "/products/water", Status: tc.status,
				ArtifactID: tc.artifactID, Publications: tc.publications,
			}}
			res := SeoScoreInputOf(context.Background(), r, inst, scoring.KindProduct, detail.ID, seoTestProjectID, "zh-CN")
			if res.Err != nil || res.Input == nil {
				t.Fatalf("合法的未发布状态不应报错：%+v", res)
			}
			if res.Input.URL != "/products/water" || res.Input.HasCanonical != tc.published || res.Input.HasSchema != tc.published {
				t.Fatalf("预定路径不能替代真实激活事实：input=%+v published=%v", res.Input, tc.published)
			}
		})
	}
}
