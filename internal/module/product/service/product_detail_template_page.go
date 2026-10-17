// product_detail_template_page.go — 「详情页模板」整页的读模型（取数编排 + 页级事实）。
//
// 与其余七屏同一形态：一屏一个方法，失败档位由视图带出，控制器只剩
// 「读参数 → 判两个出口 → 调这里 → 渲染」。不重复前几屏的理由。
//
// 这一屏特有的三件事：
//
//  1. **模板清单是主数据**：读不到就是整页装载失败，且后面的派生查询（默认模板 /
//     绑定实例 / 发布路径）一条都不发。派生自清单的结论在清单没到之前没有意义 ——
//     发出去只会在主错误之外再叠一层错误，且让排障日志变成四段而不是一段。
//  2. **绑定实例读不到不算失败**（按「还没有实例」渲染）：这一屏对实例的依赖是**展示**
//     （显示还没发布 / 发布在哪个路径），不是前置条件 —— 它读不出来时页面仍要能让人
//     点「首次发布」。反过来把实例当硬依赖，会让发布链路一抖整页就红。
//  3. **默认模板解析失败也不算失败**（按「没有默认」渲染）：它是标注，不是依赖。
//     与第 2 条同档 —— 凡是「顺带知道一下」的事实，都不参与失败判定；
//     只有主数据（工程清单、商品、模板清单）参与。
package productservice

import (
	"context"
	"fmt"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/siteurl"
)

// ProductDetailTemplatePage 「详情页模板」整页的读模型。
//
// 顺序与原控制器逐字对齐（工程清单 → 工程存在性 → 商品 → 模板清单 → 默认 / 绑定 / 路径），
// 因为「谁先失败」决定用户看到哪一档：工程清单读不到 500，`?project=<不存在的 id>` 400，
// 商品不存在 404，模板清单读不到 500。
func (s *Service) ProductDetailTemplatePage(ctx context.Context, req *productdto.ProductDetailTemplatePageReq) (*productdto.ProductDetailTemplatePageResp, error) {
	view := &productdto.ProductDetailTemplatePageResp{
		Projects:  []projectdto.ProjectResp{},
		Templates: []productdto.DetailTemplateOptionResp{},
	}
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
			// 取数没读到 = 500。404 / 400 是另外两档，不经过这里。
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
	productID := ""
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
		productID = strings.TrimSpace(req.ProductID)
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected

	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		view.HTTPStatus = 400
		firstErr = productenums.Err(productenums.ErrInvalidParam)
	}

	if firstErr == nil && productID == "" {
		// 菜单入口不带 product：控制器据此走引导出口（整页提示回列表），本页不再取数。
		// 不是 400 —— 本页本来就有「还没选商品」的入口形态。
		view.NoProduct = true
		return view, nil
	}

	var product *productdto.ProductResp
	if firstErr == nil {
		// 取商品这一跳必须带工程作用域：product 侧 Get 经 resolveProjectID 解析工程，
		// 多工程下不带 scope 时它拒绝猜一个、直接回 ErrInvalidParam —— 于是被下面的
		// ErrInvalidParam 分支判成 400，整页打不开（而商品本来取得到：它就是上面
		// 已算出的 selected 那个工程里的商品）。
		detail, derr := s.Get(ctx, &productdto.GetReq{ID: productID, ProjectID: selected})
		switch {
		case derr == nil && detail != nil:
			product = detail
		case isProductErrKey(derr, productenums.ErrNotFound):
			view.HTTPStatus = 404
		case isProductErrKey(derr, productenums.ErrInvalidParam):
			view.HTTPStatus = 400
			firstErr = productenums.Err(productenums.ErrInvalidParam)
		default:
			// 其余（含 detail 为 nil 的畸形返回）= 500：「读不出来」与「不存在」必须分开。
			if derr == nil {
				derr = fmt.Errorf("商品 %s 的详情为空", productID)
			}
			fail(derr)
		}
	}

	if firstErr == nil {
		// 三个取数口都带 selected：它们是本端口自带工程作用域的读口，多工程下不带工程
		// 会被下游回 ErrProjectRequired —— 主数据那一条因此变成整页 500。
		candidates, cerr := s.detailTemplates(ctx, selected, productcontract.EntityTypeProduct)
		if cerr != nil {
			// 主数据：读不到就是整页装载失败，后面的派生查询一条都不发（文件头第 1 条）。
			fail(cerr)
		} else {
			// 下面两项都**不参与失败判定**：它们是标注（当前默认是哪套 / 这个商品绑在哪）。
			defaultID := s.defaultDetailTemplate(ctx, selected, productcontract.EntityTypeProduct)
			bound := s.boundDetailTemplate(ctx, selected, productcontract.EntityTypeProduct, productID)
			boundID := bound.TemplateID
			if !bound.Exists {
				// 没有实例 = 还没发布过：显示上按「当前生效的那套」标，而不是留空。
				boundID = defaultID
			}
			options := make([]productdto.DetailTemplateOptionResp, 0, len(candidates))
			for _, c := range candidates {
				options = append(options, productdto.DetailTemplateOptionResp{
					ID:           c.ID,
					Name:         c.Name,
					DraftVersion: c.DraftVersion,
					UpdatedAt:    c.UpdatedAt,
					IsBound:      c.ID == boundID,
					IsDefault:    c.ID == defaultID,
				})
			}
			view.Product = productdto.DetailTemplateProductResp{
				ID:   product.ID,
				Name: product.Name,
				Slug: product.Slug,
				// 表单预填的发布路径：按选中工程的 URL 规则派生（用户可改）。
				URLPath: siteurl.DetailPathForProject(ctx, s.project, selected, siteurl.KindProduct, product.Slug, product.ID),
			}
			view.Templates = options
			view.TemplateCount = len(options)
			view.BoundTemplateID = boundID
			view.DefaultTemplateID = defaultID
			view.InstanceExists = bound.Exists
			view.InstanceStatus = bound.Status
			view.InstanceURL = bound.URLPath
			view.Ready = true
		}
	}

	if firstErr != nil {
		view.LoadFailed = true
		return view, firstErr
	}
	return view, nil
}

// detailTemplates 本工程的模板清单（端口未注入 = 降级成空清单，不是失败）。
//
// 与「未装配」的区分留在控制器：那一档由 handle 的依赖字段判断（见 product_detail_template.go
// 的 ProductDetailTemplatePage），这里只保证端口为空时不 panic。
//
// projectID 由调用方给（它已解析出 selected）：这一跳本身就是「哪个工程的模板」，
// 端口不再有「自己猜一个工程」的余地。
func (s *Service) detailTemplates(ctx context.Context, projectID, entityType string) ([]productcontract.DetailTemplateCandidateView, error) {
	if s.pageDetailTemplates == nil {
		return []productcontract.DetailTemplateCandidateView{}, nil
	}
	rows, err := s.pageDetailTemplates.DetailTemplateCandidates(ctx, projectID, entityType)
	if rows == nil {
		rows = []productcontract.DetailTemplateCandidateView{}
	}
	return rows, err
}

// defaultDetailTemplate 本工程的当前默认模板 id（端口未注入或解析不到 = 空串）。
func (s *Service) defaultDetailTemplate(ctx context.Context, projectID, entityType string) string {
	if s.pageDetailTemplates == nil {
		return ""
	}
	id, err := s.pageDetailTemplates.DefaultDetailTemplate(ctx, projectID, entityType)
	if err != nil {
		return ""
	}
	return id
}

// boundDetailTemplate 该商品当前绑定的发布实例（读不到 = 没有实例，理由见文件头第 2 条）。
func (s *Service) boundDetailTemplate(ctx context.Context, projectID, entityType, entityID string) productcontract.BoundTemplateView {
	if s.pageDetailTemplates == nil {
		return productcontract.BoundTemplateView{}
	}
	return s.pageDetailTemplates.BoundDetailTemplate(ctx, projectID, entityType, entityID)
}

var _ = projectdto.ProjectResp{}
