package producthttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go_wp/internal/adminui"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	ctoutbound "go_wp/internal/module/product/outbound/contenttemplate"
	dtoutbound "go_wp/internal/module/product/outbound/detailtemplate"
	projectdto "go_wp/internal/module/project/dto"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
	"go_wp/internal/shell"
	"go_wp/internal/siteurl"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_detail_template.go — 商品详情页模板（issue #14）与 SEO 评分（同一屏上的两块）。
//
// 为什么这两件事同文件：详情页模板决定前台形态，评分给这份形态打分，两者读的是同一份
// 详情页文档与同一批实体（商品 / 分类 / 品牌），改一处常要同时看另一处。
// 评分本体在 internal/seo/scoring，这里只做输入与视图。

var errModeUnavailable = errors.New("详情页双轨能力未装配")

// modeWriteTarget 解析双轨写动作的目标（工程 / 商品 / 发布实例）。
//
// 实例定位一律经 GetByEntity（工程作用域内），失败按 ErrNotFound 回带 —— 与
// 详情页其它写动作的失败口径一致（用户看到的是「商品不存在」，而不是内部错误）。
func (h *productPageHandle) modeWriteTarget(c *gin.Context) (
	projectID, productID string, inst *presentationcontract.InstanceResp, err error) {
	if h.modePort == nil || h.templates == nil || h.instances == nil {
		return "", "", nil, errModeUnavailable
	}
	projectID = strings.TrimSpace(c.PostForm("projectId"))
	productID = strings.TrimSpace(c.PostForm("productId"))
	if projectID == "" || productID == "" {
		return projectID, productID, nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, ierr := h.instances.GetByEntity(c.Request.Context(), &presentationcontract.GetByEntityReq{
		EntityType: productcontract.EntityTypeProduct, EntityID: productID, ProjectID: projectID,
	})
	if ierr != nil || inst == nil {
		return projectID, productID, nil, errors.New(presentationenums.ErrNotFound)
	}
	return projectID, productID, inst, nil
}

// modeRedirect 统一的落点：成功 / 失败都渲染整页提示（回商品编辑页）。
//
// 取代原先的 302 + ?err=：那条通道要求读侧再判一次「这条提示是不是本仓给的」
// （productNoticeTexts），而查询参数不是可信边界（见 product_jump.go 的文件头）。
func modeRedirect(c *gin.Context, projectID, productID string, err error) {
	if err != nil {
		productEditJump(c, false, projectID, productID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneMsg)
}

// ProductsReapplyPreset 重新套用预设：放弃该商品独立文档，回到跟随模板。
func (h *productPageHandle) ProductsReapplyPreset(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	_, err = h.modePort.ReapplyPreset(c.Request.Context(), &presentationcontract.ReapplyPresetReq{
		InstanceID: inst.ID, ProjectID: projectID,
		// 可选：同时换一套模板（换底稿）。留空 = 沿用当前绑定。
		TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	})
	modeRedirect(c, projectID, productID, err)
}

// ProductsRollbackDocument 快照级文档回滚：取历史快照的文档重发。
func (h *productPageHandle) ProductsRollbackDocument(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	snapshotID := strings.TrimSpace(c.PostForm("snapshotId"))
	if snapshotID == "" {
		modeRedirect(c, projectID, productID, errors.New(presentationenums.ErrInvalidParam))
		return
	}
	_, err = h.modePort.RollbackDocument(c.Request.Context(), &presentationcontract.RollbackDocumentReq{
		InstanceID: inst.ID, ProjectID: projectID, SnapshotID: snapshotID,
	})
	modeRedirect(c, projectID, productID, err)
}

// ProductsRollbackArtifact 产物指针回滚（秒级，不重新编译）。
func (h *productPageHandle) ProductsRollbackArtifact(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	targetHash := strings.TrimSpace(c.PostForm("targetHash"))
	if targetHash == "" {
		modeRedirect(c, projectID, productID, errors.New(presentationenums.ErrInvalidParam))
		return
	}
	_, err = h.modePort.RollbackArtifact(c.Request.Context(), &presentationcontract.RollbackArtifactReq{
		InstanceID: inst.ID, ProjectID: projectID, TargetHash: targetHash,
	})
	modeRedirect(c, projectID, productID, err)
}

// 详情页模板与发布实例都以实体类型为键：取值用 productcontract.EntityTypeProduct
//（实体类型注册表里 product 那一档），不在控制器里另留一份 "product" 字面量。

// productDetailTemplatePath 详情页模板页路径（列表行内入口）。
const productDetailTemplatePath = "/admin/products/template"

// productDetailTemplateNoProductPrompt 缺少 product 参数时的引导文案（经整页提示回带）。
//
// 侧栏菜单「内容 › 商品详情模板」的 path 是不带 product 的 productDetailTemplatePath，
// 点进来必然命中缺参分支：原先只做裸 302 回列表，用户看到页面闪回且**毫无提示**，
// 只会以为菜单坏了。引导必须指名**去哪选**（商品列表），否则「回到列表」与「操作失败」
// 在用户眼里是同一件事。
//
// 这条文案由 productDetailTemplateNoProductPromptText 取词，直接渲染进提示页（成品文案）；
// key 在 productenums.ProductDetailTemplateNoProductPrompt，这里只留中文兜底。
const productDetailTemplateNoProductPromptFallback = "请先从商品列表选择一件商品，再配置它的详情页模板。"

// productDetailTemplateNoProductPromptMsg 上面那条引导的**事实**（key + 中文兜底，取词在模板里）。
var productDetailTemplateNoProductPromptMsg = productMsg{
	Key:      productenums.ProductDetailTemplateNoProductPrompt,
	Fallback: productDetailTemplateNoProductPromptFallback,
}

// ProductPagePorts 后台商品相关页面消费的自动发布能力：翻译工作台要「按依赖标记待重建」，
// 详情页模板页要「读绑定 / 预览 / 发布 / 切换模板」。两个窄接口的并集作为装配参数类型，
// 页面各自只持有自己需要的那一个（字段类型仍是对应窄接口）。
type ProductPagePorts interface {
	ProductTranslationInstancePort
	ProductDetailTemplatePort
}

// ProductDetailTemplatePort 「详情页模板」页所需的自动发布能力（消费者侧最窄接口）。
//
// 只列本页真正用到的四个方法：读绑定、预览、首次发布、切换模板重新发布。
type ProductDetailTemplatePort interface {
	GetByEntity(ctx context.Context, req *presentationcontract.GetByEntityReq) (res *presentationcontract.InstanceResp, err error)
	PreviewInstance(ctx context.Context, req *presentationcontract.PreviewInstanceReq) (res *presentationcontract.PreviewInstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationcontract.CreateInstanceReq) (res *presentationcontract.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationcontract.RebuildReq) (res *presentationcontract.InstanceResp, err error)
	// UpdateURL 改 URL（发布后换路径）：新路径激活 + 旧路径 301 / 取消激活。
	UpdateURL(ctx context.Context, req *presentationcontract.UpdateURLReq) (res *presentationcontract.InstanceResp, err error)
}

// ProductDetailTemplateModePort 双轨能力（迁移 282）在 product 侧的**重导出**：
// 接口由 presentation 的 contract 声明（presentationcontract.DetailTemplateModePort，
// 独立成文件的收窄端口，理由与 published_locator.go 相同：让消费方在类型上够不着
// 创建 / 重建 / 删除）。这里只保留别名，既有调用点（handle 字段、下面的 setter、
// 页面测试）一字不改。
//
// 早先的形态是「本模块自定义同形接口 + 装配期运行期类型断言注入」，理由是当时
// presentation 的 contract 正由另一批工作维护；那批工作已落地（contract 声明了该端口，
// presentation service 侧有编译期断言 var _ …DetailTemplateModePort = (*Service)(nil)），
// 因此运行期断言已删除：「presentation 提不提供这个能力」现在由编译器回答，
// 缺失是构建失败，不再存在静默降级的路径。
//
// 跨模块形状不在这里另造一份（两份逐字段等价的定义经不起「一处改、另一处静默分叉」）。
type ProductDetailTemplateModePort = presentationcontract.DetailTemplateModePort

// SetDetailTemplateModePort 注入双轨能力（装配期调用）。
//
// 实参静态类型 presentationcontract.PresentationService 已嵌入该端口，装配处直接传值、
// 无需类型断言。仍然保留 setter：页面测试按两参数构造商品页 handle，
// 不注入时详情页降级为只有基础面板（绑定 / 预览 / 发布），降级可见。
func (h *productPageHandle) SetDetailTemplateModePort(port ProductDetailTemplateModePort) {
	h.modePort = port
}

// SetDetailTemplateDeps 注入「详情页模板」页所需的两份契约（装配期调用）。
//
// 用 setter 而非构造参数：既有装配（含页面测试）按两参数构造商品页 handle，
// 详情模板是增量能力；未注入时页面明确提示，而不是整体不可用。
func (h *productPageHandle) SetDetailTemplateDeps(templates contenttemplatecontract.ContentTemplateService,
	instances ProductDetailTemplatePort) {
	h.templates = templates
	h.instances = instances
	// 模板清单 / 默认模板同时进**页面只读端口**（新建页的右侧卡片区与列表页的模板列）：
	// 未装配（templates == nil）时不给端口 —— 与「模板能力未装配」那一档提示对齐
	//（给一个恒空端口会让页面显示「读到了、只是没有模板」，把配置问题说成空态）。
	if templates == nil {
		h.entityTemplates = nil
	} else {
		port := ctoutbound.New(templates)
		h.entityTemplates = port
		if wiring, ok := h.products.(productPagePortWiring); ok {
			wiring.SetEntityTemplatePort(port)
		}
	}
	// 「详情页模板」整页的主数据端口走同一次注入：它要模板清单的**四个**字段（版本号、
	// 更新时间）与绑定实例，与上面的卡片端口宽度不同、失败语义也不同，所以是另一条
	// （两条合成一条会让卡片区的降级把整页拖成 500）。
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetDetailTemplatePort(dtoutbound.New(templates, instances))
	}
}

// productDetailTemplatePageTemplate 这一屏的模板（整页 GET，成功 / 失败 / 未装配同模板同页壳）。
const productDetailTemplatePageTemplate = "admin/product/product_detail_template.html"

// ProductDetailTemplatePage GET /admin/products/template：某商品的详情页模板选择与预览页。
//
// 取数失败**不在这里出口**：整页 GET 只有末尾那一次渲染（成功 / 失败同模板同页壳，
// 差别只有状态码 / no-store / 失败槽）—— 契约见 docs/rules/template-boundary.md §整页 GET 的失败出口。
//
// 这一屏的两个出口判断留在控制器，因为它们都是**HTTP 出口**而不是取数结论：
//
//	· 依赖未装配 → 200 的就地说明（不是装载失败：区别是「没读到」与「这条能力没接上」）；
//	· 还没选商品 → 整页引导提示（要指名去哪选，见上面 productDetailTemplateNoProductPromptMsg）。
func (h *productPageHandle) ProductDetailTemplatePage(c *gin.Context) {
	// 未装配（缺内容模板 / 自动发布契约）是**装配缺陷的可见提示**，不是装载失败：
	// 与「没读到数据」分开，仍然是 200 的就地说明（先于取数判断 —— 否则两个出口会互相盖住）。
	if h.templates == nil || h.instances == nil {
		unassembled := detailTemplateUnassembledView()
		adminui.RenderPage(c, productDetailTemplatePageTemplate, &unassembled)
		return
	}
	reader, ok := h.products.(productcontract.ProductPageReader)
	if !ok {
		// 装配缺陷：读模型没接上。给 500 而不是渲染成一张空表 —— 否则
		// 「装配没接上」与「这个商品还没有模板」在页面上长得一模一样，而前者要有人去看装配。
		logger.Scene(productPageScene).Error(errDetailTemplateReadModel,
			"商品详情页模板页取数无处可去：读模型未注入")
		missing := detailTemplateLoadFailureView(http.StatusInternalServerError)
		adminui.RenderPage(c, productDetailTemplatePageTemplate, &missing)
		return
	}
	view, err := reader.ProductDetailTemplatePage(c.Request.Context(), &productdto.ProductDetailTemplatePageReq{
		ProjectID: c.Query("project"),
		ProductID: c.Query("product"),
	})
	if view == nil {
		// 契约上视图恒非 nil；契约是约定而不是类型保证，这一层兜底必须存在，
		// 否则一次实现失误会让整页 500 且没有页壳。
		failed := detailTemplateLoadFailureView(http.StatusInternalServerError)
		view = &failed
		err = errors.New("商品详情页模板页的视图为空")
	}
	if view.NoProduct {
		productListJump(c, false, productDetailTemplateNoProductPromptMsg)
		return
	}
	if err != nil {
		view.LoadFailed = true
		// 归口文案（中英 / 缺词 fallback 都在里面）；驱动原文与原始错误只进日志。
		view.LoadError = productErrPageText(c, err)
	}
	adminui.RenderPage(c, productDetailTemplatePageTemplate, view)
}

// errDetailTemplateReadModel 读模型未注入时的日志事由（不出口，只进日志）。
var errDetailTemplateReadModel = errors.New("商品页面读模型未注入（装配缺陷）")

// detailTemplateUnassembledView 模板能力未装配时的视图（200，就地说明）。
//
// Ready 为假 → 模板走「详情页模板能力未装配」那一块；不给 LoadFailed —— 那是
// 「取数没读到」的槽，两者混用会让「没装配」被说成「数据读不出来」，而前者要人去查装配。
func detailTemplateUnassembledView() productdto.ProductDetailTemplatePageView {
	view := detailTemplateLoadFailureView(http.StatusOK)
	view.Title = productenums.ProductDetailTemplateTitle
	view.Menu = "products"
	return view
}

// detailTemplateLoadFailureView 失败档的视图（给齐模板必需键，避免失败分支只设了错误键）。
func detailTemplateLoadFailureView(status int) productdto.ProductDetailTemplatePageView {
	return productdto.ProductDetailTemplatePageView{
		Title:      productenums.ProductDetailTemplateTitle,
		Menu:       "products",
		Projects:   []projectdto.ProjectResp{},
		Templates:  []productdto.DetailTemplateOptionView{},
		LoadFailed: status != http.StatusOK,
		HTTPStatus: status,
	}
}

// productDetailPath 商品详情页的默认 URL（与商品 slug 一致；发布实例按它激活静态产物）。
// ProductDetailTemplateCreate POST /admin/products/template/create：
// 新建一套命名模板（复制指定模板或当前默认模板的文档），初始版本 v1。
//
// 不在本页做模板可视化编辑：模板内容编辑走 /workbench?template=…；本页解决的是
// 「同一个商品类型下有多套命名模板可选、各自版本化」的落地与选择。
func (h *productPageHandle) ProductDetailTemplateCreate(c *gin.Context) {
	if h.templates == nil {
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	name := strings.TrimSpace(c.PostForm("name"))
	copyFrom := strings.TrimSpace(c.PostForm("copyFrom"))
	if name == "" {
		productDetailTemplateJump(c, false, projectID, productID,
			productDetailTemplateNameRequiredMsg)
		return
	}
	doc, err := h.templateDocument(c.Request.Context(), projectID, copyFrom)
	if err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateTemplateMsg(c, err))
		return
	}
	if _, err = h.templates.Create(c.Request.Context(), &contenttemplatecontract.CreateReq{
		EntityType: productcontract.EntityTypeProduct, Name: name, DraftDocument: doc, ProjectID: projectID,
	}); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateTemplateMsg(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneMsg)
}

// templateDocument 取「复制来源」的模板文档：指定模板优先，缺省取该类型默认模板。
func (h *productPageHandle) templateDocument(ctx context.Context, projectID, templateID string) (doc json.RawMessage, err error) {
	id := strings.TrimSpace(templateID)
	if id == "" {
		tpl, rerr := h.templates.ResolveTemplate(ctx, productcontract.EntityTypeProduct)
		if rerr != nil {
			return nil, rerr
		}
		id = tpl.TemplateID
	}
	res, gerr := h.templates.Get(ctx, &contenttemplatecontract.GetReq{ID: id})
	if gerr != nil {
		return nil, gerr
	}
	return res.DraftDocument, nil
}

// ProductDetailTemplatePublish POST /admin/products/template/publish：
// 首次为该商品建立发布实例并发布（可指定模板），激活静态产物。
func (h *productPageHandle) ProductDetailTemplatePublish(c *gin.Context) {
	if h.instances == nil {
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	req := &presentationcontract.CreateInstanceReq{
		ProjectID: projectID, EntityType: productcontract.EntityTypeProduct, EntityID: productID,
		TemplateID: strings.TrimSpace(c.PostForm("templateId")),
		URLPath:    strings.TrimSpace(c.PostForm("urlPath")),
	}
	if req.URLPath == "" {
		product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ID: productID})
		if err != nil {
			productDetailTemplateJump(c, false, projectID, productID, productErrMsg(c, err))
			return
		}
		// 路径按站点 URL 规则派生（SiteSettings.urlPatterns，未配置则用 siteurl 的默认模式）：
		// 这里只是把表单预填好，用户填了就用用户的 —— 见 internal/siteurl 的三条口径。
		req.URLPath = siteurl.DetailPathForProject(c.Request.Context(), h.projects, projectID,
			siteurl.KindProduct, product.Slug, productID)
	}
	if _, err := h.instances.CreateInstance(c.Request.Context(), req); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateFacingMsg(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneMsg)
}

// ProductDetailTemplateUpdateURL POST /admin/products/template/url：
// 修改商品详情页的线上路径（改 URL）。
//
// 内容（商品字段、模板绑定、产物内容）完全不动 —— 路径是站点事实而不是内容
// 的一部分：改它的代价是重建产物 + 处置旧链接，不是重新编辑商品。
func (h *productPageHandle) ProductDetailTemplateUpdateURL(c *gin.Context) {
	if h.instances == nil {
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	newPath := strings.TrimSpace(c.PostForm("newPath"))
	if newPath == "" {
		productDetailTemplateJump(c, false, projectID, productID,
			productDetailTemplatePathRequiredMsg)
		return
	}
	if _, err := h.instances.UpdateURL(c.Request.Context(), &presentationcontract.UpdateURLReq{
		EntityType:   productcontract.EntityTypeProduct,
		EntityID:     productID,
		NewPath:      newPath,
		WithRedirect: c.PostForm("withRedirect") != "",
	}); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateFacingMsg(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneMsg)
}

// ProductDetailTemplateApply POST /admin/products/template/apply：
// 把商品切换（或确认）到选中的模板并重新发布 —— 产物随模板变化（验收 4）。
func (h *productPageHandle) ProductDetailTemplateApply(c *gin.Context) {
	if h.instances == nil {
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if _, err := h.instances.Rebuild(c.Request.Context(), &presentationcontract.RebuildReq{
		EntityID: productID, TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	}); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateFacingMsg(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneMsg)
}

// ProductDetailTemplatePreview POST /admin/products/template/preview：
// 发布前预览模板渲染效果 —— 只读渲染，直接把构建结果的 HTML 输出到新标签页；
// 响应头带上实际使用的模板与版本，便于核对「预览的是哪一套」。
func (h *productPageHandle) ProductDetailTemplatePreview(c *gin.Context) {
	if h.instances == nil {
		// 装配缺陷也走「整页提示回来源页」这一条出口（原先是一行 http.StatusBadRequest
		// 的纯文本：脱壳、不翻译、也没有回去的地方）。
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationcontract.PreviewInstanceReq{
		ProjectID: c.PostForm("projectId"), EntityType: productcontract.EntityTypeProduct,
		EntityID: c.PostForm("productId"), TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	})
	if err != nil {
		// 「预览失败」对作者是可用信息，原因（模板不存在 / 文档非法 / 渲染出错）只进日志。
		adminui.PageErrorBadRequest(c, "product_detail_template", err)
		return
	}
	if res.TemplateName != "" {
		c.Header("X-Preview-Template", fmt.Sprintf("%s@v%d", res.TemplateName, res.TemplateVersion))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

// detailTemplateFacingMessages 详情页模板页可展示的失败文案（键是 presentation 的 enums 常量值）。
//
// presentation 返回的错误是 enums key（ErrPathOccupied 这种），直接回显到页面
// 等于让运营看常量名 —— 而改 URL 最常见的失败恰恰是「路径撞车」，必须说人话。
// 发布 / 切换模板 / 改 URL 三个写动作共用同一张表。
var detailTemplateFacingMessages = map[string]string{
	presentationenums.ErrInvalidParam:         "参数不完整，请检查工程、商品与路径。",
	presentationenums.ErrNotFound:             "这个商品还没有详情页实例，先发布一次。",
	presentationenums.ErrNoTemplate:           "该类型没有可用的内容模板，先建一套商品详情模板。",
	presentationenums.ErrBuildFailed:          "构建失败，请检查模板与商品数据后重试。",
	presentationenums.ErrTemplateTypeMismatch: "这套模板不是商品类型的，换一套再试。",
	presentationenums.ErrInvalidPath:          "访问路径不合法：必须以 / 开头，且不含空格、引号与 .. 路径段。",
	presentationenums.ErrSamePath:             "新路径与当前路径相同，没有需要修改的地方。",
	presentationenums.ErrPathOccupied:         "这个路径已被其他页面或详情页占用，换一个再试。",
}

// detailTemplateTemplateMessages 模板契约（contenttemplate）的业务错误 → 可展示文案。
//
// 这一页的 err 来自三个依赖（product / contenttemplate / presentation），三份白名单
// **分开**查：同名 key 在各自语境下含义不同（contenttemplate 的 ErrNotFound 是「模板不存在」，
// product 的是「商品不存在」），合并成一张表必然吃掉一边。
var detailTemplateTemplateMessages = map[string]string{
	contenttemplateenums.ErrInvalidParam:        "参数不完整，请检查工程、模板名与复制来源。",
	contenttemplateenums.ErrNotFound:            "这套模板不存在，可能已被删除。",
	contenttemplateenums.ErrTemplateInUse:       "这套模板仍被自动发布实例引用，不能删除。",
	contenttemplateenums.ErrInvalidType:         "不支持的内容类型（本页只处理商品详情模板）。",
	contenttemplateenums.ErrDataInvalid:         "模板文档格式非法：请回到工作台重新保存后再复制。",
	contenttemplateenums.ErrFieldBindingInvalid: "模板里的字段绑定越界：请回到工作台改用本模板数据源内的字段。",
	contenttemplateenums.ErrProjectRequired:     "站点里有多个工程，请显式选择这套模板所属的工程。",
	contenttemplateenums.ErrProjectNotFound:     "选择的站点工程不存在，请刷新后重试。",
}

// detailTemplateFacingMsg presentation 的错误 → 一句结论事实。
func detailTemplateFacingMsg(c *gin.Context, err error) productMsg {
	return detailTemplateErrMsg(c, err, detailTemplateFacingMessages)
}

// detailTemplateTemplateMsg 模板契约（contenttemplate）错误 → 一句结论事实。
func detailTemplateTemplateMsg(c *gin.Context, err error) productMsg {
	return detailTemplateErrMsg(c, err, detailTemplateTemplateMessages)
}

// detailTemplateErrMsg 判定复用 productErrFactOf，中文兜底按依赖挑表（表里没有则回落
// productErrFallbacks / 共享归口）。
func detailTemplateErrMsg(c *gin.Context, err error, table map[string]string) productMsg {
	f := productErrFact(c, err)
	if f.Key == "" {
		return productMsg{}
	}
	fb := table[f.Key]
	if fb == "" {
		fb = productErrFallbackOf(f.Key, f.Fallback)
	}
	return productMsg{Key: f.Key, Fallback: fb}
}

// errTemplateDepsMissing 详情模板契约未装配时的提示（装配缺陷，页面可见）。
//
// 中文原文**兼作取词兜底**（与 product_jump.go 的 key + 兜底形态一致）：词条缺失时页面显示
// 这句话，而不是裸 key（页面上出现 `admin.product_detail_template.depsMissing` 这种串，
// 运营只会以为后台坏了）。
const errTemplateDepsMissing = "详情页模板能力未装配（缺少内容模板或自动发布契约）"

// detailTemplateDepsMissingMsg 「依赖未装配」提示的**事实**（key + 中文兜底）。
// key 在 productenums.ProductDetailTemplateDepsMissing（迁移 407 登记中英词条）。
var detailTemplateDepsMissingMsg = productMsg{Key: productenums.ProductDetailTemplateDepsMissing, Fallback: errTemplateDepsMissing}

// detailTemplateDepsMissingRedirect 依赖未装配时的统一出口。
//
// 为什么不是 `c.String(400, errTemplateDepsMissing)`（本处原先的写法）：浏览器里没有页面，
// 只有一行纯文本，且**没走 i18n**（英文站点上照样是中文）。预览又是 `target="_blank"` 打开的
// 新标签页 —— 用户看到一块白底黑字，既不知道缺什么、也没有回去的地方。
//
// 整页提示（取代原先的 303 + ?err=）回详情页：这是 POST 表单提交失败，把人送回来源页
// 改一处再试（提示页不自动跳转，避免刷新重复提交）。
//
// 回跳目标带 project / product，而不是裸 productDetailTemplatePath：后者落到页面 handler 的
// 缺参分支，会**再**提示「请先从商品列表选择一件商品」—— 把装配缺陷说成用户没选商品。
func (h *productPageHandle) detailTemplateDepsMissingRedirect(c *gin.Context) {
	productDetailTemplateJump(c, false, c.PostForm("projectId"), c.PostForm("productId"), detailTemplateDepsMissingMsg)
}

// 本页的两个参数级提示的中文兜底（key 在 productenums.ProductDetailTemplate*Required）。
const (
	productDetailTemplateNameRequiredFallback = "模板名不能为空"
	productDetailTemplatePathRequiredFallback = "请填写新的访问路径。"
)

// 两个参数级提示的**事实**（取词在提示页模板里，控制器不再拼成品串）。
var (
	productDetailTemplateNameRequiredMsg = productMsg{
		Key: productenums.ProductDetailTemplateNameRequired, Fallback: productDetailTemplateNameRequiredFallback,
	}
	productDetailTemplatePathRequiredMsg = productMsg{
		Key: productenums.ProductDetailTemplatePathRequired, Fallback: productDetailTemplatePathRequiredFallback,
	}
)

// detailTemplatePanel 组装详情页模板面板的数据（docs/04-C-instance-override.md §5）。
//
// 三态：未绑定（引导首次发布）/ 已绑定（预览 + 进入自定义）/ 能力未装配（降级提示）。
// 未装配模板能力（templates / instances 未注入）或没给商品 id 时，只回 {"Avail": false}，
// 模板据 isset 给降级提示，页面照常渲染。
func (h *productPageHandle) detailTemplatePanel(ctx context.Context, selected, productID string) gin.H {
	tplPanel := gin.H{"Avail": false}
	if h.templates == nil || h.instances == nil || strings.TrimSpace(productID) == "" {
		return tplPanel
	}
	tplPanel["Avail"] = true
	if inst, ierr := h.instances.GetByEntity(ctx, &presentationcontract.GetByEntityReq{
		EntityType: productcontract.EntityTypeProduct, EntityID: productID, ProjectID: selected,
	}); ierr == nil && inst != nil {
		tplPanel["InstanceID"] = inst.ID
		tplPanel["TemplateID"] = inst.TemplateID
		tplPanel["Published"] = inst.Status == "published" || inst.Status == "active"
		tplPanel["URLPath"] = inst.URLPath
		tplPanel["PreviewQS"] = "template=" + inst.TemplateID + "&entityType=product&entityId=" +
			productID + "&projectId=" + selected
		// 双轨（迁移 282）：模式徽标 + 两个模式的入口分流。
		// document：可进入自定义、可重新套用预设、可按历史快照回滚；
		// template：布局的正确修改位置是模板 —— 按钮写成「编辑模板（影响 N 个商品）」，
		// 影响面用真实计数（含该模板下 template 模式的实例数），不写就让用户凭猜。
		isDoc := inst.RenderMode == presentationcontract.RenderModeDocument
		tplPanel["RenderMode"] = presentationcontract.RenderModeTemplate
		if isDoc {
			tplPanel["RenderMode"] = presentationcontract.RenderModeDocument
		}
		tplPanel["IsDocumentMode"] = isDoc
		// 「预设有新版本」：document 模式不会自动跟随模板，只能靠快照记录的
		// 模板版本与模板最新版比对来提示（判定依据由 toResp 给出）。
		if isDoc {
			if tpl, rerr := h.templates.ResolveTemplate(ctx, productcontract.EntityTypeProduct); rerr == nil && tpl != nil &&
				inst.SourceTemplateVersionID != "" && tpl.VersionID != inst.SourceTemplateVersionID {
				tplPanel["PresetUpdated"] = true
			}
			if h.modePort != nil {
				if snaps, serr := h.modePort.ListSnapshots(ctx, &presentationcontract.ListSnapshotsReq{
					InstanceID: inst.ID, ProjectID: selected, Limit: 8,
				}); serr == nil {
					tplPanel["Snapshots"] = snaps
				}
			}
		}
		if h.modePort != nil {
			if counts, cerr := h.modePort.CountByTemplate(ctx, &presentationcontract.CountByTemplateReq{
				TemplateID: inst.TemplateID, ProjectID: selected,
			}); cerr == nil && counts != nil {
				tplPanel["AffectedCount"] = counts.TemplateMode
			}
		}
	}
	if rows, terr := h.templates.List(ctx, &contenttemplatecontract.ListReq{EntityType: productcontract.EntityTypeProduct}); terr == nil {
		tplPanel["Templates"] = rows
	}
	return tplPanel
}

// ==========================================================================
// SEO 评分：行内面板、抽屉与三个实体的取数（商品 / 分类 / 品牌）。
//
// 三个 POST 端点共用 entityScoreInputOf 一份取数口径 —— 抽屉与行内按钮各写一份的下场是
// 「同一个商品在两处分数不同」。评分本体在 internal/seo/scoring，这里只做输入与视图。

// 评分等级文案（颜色 → 当前语言）的**唯一映射**在 seoscore.ScoreGradeText
//（internal/seo/score_grade.go），调用点在 product_seo_score_page.go 的
// entityScoreView。本文件此前自带一份 key + 中文兜底表（与本模块 enums 里的
// admin.seo.grade.* 常量配套），已按「同一批展示文案只能有一份定义」收编 ——
// 收编后 product 取到的是 content / project 用的同一批词条 admin.seo.score.grade.*。

// seo_entity_score_input.go - 实体评分的输入素材提取（标题、正文纯文本、图片与分类名）。
// ==========================================================================
const (
	MsgProductsTitle = "MsgProductsTitle"
)

// requestScoreLang 评分使用的语言（后台 Cookie / Accept-Language，SEO-001）。
func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// seoTitleIndexSize 标题索引每类实体的取样上限。
//
// 「编辑期轻量版」的重点是点一下就有结果（一次列表查询，不是遍历产物文件）。
// 超过上限时商品一侧由 service 按自己的分页上限收敛 —— 这点在注释里写明，
// 因为「索引不完整」会让冲突清单漏报，而漏报是检查类功能最不能有的失效模式。
const seoTitleIndexSize = 100

// SetSeoTitleSources 注入全站标题索引所需的另外两份只读契约（装配期调用，可空）。
//
// 页面与文章是同一个站点的其它页面，做 title 唯一性时必须在同一份索引里 ——
// 只比商品域的话，商品标题与文章标题撞车照样检不出来。两份契约都可空：
// 未注入时索引退化成商品 / 分类 / 品牌三域，评分本身照常（缺依赖不该变成 500）。
func (h *productPageHandle) SetSeoTitleSources(pages pagecontract.PageService, contents contentcontract.ContentService) {
	h.seoPages = pages
	h.seoContents = contents
}

// ProductScorePanel 商品详情页的编辑期评分（POST /admin/products/seo-score）。
func (h *productPageHandle) ProductScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoring.KindProduct,
		strings.TrimSpace(c.PostForm("productId")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 productdto.ScoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// ProductCategoryScorePanel 商品分类页的编辑期评分（POST /admin/product-categories/seo-score）。
func (h *productPageHandle) ProductCategoryScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoring.KindCategory,
		strings.TrimSpace(c.PostForm("id")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 productdto.ScoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// ProductBrandScorePanel 品牌页的编辑期评分（POST /admin/product-brands/seo-score）。
func (h *productPageHandle) ProductBrandScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoring.KindBrand,
		strings.TrimSpace(c.PostForm("id")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 productdto.ScoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// entityScoreViewOf 算分 + 查 title 重复 → 片段视图（三个 POST 端点与抽屉共用同一段流程）。
func (h *productPageHandle) entityScoreViewOf(ctx context.Context, c *gin.Context,
	in *scoring.EntityPageInput, projectID, selfID string) productdto.ScoreView {
	res := scoring.ScoreEntityPage(in)
	tr := shell.TranslateFor(c)
	dups := scoring.DuplicateTitles(scoring.EntityTitle(in), h.seoTitleIndex(ctx, projectID, tr), selfID)
	return entityScoreView(tr, in, res, dups)
}

// renderEntityScore 算分 + 查 title 重复 + 渲染片段（三个 POST 入口共用）。
func (h *productPageHandle) renderEntityScore(c *gin.Context, in *scoring.EntityPageInput, projectID, selfID string) {
	// 取词交 adminui.PrepareBare：片段不经整份 Prepare，但模板要 t —— 漏注入时 Jet 把
	// 取词调用求值成空串（不报错、不 500、不记日志），整片提示会变成空白。
	c.HTML(http.StatusOK, "fragments/seo_score", adminui.PrepareBare(c,
		gin.H{"Score": h.entityScoreViewOf(c.Request.Context(), c, in, projectID, selfID)}))
}

// renderScoreView 渲染评分片段（空态与结果共用同一份模板：片段按 Score.OK 分流）。
func renderScoreView(c *gin.Context, sv productdto.ScoreView) {
	c.HTML(http.StatusOK, "fragments/seo_score", adminui.PrepareBare(c, gin.H{"Score": sv}))
}

// entityScoreView 评分结果 → 片段视图（含页型回显与 title 冲突清单）。
func entityScoreView(tr func(key, fallback string) string, in *scoring.EntityPageInput, res *scoring.Result,
	dups []scoring.TitleEntry) productdto.ScoreView {
	sv := productdto.ScoreView{OK: true}
	if res != nil {
		sv.Total = res.Total
		sv.Grade = res.Grade
		// 页型与调权理由回显（docs/02-E1 §5）：编辑者要能看出「这份分数是按商品页的
		// 尺子量的」，否则同一段描述在商品页比文章页高几分会被当成评分器不稳。
		if res.Profile != nil {
			sv.ProfileType = res.Profile.Type
			sv.ProfileReason = res.Profile.Reason
		}
		// 未达标行的整句模板：标签 / 实测 / 基准 / 建议四段都是数据，句子的语序与标点
		// 由词条决定（英文的冒号与括号与中文不同），命名占位符 + FillTranslate 负责填充
		//（词条被写坏时自动回落下面那句中文兜底）。
		for _, sec := range res.Sections {
			item := productdto.ScoreSectionView{
				Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
				ColorLabel: seoscore.ScoreGradeText(tr, sec.Color),
			}
			for _, ck := range sec.Checks {
				if ck.Score >= ck.Max {
					continue
				}
				item.Issues = append(item.Issues, productdto.ScoreIssueView{
					Text: i18n.FillTranslate(tr, productenums.SEOIssueLine,
						"{label}：{actual}（基准 {benchmark}）→ {hint}",
						map[string]string{
							"label": ck.Label, "actual": ck.Actual,
							"benchmark": ck.Benchmark, "hint": ck.Hint,
						}),
					Target: ck.Target,
				})
			}
			sv.Sections = append(sv.Sections, item)
		}
	}
	title := scoring.EntityTitle(in)
	sv.SerpTitle = title
	if sv.SerpTitle == "" {
		sv.SerpTitle = tr(productenums.SEOSerpTitleEmpty, "（未填写标题）")
	} else {
		sv.SerpTitle = seoscore.TruncateDisplayWidth(sv.SerpTitle, 60)
	}
	sv.SerpURL = in.URL
	if sv.SerpURL == "" {
		sv.SerpURL = tr(productenums.SEOSerpURLEmpty, "（线上路径未知）")
	}
	sv.SerpDesc = strings.TrimSpace(in.SEODescription)
	if sv.SerpDesc == "" {
		sv.SerpDesc = strings.TrimSpace(in.Description)
	}
	if sv.SerpDesc == "" {
		sv.SerpDesc = tr(productenums.SEOSerpDescEmpty, "（未填写描述）")
	}
	// title 唯一性（SEO-018 编辑期轻量版）：结论形状与发布侧体检一致 —— 列出全部冲突页面。
	if len(dups) > 0 {
		sv.DuplicateNote = scoring.DuplicateTitleMessage(title, dups)
		for _, d := range dups {
			sv.Duplicates = append(sv.Duplicates, d.Page)
		}
	}
	return sv
}

// seoTitleIndex 同工程已发布 / 已有内容的标题索引（审计 SEO-018 编辑期轻量版）。
//
// 每类实体各一次列表查询（商品 / 分类 / 品牌 / 页面 / 文章），不遍历产物文件 ——
// 那是发布侧体检（SEO-019）的活，编辑期要的是点一下就能出结果。
//
// 与发布侧对齐的是**结论形状**（列出全部冲突页面），不是数据来源：发布侧读的是真正
// 服务出去的 <title>，这里读的是实体字段（写之前就得能提示）。两者的差异写在
// scoring.DuplicateTitles 的注释里，改任一处都要一起看。
//
// 「已发布」的判据按实体能力各自取：
//   - 商品：status=published（草稿商品还没有线上页面，拿它判重复会让运营被一堆
//     没上架的东西挡住）；
//   - 分类 / 品牌：它们是分类法，没有发布状态，随商品上线 —— 全量参与比对；
//   - 页面：草稿文档里看不出发布与否，按全部页面参与（宁可多提示一次，
//     也不要漏掉一个即将上线的重复标题）。
func (h *productPageHandle) seoTitleIndex(ctx context.Context, projectID string,
	tr func(key, fallback string) string) []scoring.TitleEntry {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	var out []scoring.TitleEntry
	if list, err := h.products.List(ctx, &productdto.ListReq{
		ProjectID: projectID, Status: productenums.StatusPublished, Size: seoTitleIndexSize,
	}); err == nil {
		for _, p := range list {
			if p == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				// 标题就是商品名（2026-09-30 合并后口径）：唯一性检查必须拿**会发布出去
				// 的那个标题**比对，再读 seo_title 只会拿库里的残留旧值去比。
				Title: p.Name,
				Page:  slashSlug(p.Slug),
				ID:    p.ID,
			})
		}
	}
	if cats, err := h.flatCategories(ctx, projectID); err == nil {
		for _, cat := range cats {
			if cat == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				Title: cat.Name,
				Page:  slashSlug(cat.Slug),
				ID:    cat.ID,
			})
		}
	}
	if brands, err := h.listBrands(ctx, projectID); err == nil {
		for _, b := range brands {
			if b == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				Title: b.Name,
				Page:  slashSlug(b.Slug),
				ID:    b.ID,
			})
		}
	}
	// 页面与文章（可空注入）：两者都是「同一个站点的其它页面」，缺了它们
	// 跨内容的重复标题检不出来 —— 但缺依赖不该让评分不可用，故按可选处理。
	if h.seoPages != nil {
		if drafts, err := h.seoPages.ListDrafts(ctx); err == nil {
			for _, d := range drafts {
				if d.ProjectID != projectID {
					continue
				}
				if title := pageDraftTitle(d.DraftDocument); title != "" {
					out = append(out, scoring.TitleEntry{Title: title, Page: slashSlug(d.DraftPath), ID: d.ID})
				}
			}
		}
	}
	if h.seoContents != nil {
		if rows, err := h.seoContents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: seoTitleIndexSize}); err == nil {
			for _, r := range rows {
				if r == nil || r.Data == nil {
					continue
				}
				// 文章的 SEO 标题即标题（2026-09-30 合并）：不再优先读 seoTitle。
				title := contentText(r.Data, "title")
				if title == "" {
					continue
				}
				// contents 列表接口没有工程过滤参数，标题索引据此标注来源：
				// 多工程部署时别的工程的文章也会进这份索引，宁可报一次重复让人核对，
				// 也好过因为查不到而假报「没有重复」。
				out = append(out, scoring.TitleEntry{
					Title: title,
					Page: i18n.FillTranslate(tr, productenums.SEOIndexArticle, "文章 {slug}",
						map[string]string{"slug": r.Slug}),
					ID: r.ID,
				})
			}
		}
	}
	return out
}

// instancePath 取实体详情页实例的线上路径（未注入实例契约 / 未发布 → 空串）。
func (h *productPageHandle) instancePath(ctx context.Context, entityType, entityID string) string {
	if h.instances == nil || strings.TrimSpace(entityID) == "" {
		return ""
	}
	inst, err := h.instances.GetByEntity(ctx, &presentationcontract.GetByEntityReq{
		EntityType: entityType, EntityID: entityID,
	})
	if err != nil || inst == nil {
		return ""
	}
	return strings.TrimSpace(inst.URLPath)
}

// entityScoreInputOf 按实体类型取数并构造评分输入。
//
// 三个 POST 端点共用这一份：同一个实体不该有两种取数口径（抽屉与行内按钮各写一份，
// 下场是「同一个商品在两处分数不同」）。
//
// kind 就是 scoring.PageKind（「用哪套尺子」）：取值不在这里另立一份常量 ——
// 控制器此前自带 scoreKindProduct / Category / Brand 三个字符串，与 scoring.Kind* 逐字同值，
// 改一处另一处静默失配。缺实体 id 时的空态文案走 productenums.ScoreMissingEntityLabel。
//
// 第三个返回值是**空态视图**（OK=false，Empty 已取好词）：非 OK 时调用方直接渲染它 ——
// 三个端点因此共用同一条失败路径，不必各自拼一句文案。
func (h *productPageHandle) entityScoreInputOf(ctx context.Context, c *gin.Context,
	kind scoring.PageKind, id, projectID string) (in *scoring.EntityPageInput, selfID string, emptyView productdto.ScoreView) {
	tr := shell.TranslateFor(c)
	emptyOf := func(key, fallback string) productdto.ScoreView { return productdto.ScoreView{Empty: tr(key, fallback)} }

	if id == "" {
		// 缺实体 id 的空态：页型 → 文案事实的映射在 enums（控制器不挑文案）。
		key, fallback := productenums.ScoreMissingEntityLabel(string(kind))
		return nil, "", emptyOf(key, fallback)
	}

	switch kind {
	case scoring.KindCategory:
		node, err := h.products.GetCategory(ctx, &productdto.GetCategoryReq{ID: id})
		if err != nil || node == nil {
			return nil, "", emptyOf(productenums.SEOEmptyCategoryUnreadable, "读不到这个分类，无法评分")
		}
		// SEO 标题 / 描述与「分类名 / 分类描述」合并（2026-09-30，与商品同口径）：
		// 分类名即 <title>、分类描述即 meta description。描述是富文本，先去掉标签
		// 再喂给评分器 —— 否则 <p> 会被算进 meta 描述长度。
		instPath := h.instancePath(ctx, productcontract.EntityTypeCategory, node.ID)
		return &scoring.EntityPageInput{
			Kind:           scoring.KindCategory,
			Name:           node.Name,
			Description:    stripEntityTags(node.Description),
			SEOTitle:       node.Name,
			SEODescription: stripEntityTags(node.Description),
			Slug:           node.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(node.Slug)),
			Images:         singleImage(node.Image, "hero"),
			ChildNames:     categoryChildNames(ctx, h, projectID, node.ID),
			Locale:         requestScoreLang(c),
			HasCanonical:   instPath != "",
			HasSchema:      instPath != "",
		}, node.ID, productdto.ScoreView{}

	case scoring.KindBrand:
		brand, err := h.products.GetBrand(ctx, &productdto.GetBrandReq{ID: id})
		if err != nil || brand == nil {
			return nil, "", emptyOf(productenums.SEOEmptyBrandUnreadable, "读不到这个品牌，无法评分")
		}
		// 同分类：品牌名即 <title>、品牌描述即 meta description（2026-09-30 合并）。
		instPath := h.instancePath(ctx, productcontract.EntityTypeBrand, brand.ID)
		return &scoring.EntityPageInput{
			Kind:           scoring.KindBrand,
			Name:           brand.Name,
			Description:    stripEntityTags(brand.Description),
			SEOTitle:       brand.Name,
			SEODescription: stripEntityTags(brand.Description),
			Slug:           brand.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(brand.Slug)),
			Images:         singleImage(brand.Logo, "hero"),
			Locale:         requestScoreLang(c),
			HasCanonical:   instPath != "",
			HasSchema:      instPath != "",
		}, brand.ID, productdto.ScoreView{}

	default:
		detail, err := h.products.Get(ctx, &productdto.GetReq{ID: id})
		if err != nil || detail == nil {
			return nil, "", emptyOf(productenums.SEOEmptyProductUnreadable, "读不到这个商品，无法评分")
		}
		// 线上路径与「产物是否已带 canonical / JSON-LD」是同一个问题的两面：
		// 实例有线上路径 = 详情页真的发布过 = 构建期已注入这两样（presentation 的 applyEntitySEO）。
		instPath := h.instancePath(ctx, productcontract.EntityTypeProduct, detail.ID)
		return &scoring.EntityPageInput{
			Kind:        scoring.KindProduct,
			Name:        detail.Name,
			Subtitle:    detail.Subtitle,
			Description: entityPlainText(detail.Description),
			// 评分与前台对齐（2026-09-30 合并）：商品名即 <title>、副标题即 meta description。
			// 若这里仍读 seo_title/seo_description，评分看到的标题会和实际发布出去的**不是同一个**——
			// 编辑改商品名、分数不动，是最难解释的一种不一致。
			SEOTitle:       detail.Name,
			SEODescription: detail.Subtitle,
			Slug:           detail.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(detail.Slug)),
			Images:         productPageImages(detail),
			SpecNames:      variationSpecNames(detail),
			ChildNames:     productCategoryNames(detail),
			Locale:         requestScoreLang(c),
			// 未发布时判假：评分器只该显示编辑者能改的东西，而这两项要发布一次才会出现。
			HasCanonical: instPath != "",
			HasSchema:    instPath != "",
		}, detail.ID, productdto.ScoreView{}
	}
}

// EntitySeoDrawer 商品 SEO 评分抽屉片段（GET /admin/products/seo/drawer?productId=&project=）。
//
// 为什么只有商品走抽屉：抽屉基座（ui/drawer.js）是**单例** —— 打开新抽屉会替换当前那个。
// 而分类 / 品牌的评分按钮活在**行内编辑抽屉**里（列表页的编辑模板 include 了表单片段），
// 把那里改成跳转式抽屉，用户在分类编辑到一半点「SEO 评分」，正在填的表单会被顶掉且无路返回。
// 那两处保持原有的「htmx 就地展开分数」形态 —— 它们本来就不占常驻版面，没有要修的问题。
//
// 商品编辑页是**独立整页**，不存在这个冲突，且它原来有一张常驻折叠卡占着版面底部。
//
// 打开即算：抽屉本来就是「我要看分」这个动作的落点，不该再要用户先点一次「开始评分」
// （行内按钮需要那一步，是因为它没有「打开」这个动作）。
func (h *productPageHandle) EntitySeoDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("productId"))
	projectID := strings.TrimSpace(c.Query("project"))
	if h == nil || h.products == nil {
		c.Status(http.StatusNotFound)
		return
	}
	data := gin.H{
		"ScoreURL":     "/admin/products/seo-score",
		"TargetID":     "product-seo-score-" + id,
		"HiddenFields": productScoreHiddenFields(id, projectID),
	}
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoring.KindProduct, id, projectID)
	if in == nil {
		// 读不到实体：仍给 200 + 片段（里面就是那句空态文案）—— 这不是「加载失败」，
		// 抽屉的失败重试界面会把「这个商品读不到」伪装成网络问题。
		data["Score"] = emptyView
		c.HTML(http.StatusOK, "admin/product/entity_seo_drawer.html", adminui.Prepare(c, data))
		return
	}
	data["Score"] = h.entityScoreViewOf(c.Request.Context(), c, in, projectID, selfID)
	c.HTML(http.StatusOK, "admin/product/entity_seo_drawer.html", adminui.Prepare(c, data))
}

// productScoreHiddenFields 商品评分端点的隐藏字段（与页面上那个 POST 端点同一口径）。
func productScoreHiddenFields(id, projectID string) []seoHiddenField {
	return []seoHiddenField{{"projectId", projectID}, {"productId", id}}
}

// seoHiddenField 抽屉里「评分」表单的一个隐藏字段。
type seoHiddenField struct{ Name, Value string }

// pageDraftTitle 页面草稿的 SEO 标题（settings.seo.title）。
func pageDraftTitle(doc json.RawMessage) string {
	if len(doc) == 0 {
		return ""
	}
	var parsed struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Settings.SEO.Title)
}

// contentText 取内容实体数据里的一个字符串字段（非字符串按空处理）。
func contentText(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// entityPlainText 商品描述（jsonb）→ 纯文本。
//
// 两种形态与构建期一致（{"html": "..."} 富文本 / JSON 字符串纯文本），
// 去标签的理由与文章侧相同：把 <p> 当正文算进字数，一篇 300 字的描述会被算成 900。
func entityPlainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if s, ok := asMap["html"].(string); ok {
			return stripEntityTags(s)
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return stripEntityTags(asString)
	}
	return ""
}

var entityTagRe = regexp.MustCompile("<[^>]+>")

// stripEntityTags 去 HTML 标签并压掉多余空白（只读展示与统计用，不承担清洗职责）。
func stripEntityTags(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(entityTagRe.ReplaceAllString(s, " ")), " "))
}

// productPageImages 商品图集 → 评分图片项（首图按 hero、其余按 content，
// alt 与 Images 逐位对应 —— 与构建期 imageAltsJSON 同一份数据）。
func productPageImages(p *productdto.ProductResp) []scoring.Image {
	if p == nil {
		return nil
	}
	out := make([]scoring.Image, 0, len(p.Images))
	for i, src := range p.Images {
		if strings.TrimSpace(src) == "" {
			continue
		}
		kind := "content"
		if i == 0 {
			kind = "hero"
		}
		alt := ""
		if i < len(p.ImageAlts) {
			alt = strings.TrimSpace(p.ImageAlts[i])
		}
		out = append(out, scoring.Image{Src: strings.TrimSpace(src), Alt: alt, Kind: kind})
	}
	return out
}

// singleImage 单图实体（分类图 / 品牌 logo）→ 评分图片项。
func singleImage(src, kind string) []scoring.Image {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	return []scoring.Image{{Src: strings.TrimSpace(src), Kind: kind}}
}

// variationSpecNames 参与变体的属性组名（详情页把它们渲染成分组小标题）。
func variationSpecNames(p *productdto.ProductResp) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Attributes))
	for _, a := range p.Attributes {
		if a != nil && a.IsVariation && strings.TrimSpace(a.Name) != "" {
			out = append(out, strings.TrimSpace(a.Name))
		}
	}
	return out
}

// productCategoryNames 商品挂载的分类名（详情页的关联分区标题）。
func productCategoryNames(p *productdto.ProductResp) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Categories))
	for _, c := range p.Categories {
		if c != nil && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out
}

// categoryChildNames 某分类的子分类名（列表页把子分类渲染成小节）。
//
// 取不到（分类不存在 / 列表失败）返回 nil：少一个小标题只会让标题结构项如实报缺，
// 不会让评分整体失败。
func categoryChildNames(ctx context.Context, h *productPageHandle, projectID, id string) []string {
	cats, err := h.flatCategories(ctx, projectID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, 2)
	for _, c := range cats {
		if c != nil && c.ParentID == id && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out
}
