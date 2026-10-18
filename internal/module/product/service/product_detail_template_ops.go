package productservice

// product_detail_template_ops.go —— 「详情页模板」的**写动作与预览编排**（issue #14）。
//
// 与同目录 product_detail_template_page.go 的分工（两个文件名像、职责不同，改之前先看这段）：
//   - product_detail_template_page.go = **整页读模型**：GET /admin/products/template 的候选模板
//     清单 / 默认模板 / 绑定实例 → productdto.ProductDetailTemplatePageResp（含分页、失败槽）；
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
	"errors"
	json "go_wp/pkg/jsonx"
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

// 两条防御性错误（任务 2）：契约上不该发生，一旦发生就是「实现违约 / 坏数据」——
// 继续往下走要么 panic、要么把空文档写进库。这正是要拦的两种形态。
//
// 不用 enums 常量：这两个 key 不在 handler 的文案白名单里（它按 contenttemplate 的
// 业务 key 映射），于是会落到共享归口文案 —— 用户看到「内部错误」，原文只进日志。
var (
	// errDetailTemplateNilGetResult 模板契约 Get 返回 (nil, nil)：下一行就会解引用 res.DraftDocument。
	errDetailTemplateNilGetResult = errors.New("product: 模板契约 Get 返回空结果（nil, nil），取不到模板文档")
	// errDetailTemplateEmptyDoc 复制来源的文档为空（空串 / 纯空白 / JSON null），拒绝建空模板。
	errDetailTemplateEmptyDoc = errors.New("product: 复制来源的模板文档为空，拒绝建没有内容的模板")
)

// DetailTemplateDocument 取「复制来源」的模板文档：指定模板优先，缺省取该类型默认模板。
//
// projectID 是**必须**的工程作用域（调用方持有：HTTP 表单的 projectId / 编排的上游）。
// 缺省来源走 ResolveTemplateScoped 而不是无工程的 ResolveTemplate —— 后者在 contenttemplate
// 侧只能取「唯一工程」，多工程部署下 resolveProjectID("") 直接回 ErrProjectRequired，
// 于是「新建模板但不指定复制来源」这条最常用的路径整条失败（用户看到的是「新建失败」，
// 而该类型下本来就有可复制的模板）。这一层不猜工程、也不兜底取第一个 —— 猜错会静默
// 复制成**别的工程**的模板文档。
//
// 下面 Get 那一跳**不需要**作用域：contenttemplate 的 Get 逐工程探测、根本不读 req.ProjectID
// （带作用域的入口是 GetScoped）。两者判据不同，不要顺手一起改。
func DetailTemplateDocument(ctx context.Context, templates contenttemplatecontract.ContentTemplateService,
	projectID, templateID string) (doc json.RawMessage, err error) {
	id := strings.TrimSpace(templateID)
	if id == "" {
		tpl, rerr := templates.ResolveTemplateScoped(ctx, projectID, productcontract.EntityTypeProduct)
		if rerr != nil {
			return nil, rerr
		}
		id = tpl.TemplateID
	}
	res, gerr := templates.Get(ctx, &contenttemplatecontract.GetReq{ID: id})
	if gerr != nil {
		return nil, gerr
	}
	// 契约上 Get 成功必给结果，(nil, nil) 属于实现违约。不拦这一条，下一行解引用
	// res.DraftDocument 就是 panic —— 现场只剩一个栈，连「哪一步坏了」都读不出来。
	if res == nil {
		return nil, errDetailTemplateNilGetResult
	}
	return res.DraftDocument, nil
}

// CreateDetailTemplate 新建一套命名模板（复制指定模板或当前默认模板的文档），初始版本 v1。
//
// 顺序与拆分前一致：**先取文档**（DetailTemplateDocument），取不到就不建模板 ——
// 反过来先建空模板再补文档，会在失败时留下一套没有内容的模板。
// 「模板名为空」这类参数级校验留在 HTTP 层：它决定的是回哪个提示页。
func CreateDetailTemplate(ctx context.Context, deps DetailTemplateDeps, projectID, name, copyFrom string) error {
	doc, err := DetailTemplateDocument(ctx, deps.Templates, projectID, copyFrom)
	if err != nil {
		return err
	}
	// 防坏数据，不改变非空路径的行为：HTTP 绑定层的 binding:"required" 只在绑定期生效，
	// service 直调（本包测试、MCP 工具、后台脚本）不经它 —— 于是空文档会一路写进库，
	// 表现为「模板建出来了，编辑器打开是一片空白」，而用户当时看到的是成功提示。
	if detailTemplateDocEmpty(doc) {
		return errDetailTemplateEmptyDoc
	}
	_, err = deps.Templates.Create(ctx, &contenttemplatecontract.CreateReq{
		EntityType: productcontract.EntityTypeProduct, Name: name, DraftDocument: doc, ProjectID: projectID,
	})
	return err
}

// detailTemplateDocEmpty 文档是否为空内容（空串 / 纯空白 / JSON null）。
//
// 判据取「解出来的字节里没有内容」，不解析文档结构：文档语义校验是编辑器与
// contenttemplate 契约的职责，这一层只拦「空文档落库」这一种坏数据。
func detailTemplateDocEmpty(doc json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(doc))
	return trimmed == "" || trimmed == "null"
}

// PublishDetailTemplate 首次为该商品建立发布实例并发布（可指定模板），激活静态产物。
//
// URLPath 为空时按站点 URL 规则派生（SiteSettings.urlPatterns，未配置则用 siteurl 的默认模式）：
// 这里只是把表单预填好，用户填了就用用户的 —— 见 internal/siteurl 的三条口径。
//
// DetailTemplateProductLookupError 包装「发布时取商品失败」这一条错误。
//
// 为什么需要它：两条失败路径的**声明处不同** —— 取商品失败是 product 的 enums（ErrNotFound 指
// 「商品不存在」），建实例失败是 presentation 的 enums（同名 ErrNotFound 指「还没有发布实例」）。
// 下沉到 service 后调用方只拿到一个 error，若不分流，商品取数失败会被按 presentation 的口径
// 翻译成「实例不存在」—— 文案变了，用户据此会去发布一次（商品其实早已不在）。
// 两边的兜底都跟着各自的声明处走（productenums.Err / presentationenums.Err），这里只需要保住
// 「这条错误是谁声明的」。
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
		// 多工程下这一跳必须带作用域：product 侧 Get 经 resolveProjectID 解析工程，
		// 工程不唯一时它拒绝猜一个、直接回 ErrInvalidParam —— 表现是「发布失败」，
		// 而这个商品的实例本该建得出来（同 CreateInstance 的 ProjectID，见上）。
		product, err := deps.Products.Get(ctx, &productdto.GetReq{ID: productID, ProjectID: projectID})
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
// 规范化、唯一性检查与冲突行为都在 presentation 侧（本函数只透传 projectID / NewPath / WithRedirect）。
//
// 工程作用域靠调用方传入，不依赖 presentation 侧的「工程唯一时取该工程」兜底：多工程站点上
// 那一兜底会把实例定位与占用预检落到**另一个工程**，于是改的是别的站点的线上路径，
// 而页面照样回「修改成功」。空串也原样透传 —— 挑工程不是 service 的事。
func UpdateDetailTemplateURL(ctx context.Context, instances DetailTemplateInstancePort,
	projectID, productID, newPath string, withRedirect bool) error {
	_, err := instances.UpdateURL(ctx, &presentationcontract.UpdateURLReq{
		EntityType:   productcontract.EntityTypeProduct,
		EntityID:     productID,
		ProjectID:    projectID,
		NewPath:      newPath,
		WithRedirect: withRedirect,
	})
	return err
}

// ApplyDetailTemplate 把商品切换（或确认）到选中的模板并重新发布 —— 产物随模板变化（验收 4）。
//
// 工程作用域同样靠调用方传入（同 UpdateDetailTemplateURL）：RebuildReq.ProjectID 空着时
// presentation 走「工程唯一时取该工程」的兜底，多工程站点会重建别的工程的实例，
// 用户看到「切换成功」而当前站点的产物没有变。
func ApplyDetailTemplate(ctx context.Context, instances DetailTemplateInstancePort,
	projectID, productID, templateID string) error {
	_, err := instances.Rebuild(ctx, &presentationcontract.RebuildReq{
		EntityID: productID, ProjectID: projectID, TemplateID: templateID,
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
