package productservice

// product_detail_template_ops.go —— 「详情页模板」的**写动作与预览编排**（issue #14）。
//
// 与同目录 product_detail_template_page.go 的分工（两个文件名像、职责不同，改之前先看这段）：
//   - product_detail_template_page.go = **整页读模型**：GET /admin/products/template 的候选模板
//     清单 / 默认模板 / 绑定实例 → productdto.ProductDetailTemplatePageView（含分页、失败槽）；
//   - 本文件 = **写动作与预览的编排**：模板文档组装、首次发布、切换模板重新发布、改 URL、
//     发布前预览。不返回任何页面视图，也不渲染 HTML —— 渲染与错误映射留在 HTTP 层。
//
// 与 product_seo_score.go 的关系：同包，模板流程若需要评分输入可直接调用那边导出的用例
//（SeoScoreInputOf / SeoEntityScore），不需要 import。**本文件现状不消费评分** ——
// 模板的发布与预览与分数无关；把「调用评分用例」写成事实而不是形式。
//
// 依赖以**窄端口**注入（DetailTemplateInstancePort / ProductReadPort / project 的 URL 规则读口），
// 实参由 inbound/http 传入（它持有装配期注入的那几份契约）。本文件同样**不依赖 HTTP 框架**。

import (
	"context"
	"encoding/json"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/siteurl"
)

// DetailTemplateInstancePort 「详情页模板」写动作与预览所需的自动发布能力（消费者侧最窄接口）。
//
// 只列本文件真正用到的四个方法：预览 / 首次发布 / 切换模板重建 / 改 URL。
// 与 inbound/http 的 ProductDetailTemplatePort **方法逐字相同**（它们是同一个 presentation
// service 的两个静态类型），但不复用那个类型：service 不能 import inbound/http（方向相反）。
type DetailTemplateInstancePort interface {
	PreviewInstance(ctx context.Context, req *presentationcontract.PreviewInstanceReq) (res *presentationcontract.PreviewInstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationcontract.CreateInstanceReq) (res *presentationcontract.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationcontract.RebuildReq) (res *presentationcontract.InstanceResp, err error)
	UpdateURL(ctx context.Context, req *presentationcontract.UpdateURLReq) (res *presentationcontract.InstanceResp, err error)
}

// DetailTemplateDeps 模板编排的依赖（HTTP 侧按 handle 的字段就地组装）。
//
// 四项都可空（HTTP 侧未装配时为空接口）：调用方在**进入用例之前**就判过装配，
// 未装配走的是「依赖未装配」的提示页 —— 这里是行为契约的一部分（拆分前是 h.templates == nil
// 判断），不是在用 nil 兜错。
type DetailTemplateDeps struct {
	Templates contenttemplatecontract.ContentTemplateService // 模板文档组装 / 新建模板
	Instances DetailTemplateInstancePort                     // 发布 / 切换模板 / 改 URL / 预览
	Products  ProductReadPort                                // 发布时取 slug 派生默认路径
	Projects  siteurl.ProjectDetailReader                    // 站点 URL 规则（默认路径的派生依据）
}

// DetailTemplateDocument 取「复制来源」的模板文档：指定模板优先，缺省取该类型默认模板。
func DetailTemplateDocument(ctx context.Context, templates contenttemplatecontract.ContentTemplateService,
	templateID string) (doc json.RawMessage, err error) {
	id := strings.TrimSpace(templateID)
	if id == "" {
		tpl, rerr := templates.ResolveTemplate(ctx, productcontract.EntityTypeProduct)
		if rerr != nil {
			return nil, rerr
		}
		id = tpl.TemplateID
	}
	res, gerr := templates.Get(ctx, &contenttemplatecontract.GetReq{ID: id})
	if gerr != nil {
		return nil, gerr
	}
	return res.DraftDocument, nil
}

// CreateDetailTemplate 新建一套命名模板（复制指定模板或当前默认模板的文档），初始版本 v1。
//
// 顺序与拆分前一致：**先取文档**（DetailTemplateDocument），取不到就不建模板 ——
// 反过来先建空模板再补文档，会在失败时留下一套没有内容的模板。
// 「模板名为空」这类参数级校验留在 HTTP 层：它决定的是回哪个提示页。
func CreateDetailTemplate(ctx context.Context, deps DetailTemplateDeps, projectID, name, copyFrom string) error {
	doc, err := DetailTemplateDocument(ctx, deps.Templates, copyFrom)
	if err != nil {
		return err
	}
	_, err = deps.Templates.Create(ctx, &contenttemplatecontract.CreateReq{
		EntityType: productcontract.EntityTypeProduct, Name: name, DraftDocument: doc, ProjectID: projectID,
	})
	return err
}

// PublishDetailTemplate 首次为该商品建立发布实例并发布（可指定模板），激活静态产物。
//
// URLPath 为空时按站点 URL 规则派生（SiteSettings.urlPatterns，未配置则用 siteurl 的默认模式）：
// 这里只是把表单预填好，用户填了就用用户的 —— 见 internal/siteurl 的三条口径。
//
// DetailTemplateProductLookupError 包装「发布时取商品失败」这一条错误。
//
// 为什么需要它：拆分前 Publish 的两条失败路径走**两张不同的文案表** —— 取商品失败走
// productErrMsg（product 的 enums），建实例失败走 detailTemplateFacingMsg（presentation 的
// enums）。下沉到 service 后调用方只拿到一个 error，若不分流，商品取数失败会被按 presentation
// 的口径翻译成「这个商品还没有详情页实例」—— 文案变了，用户据此会去发布一次（商品其实早已不在）。
//
// Unwrap 让调用方既能 errors.As 识别这一类，也能 errors.Is 到底层原因。
type DetailTemplateProductLookupError struct{ Err error }

func (e *DetailTemplateProductLookupError) Error() string { return e.Err.Error() }

// Unwrap 暴露底层错误（取商品的原始失败原因）。
func (e *DetailTemplateProductLookupError) Unwrap() error { return e.Err }

// 顺序与拆分前一致：**先取商品**（拿 slug），取不到就直接报错，不继续建实例。
func PublishDetailTemplate(ctx context.Context, deps DetailTemplateDeps, projectID, productID, templateID, urlPath string) error {
	req := &presentationcontract.CreateInstanceReq{
		ProjectID: projectID, EntityType: productcontract.EntityTypeProduct, EntityID: productID,
		TemplateID: templateID, URLPath: urlPath,
	}
	if req.URLPath == "" {
		product, err := deps.Products.Get(ctx, &productdto.GetReq{ID: productID})
		if err != nil {
			return &DetailTemplateProductLookupError{Err: err}
		}
		req.URLPath = siteurl.DetailPathForProject(ctx, deps.Projects, projectID,
			siteurl.KindProduct, product.Slug, productID)
	}
	_, err := deps.Instances.CreateInstance(ctx, req)
	return err
}

// UpdateDetailTemplateURL 修改商品详情页的线上路径（改 URL）。
//
// 内容（商品字段、模板绑定、产物内容）完全不动 —— 路径是站点事实而不是内容的一部分：
// 改它的代价是重建产物 + 处置旧链接，不是重新编辑商品。
// 规范化、唯一性检查与冲突行为都在 presentation 侧（本函数只透传 NewPath / WithRedirect），
// 因此这一条的行为与拆分前逐字一致。
func UpdateDetailTemplateURL(ctx context.Context, instances DetailTemplateInstancePort,
	productID, newPath string, withRedirect bool) error {
	_, err := instances.UpdateURL(ctx, &presentationcontract.UpdateURLReq{
		EntityType:   productcontract.EntityTypeProduct,
		EntityID:     productID,
		NewPath:      newPath,
		WithRedirect: withRedirect,
	})
	return err
}

// ApplyDetailTemplate 把商品切换（或确认）到选中的模板并重新发布 —— 产物随模板变化（验收 4）。
func ApplyDetailTemplate(ctx context.Context, instances DetailTemplateInstancePort,
	productID, templateID string) error {
	_, err := instances.Rebuild(ctx, &presentationcontract.RebuildReq{
		EntityID: productID, TemplateID: templateID,
	})
	return err
}

// PreviewDetailTemplate 发布前预览模板渲染效果 —— 只读渲染（不写快照 / 产物 / 指针，不激活 URL）。
//
// 返回实际使用的模板与版本，调用方据此在响应头上标注「预览的是哪一套」
// （HTTP 头的拼装留在 HTTP 层）。
func PreviewDetailTemplate(ctx context.Context, instances DetailTemplateInstancePort,
	projectID, entityID, templateID string) (*presentationcontract.PreviewInstanceResp, error) {
	return instances.PreviewInstance(ctx, &presentationcontract.PreviewInstanceReq{
		ProjectID: projectID, EntityType: productcontract.EntityTypeProduct,
		EntityID: entityID, TemplateID: templateID,
	})
}
