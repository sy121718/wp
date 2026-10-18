// product_page.go — 商品后台页的读模型（一屏一个方法）。
//
// 为什么在 service 而不是控制器：一屏要取哪些数据、按什么顺序取、哪一条失败算 400 /
// 哪一条只降级、失败之后还要不要再发派生请求 —— 这些都是**业务的结论**，不是 HTTP 的
// 结论。留在控制器里时它们与「怎么渲染」交织在一起，于是每个失败点各写一遍失败页
// （早期形态 6 行 × 18 处），改一处必然漏一处。
//
// 控制器因此只剩三件事：读参数 → 调这里 → 把视图与错误交回（return view, nil）。视图自带
// 档位（pkg/view.Result）与模板键（dto 的 ProductNewPageResp），控制器不再碰 gin.H，
// 也不碰模板：渲染由路由装配处的 adminui.Page / adminui.Bare 决定。
//
// 依赖方向：本文件只认识本模块的 contract（端口）与 dto；跨模块的实现由装配期注入，
// 且端口上**只有读方法**（见 contract/warehouse_options.go 的文件头）。
package productservice

import (
	"context"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// PageAttributeOptionSize 后台页属性组勾选列表的取值上限（新建页读模型与编辑页共用）。
//
// 与属性页的分页不同：勾选列表是**一次性全量**（选了才有意义），所以要一个上限兜住
// 「属性组无限增长」的场景。200 是既有取值（下沉前控制器里就写在这里），不顺手改口径
// —— 改了会让这一屏的候选集与历史截图对不上。
//
// 导出是因为页面处理器还有一条**没走读模型**的老路径（编辑页的勾选列表）：口径必须
// 是同一个数，两处各写一个 200 时，改一处的那天两个页面会静默给出不同的候选集。
const PageAttributeOptionSize = 200

// VariationAttributes 参与变体的属性组（组合生成面板只勾选这些组）。
//
// 导出是为了让页面处理器与读模型共用**同一份判据**：变体生成抽屉（productRow 的
// VariationAttributes 字段）与捆绑页的「自选属性组合」来源面板都只列参与变体的组 ——
// 不参与变体的组构不出组合。两处各写一遍时，哪天加了「按某类属性组兜底组合」的规则，
// 只改一处就会让两个入口给出不同的候选集。
func VariationAttributes(attrs []*productdto.AttributeResp) []*productdto.AttributeResp {
	out := make([]*productdto.AttributeResp, 0, len(attrs))
	for _, a := range attrs {
		if a != nil && a.IsVariation {
			out = append(out, a)
		}
	}
	return out
}

// ProjectExists 工程 id 是否真的在工程清单里。
//
// 用于把「工程不存在」与「这个工程下没有商品」分开：前者是请求参数的问题（400），
// 后者才是空态（200）。导出是为了让控制器与读模型共用**同一份判据** —— 两处各写一遍
// 时，改了一处的那天两个入口对同一个 ?project=xxx 会给出不同档位。
func ProjectExists(projects []projectdto.ProjectResp, id string) bool {
	for i := range projects {
		if projects[i].ID == id {
			return true
		}
	}
	return false
}

// AttributeOptionViews 属性组列表 → 勾选列表视图。
//
// 标签口径（名字 + 标识 key）只此一处：商品详情页与属性页都按 key 指代属性组，
// 只给名字会在同名属性组之间选错。三处各拼一遍时，「带上 key」这条规则改一处就漏两处。
func AttributeOptionViews(list []*productdto.AttributeResp) []productdto.AttributeOptionResp {
	out := make([]productdto.AttributeOptionResp, 0, len(list))
	for _, a := range list {
		if a == nil {
			continue
		}
		label := a.Name
		if key := strings.TrimSpace(a.Key); key != "" {
			label = a.Name + "（" + key + "）"
		}
		out = append(out, productdto.AttributeOptionResp{ID: a.ID, Label: label, IsVariation: a.IsVariation})
	}
	return out
}

// SetWarehouseOptionsPort 注入仓库清单 / 每仓 SKU 候选端口（装配期调用，可空）。
//
// 与其它 Set* 同模式：端口定义在本模块契约、实现在 inventory 模块、由顶层装配注入。
// 未注入时页面照常渲染，只是没有「归属仓」与「从仓库选」两块 —— 与接线前的
// `h.inventories == nil` 同一档降级。
func (s *Service) SetWarehouseOptionsPort(port productcontract.WarehouseOptionsPort) {
	s.pageWarehouses = port
}

// SetEntityTemplatePort 注入详情模板清单 / 默认模板端口（装配期调用，可空）。
//
// 未注入 = 模板能力未装配（TemplatesAvail 为假，页面右侧给降级提示）；
// 注入后读失败 = 「读到了、没有模板」，是另一档提示。
func (s *Service) SetEntityTemplatePort(port productcontract.EntityTemplatePort) {
	s.pageTemplates = port
}

// 读模型是**本类型自己的方法**（没有「注入」这个动作）：端口方法改名 / 改签名时
// 这条断言当场编译错，而不是等到某个页面 404。
var _ productcontract.ProductPageReader = (*Service)(nil)

// ProductNewPage 商品新建整页的读模型（取数编排 + 视图装配）。
//
// 顺序与原控制器逐字对齐（工程的读 → 参数档位 → 仓库 → 属性组 → 每仓候选 → 模板），
// 因为「谁先失败」决定了用户看到哪一句话：工程清单读不到是 500，`?project=<不存在的 id>`
// 是 400，两者都能让后面的取数失败，但只有第一个失败原因会被说出来。
//
// 失败之后**一条派生取数都不发**：在已被判非法的工程上继续查，会把「工程不存在」
// 渲染成「这个工程下没有可选项」—— 参数错误看起来成了空态。所以每步都套
// `firstErr == nil`，而不是把错误收集起来最后一并报。
func (s *Service) ProductNewPage(ctx context.Context, req *productdto.ProductNewPageReq) (res *productdto.ProductNewPageResp, err error) {
	view := &productdto.ProductNewPageResp{
		// Title 放 i18n key 而不是译文：译文按请求语言取，取词在渲染期（adminui.Prepare）。
		Projects:            []projectdto.ProjectResp{},
		WarehouseOptions:    []productdto.WarehouseOptionResp{},
		AttributeOptions:    []productdto.AttributeOptionResp{},
		WarehouseSKUOptions: []productdto.WarehouseSKUGroupResp{},
		Templates:           []productdto.EntityTemplateOptionResp{},
		HTTPStatus:          200,
	}
	// firstErr 只记**第一处**失败：后面的步骤据此跳过，末尾据此给失败槽与档位。
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
			// 取数没读到 = 500。参数不合法是另一档（下面的工程校验）。
			view.HTTPStatus = 500
		}
	}

	projects, perr := s.project.List(ctx)
	if perr != nil {
		fail(perr)
	} else {
		view.Projects = projects
	}

	selected := ""
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected

	// 查询串不是可信边界：`?project=<不存在的 id>` 一路渲染下去会得到「这个工程下没有
	// 可选项」—— 把「工程不存在」说成了「工程是空的」。参数级失败仍是**页面请求**：
	// 整页 + 400 + no-store（档位由视图带出，见 dto.ProductNewPageResp.PageStatus）。
	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		view.HTTPStatus = 400
		firstErr = productenums.Err(productenums.ErrInvalidParam)
	}

	if firstErr == nil && s.pageWarehouses != nil {
		rows, werr := s.pageWarehouses.WarehouseOptions(ctx, selected)
		if werr != nil {
			fail(werr)
		} else {
			view.WarehouseOptions = rows
		}
	}
	if firstErr == nil {
		// 属性组走本模块自己的用例（读的是本模块的表）：不为了「统一形状」再包一层端口 ——
		// 端口是给**别的模块**的能力用的，本模块的读不需要绕一圈。
		list, aerr := s.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: selected, Size: PageAttributeOptionSize})
		if aerr != nil {
			fail(aerr)
		} else {
			view.AttributeOptions = AttributeOptionViews(list)
		}
	}
	if firstErr == nil && s.pageWarehouses != nil {
		groups, gerr := s.pageWarehouses.WarehouseSKUOptions(ctx, selected, view.WarehouseOptions)
		if gerr != nil {
			fail(gerr)
		} else {
			view.WarehouseSKUOptions = groups
		}
	}
	// 模板卡片区是**可选辅助**（docs/rules/template-boundary.md：主数据的失败才让整页失败）。
	// 端口没注入 = 模板能力未装配；注入后读不到 = 这一屏没有候选 —— 都不构成整页失败，
	// 页面右侧据此给两句不同的提示（TemplatesAvail 分开说）。
	if firstErr == nil && s.pageTemplates != nil {
		view.TemplatesAvail = true
		// 两个读口都带**本页选中的工程**（selected）：内容模板表带 FORCE 工程策略，
		// 工程留空时对方只能回落「唯一工程」，多工程部署下回 ErrProjectRequired ——
		// 而这条端口的失败按降级处理（空清单），于是卡片区静默变空、页面照常 200，
		// 「配置读不到」被渲染成「这一屏没有候选」。
		if rows, terr := s.pageTemplates.EntityTemplates(ctx, selected, productcontract.EntityTypeProduct); terr == nil {
			view.Templates = rows
		}
		if id, rerr := s.pageTemplates.DefaultEntityTemplate(ctx, selected, productcontract.EntityTypeProduct); rerr == nil {
			view.DefaultTemplateID = id
		}
	}

	if firstErr != nil {
		// 失败页给齐模板必需键：这里全部字段都有值（空列表而不是缺失），渲染不会断在
		// 「失败分支只设了错误键」上（internal/templates/CLAUDE.md §错误分支也必须给齐必需键）。
		view.LoadFailed = true
		return view, firstErr
	}
	return view, nil
}
