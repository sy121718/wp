package producthttp

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

const taxonomyDriverText = `pq: relation "product_attributes" does not exist (SQLSTATE 42P01)`

type taxonomyPage struct {
	name     string
	route    string
	path     string
	basePath string
	success  func() *productPageReaderStub
	empty    func() *productPageReaderStub
	rendered []string
}

var taxonomyPages = []taxonomyPage{
	{
		name:     "标签页",
		route:    "/admin/product-tags",
		path:     "/admin/product-tags?project=p1",
		basePath: "/admin/product-tags",
		success: func() *productPageReaderStub {
			return &productPageReaderStub{tags: []*productdto.TagResp{{ID: "t1", Name: "手工标签", Kind: "manual"}}}
		},
		empty: func() *productPageReaderStub {
			return &productPageReaderStub{}
		},
		rendered: []string{"手工标签"},
	},
	{
		name:     "分类页",
		route:    "/admin/product-categories",
		path:     "/admin/product-categories?project=p1",
		basePath: "/admin/product-categories",
		success: func() *productPageReaderStub {
			return &productPageReaderStub{categories: []*productdto.CategoryResp{{ID: "c1", ProjectID: "p1", Name: "男装"}}}
		},
		empty: func() *productPageReaderStub {
			return &productPageReaderStub{}
		},
		rendered: []string{"男装"},
	},
	{
		name:     "品牌页",
		route:    "/admin/product-brands",
		path:     "/admin/product-brands?project=p1",
		basePath: "/admin/product-brands",
		success: func() *productPageReaderStub {
			return &productPageReaderStub{brands: []*productdto.BrandResp{{ID: "b1", Name: "品牌一"}}}
		},
		empty: func() *productPageReaderStub {
			return &productPageReaderStub{}
		},
		rendered: []string{"品牌一"},
	},
}

func taxonomyHandle(stub *productPageReaderStub) *ProductPageHandle {
	return &ProductPageHandle{
		products: stub,
	}
}

func failTaxonomyView(p taxonomyPage, err error) *productPageReaderStub {
	stub := p.success()
	switch {
	case stub.categories != nil:
		stub.categoryErr = err
	case stub.brands != nil:
		stub.brandErr = err
	default:
		stub.tagErr = err
	}
	return stub
}

func TestTaxonomyPagesLoadFailureRendersSamePage(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			rec := productPageRequestFor(t, taxonomyHandle(failTaxonomyView(p, errors.New(taxonomyDriverText))), p.route, p.path)
			body := assertProductJSONFailure(t, p.name+"主数据没读到", rec, http.StatusInternalServerError, "内部错误")
			for _, part := range []string{"42P01", `relation "`, "does not exist", "pq:"} {
				if strings.Contains(body, part) {
					t.Fatalf("%s：正文里出现了驱动原文 %q，body=%s", p.name, part, headOf(body))
				}
			}
		})
	}
}

func TestTaxonomyPagesUnknownProjectIsBadRequest(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			rec := productPageRequestFor(t, taxonomyHandle(failTaxonomyView(p, productenums.Err(productenums.ErrInvalidParam))), p.route, p.basePath+"?project=does-not-exist")
			assertProductJSONFailure(t, p.name+"未知工程", rec, http.StatusBadRequest, "请求参数无效")
		})
	}
}

func TestTaxonomyPagesSuccessRendersPage(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			rec := productPageRequestFor(t, taxonomyHandle(p.success()), p.route, p.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 成功档应 200，实际 %d，body=%s", p.name, rec.Code, headOf(rec.Body.String()))
			}
			if got := rec.Header().Get("Cache-Control"); got == "no-store" {
				t.Fatalf("%s 成功页不该标 no-store（那是失败页的档）", p.name)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "</html>") {
				t.Fatalf("%s 成功页页壳不完整，body=%s", p.name, headOf(body))
			}
			if strings.HasPrefix(strings.TrimSpace(body), "{") {
				t.Fatalf("%s 成功页输出了 JSON 报文，body=%s", p.name, headOf(body))
			}
			for _, want := range p.rendered {
				if !strings.Contains(body, want) {
					t.Fatalf("%s：数据行 %q 没被装出来（行装配被删掉也能绿？），body=%s", p.name, want, headOf(body))
				}
			}
		})
	}
}

func TestTaxonomyPagesForwardRequestParams(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			stub := p.success()
			productPageRequestFor(t, taxonomyHandle(stub), p.route, p.basePath+"?project=p1&keyword=衬衫&page=3")

			var gotProject, gotKeyword string
			var gotPage int
			if len(stub.queryReqs) != 1 {
				t.Fatalf("查询次数=%d", len(stub.queryReqs))
			}
			gotProject, gotKeyword, gotPage = stub.queryReqs[0].ProjectID, stub.queryReqs[0].Keyword, stub.queryReqs[0].Page
			if gotProject != "p1" || gotKeyword != "衬衫" || gotPage != 3 {
				t.Fatalf("%s：三个参数应原样转发，实际 project=%q keyword=%q page=%d", p.name, gotProject, gotKeyword, gotPage)
			}
		})
	}
}

func TestTaxonomyPagesEmptyResultIsNotFailure(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			rec := productPageRequestFor(t, taxonomyHandle(p.empty()), p.route, p.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 空数据是空态不是失败，应 200，实际 %d，body=%s", p.name, rec.Code, headOf(rec.Body.String()))
			}
			if got := rec.Header().Get("Cache-Control"); got == "no-store" {
				t.Fatalf("%s 空态不该标 no-store", p.name)
			}
			if strings.HasPrefix(strings.TrimSpace(rec.Body.String()), "{") {
				t.Fatalf("%s 空态输出了 JSON 报文，body=%s", p.name, headOf(rec.Body.String()))
			}
		})
	}
}

func TestTaxonomyPagesFailureIsNeverAPage(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			rec := productPageRequestFor(t, taxonomyHandle(failTaxonomyView(p, errors.New(taxonomyDriverText))), p.route, p.path)
			body := assertProductJSONFailure(t, p.name+"失败形态", rec, http.StatusInternalServerError, "内部错误")
			if strings.Contains(body, "empty-state") {
				t.Fatalf("%s：失败报文里出现了空态容器，body=%s", p.name, headOf(body))
			}
		})
	}
}
