package producthttp

// product_taxonomy_page_render_test.go — 分类 / 品牌 / 标签三页整页 GET 的出口判据。
//
// 契约见 docs/rules/template-boundary.md §整页 GET 的失败出口：取数失败的整页 GET **也必须是
// 「这一页」**（同一份模板 + 页壳 + 侧边菜单 + `Cache-Control: no-store`），状态码按档位说实话：
//
//	主数据没读到            → 500
//	?project=<不存在的 id>  → 400（请求参数不合法：判据是**本次请求已加载的工程集合**，
//	                          与页面里的工程选择器同源；工程被删除或并发变化会让旧链接的工程失效，
//	                          那时 400 是准确的事实）
//
// 三页的取数编排（工程清单 → 参数档位 → 计数 → 页码收敛 → 取页，以及「失败之后一条派生取数
// 都不发」）已随取数下沉进 service 的读模型：那几条判据在
// internal/module/product/service/product_taxonomy_page_test.go 与
// public/test/product/feature 的真实库用例里。本文件钉的是**控制器那一半**：
// 拿到失败视图之后做什么（档位 / no-store / 同页形状 / 失败槽与空态不混用 / 不直出内部错误），
// 以及拿到成功视图之后真的把行装出来、把请求参数原样转发。
//
// 这三页此前把每一次取数失败都交给 shell.PageError：浏览器收到 `{"code":500,…}`，
// DOM 里没有标题栏与左侧菜单，用户只能手改地址栏回去 —— 形状合法、静态门禁看不见，
// 只有把真实模板渲染出来才看得见。

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// taxonomyDriverText 驱动原文（驱动前缀 + 表名 + SQLSTATE + 关系名）：
// 它只允许进日志，绝不允许出现在 HTML 里。
const taxonomyDriverText = `pq: relation "product_attributes" does not exist (SQLSTATE 42P01)`

// taxonomyPage 一页的路由、路径与「成功档的读模型桩怎么造」。
type taxonomyPage struct {
	name  string
	route string
	// path 带 ?project=p1；basePath 不带查询串（未知工程那一档要换成别的工程）。
	path     string
	basePath string
	// success 成功档桩（视图键给齐、带一行数据）。
	success func() *productPageReaderStub
	// empty 空态桩（合法工程、没有数据）。
	empty func() *productPageReaderStub
	// rendered 成功档里必须**渲染出来**的片段（数据行的名字）：断言行装配真的发生了 ——
	// 少了它，把装配整段删掉、只留页壳也照样绿。
	rendered []string
}

var taxonomyPages = []taxonomyPage{
	{
		name:     "标签页",
		route:    "/admin/product-tags",
		path:     "/admin/product-tags?project=p1",
		basePath: "/admin/product-tags",
		success:  func() *productPageReaderStub { return &productPageReaderStub{tagsView: productTagsPageViewStub(nil)} },
		empty: func() *productPageReaderStub {
			return &productPageReaderStub{tagsView: &productdto.ProductTagsPageResp{SelectedProject: "p1"}}
		},
		rendered: []string{"手工标签"},
	},
	{
		name:     "分类页",
		route:    "/admin/product-categories",
		path:     "/admin/product-categories?project=p1",
		basePath: "/admin/product-categories",
		success: func() *productPageReaderStub {
			return &productPageReaderStub{categoriesView: productCategoriesPageViewStub(nil)}
		},
		empty: func() *productPageReaderStub {
			return &productPageReaderStub{categoriesView: &productdto.ProductCategoriesPageResp{SelectedProject: "p1"}}
		},
		rendered: []string{"男装"},
	},
	{
		name:     "品牌页",
		route:    "/admin/product-brands",
		path:     "/admin/product-brands?project=p1",
		basePath: "/admin/product-brands",
		success: func() *productPageReaderStub {
			return &productPageReaderStub{brandsView: productBrandsPageViewStub(nil)}
		},
		empty: func() *productPageReaderStub {
			return &productPageReaderStub{brandsView: &productdto.ProductBrandsPageResp{SelectedProject: "p1"}}
		},
		rendered: []string{"品牌一"},
	},
}

// taxonomyHandle 一页的处理器（工程清单桩与属性页共用同一份形状）。
func taxonomyHandle(stub *productPageReaderStub) *ProductPageHandle {
	return &ProductPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: stub, reader: stub,
	}
}

// failTaxonomyView 把一页的读模型桩改成失败档：视图带档位（契约：失败也给齐模板必需键），
// 同时返回 error —— 控制器据此归口失败槽文案（`{{loadError}}` 那一句）。
//
// 400 档要交**业务错误**（`productenums.ErrInvalidParam`）：控制器按「是不是业务 key」分流，
// 交一个驱动原文得到的是内部兜底文案，那样测到的是兜底路径而不是参数档。
func failTaxonomyView(p taxonomyPage, err error) *productPageReaderStub {
	stub := p.success()
	switch {
	case stub.categoriesView != nil:
		stub.categoriesErr = err
	case stub.brandsView != nil:
		stub.brandsErr = err
	default:
		stub.tagsErr = err
	}
	return stub
}

// TestTaxonomyPagesLoadFailureRendersSamePage 失败视图 → 同页完整 HTML + 500 + no-store + 失败槽。
//
// 内部错误（驱动原文）只进日志：正文里连它的片段都不许有。
func TestTaxonomyPagesLoadFailureRendersSamePage(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			rec := productPageRequestFor(t, taxonomyHandle(failTaxonomyView(p, errors.New(taxonomyDriverText))), p.route, p.path)
			body := assertProductJSONFailure(t, p.name+"主数据没读到", rec, http.StatusInternalServerError, "内部错误")
			// 驱动原文（SQLSTATE / 关系名 / 驱动前缀）只进日志。
			//
			// 这里**不能**拿裸表名 `product_attributes` 当判据：改造后 title 交的是 i18n key
			// （admin.product_attributes.title），测试环境没有词条时 Prepare 会原样回填 key，
			// 于是这条断言会对着一个合法 key 报假红。判据改成只在驱动原文里出现的片段。
			for _, part := range []string{"42P01", `relation "`, "does not exist", "pq:"} {
				if strings.Contains(body, part) {
					t.Fatalf("%s：正文里出现了驱动原文 %q，body=%s", p.name, part, headOf(body))
				}
			}
		})
	}
}

// TestTaxonomyPagesUnknownProjectIsBadRequest 400 档（读模型判定工程不在可选集合里）：
// 整页 + 400 + no-store。
//
// 这一档是「参数不合法」，不是「读不到数据」：把它做成 500 会让运营去重试，
// 而正确动作是换回一个存在的工程。
func TestTaxonomyPagesUnknownProjectIsBadRequest(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			rec := productPageRequestFor(t, taxonomyHandle(failTaxonomyView(p, productenums.Err(productenums.ErrInvalidParam))), p.route, p.basePath+"?project=does-not-exist")
			assertProductJSONFailure(t, p.name+"未知工程", rec, http.StatusBadRequest, "请求参数无效")
		})
	}
}

// TestTaxonomyPagesSuccessRendersPage 成功档：200、完整页壳、不出失败槽，且**数据行真的渲出来**。
//
// 反面断言（rendered）：这条用来挡「把行装配整段删掉、只留页壳」也能全绿的假绿 ——
// 行装配是控制器这一半唯一还在做的装配工作。
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

// TestTaxonomyPagesForwardRequestParams 控制器把三个查询参数**原样**交给读模型
// （工程 / 关键词 / 页码），自己不做归一 —— 归一在 service（页码收敛、空工程回退第一个）。
//
// 这条挡的是「控制器顺手把页码改成 1」这类消融：页面看起来完全正常，翻页却永远停在第一页。
func TestTaxonomyPagesForwardRequestParams(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			stub := p.success()
			productPageRequestFor(t, taxonomyHandle(stub), p.route, p.basePath+"?project=p1&keyword=衬衫&page=3")

			var gotProject, gotKeyword string
			var gotPage int
			switch {
			case stub.categoriesReqs != nil:
				gotProject, gotKeyword, gotPage = stub.categoriesReqs[0].ProjectID, stub.categoriesReqs[0].Keyword, stub.categoriesReqs[0].Page
			case stub.brandsReqs != nil:
				gotProject, gotKeyword, gotPage = stub.brandsReqs[0].ProjectID, stub.brandsReqs[0].Keyword, stub.brandsReqs[0].Page
			default:
				gotProject, gotKeyword, gotPage = stub.tagsReqs[0].ProjectID, stub.tagsReqs[0].Keyword, stub.tagsReqs[0].Page
			}
			if gotProject != "p1" || gotKeyword != "衬衫" || gotPage != 3 {
				t.Fatalf("%s：三个参数应原样转发，实际 project=%q keyword=%q page=%d", p.name, gotProject, gotKeyword, gotPage)
			}
		})
	}
}

// TestTaxonomyPagesEmptyResultIsNotFailure 空视图（合法工程、没有数据）：空态，不是失败
// （200、不标 no-store、不出失败槽）。
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

// TestTaxonomyPagesFailureIsNeverAPage 失败档不再是页面：没有空态容器、没有「新建」入口。
//
// 空态说的是「读到了、没有数据」，失败说的是「没读到」——两者混用会让运营以为数据被删了
// （并照着空态里的「新建」按钮再建一份）。
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

// 读模型没接上这一档已从运行期消失：读模型是构造实参（ProductPageHandle.reader），
// 漏接 = 编译错误，判据见 product_reader_injection_test.go —— 运行期不再有
// 「类型断言失败 → 500 兜底」这条路径，所以这里不再有对应用例。

// 桩 taxonomyNoReaderStub（只实现 ProductService、故意不实现读模型）随之删除：
// 「漏接读模型」不再有运行期形态，构造处少一个实参就编译不过。
