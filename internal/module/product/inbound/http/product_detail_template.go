package producthttp

import (
	"context"
	"errors"
	"fmt"
	"go_wp/internal/adminui"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	ctoutbound "go_wp/internal/module/product/outbound/contenttemplate"
	dtoutbound "go_wp/internal/module/product/outbound/detailtemplate"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/response"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_detail_template.go — 商品详情页模板（issue #14）与 SEO 评分的 **HTTP 层**。
//
// 这两块过去同文件、且逻辑也都在这里；拆分后本文件只做 HTTP 该做的事：请求绑定、
// 权限入口（路由在 product_router.go）、调用 service 用例、HTTP 错误映射与响应渲染。
// 两块能力的实现位置：
//
//	· 详情页模板：模板文档组装 / 首次发布 / 切换模板 / 改 URL / 预览编排 →
//	  internal/module/product/service/product_detail_template_ops.go；
//	  整页读模型 → internal/module/product/service/product_detail_template_page.go
//	  （两个文件名像、职责不同，改之前看各自文件头）。
//
// SEO 评分的视图层在 product_score.go（三个实体共用），不在本文件。

// 编译期断言：service 用例声明的两个窄端口由本包这两个契约面满足 ——
// 端口是「评分输入 / 模板编排真正需要什么」的形状，契约改宽改窄时这里先报错。
var (
	_ productservice.ProductReadPort            = (productcontract.ProductService)(nil)
	_ productservice.DetailTemplateInstancePort = (ProductDetailTemplatePort)(nil)
)

var errModeUnavailable = errors.New("详情页双轨能力未装配")

// ReapplyDetailTemplatePreset POST /api/product/detail-template/reapply-preset：
// 重新套用预设 —— 放弃该商品实例的独立文档，回到跟随模板并重建（迁移 282 的另一半）。
//
// 页面侧原先的 /admin/products/reapply-preset 随写通道一起删除：结论改成一行回执
// （成功由 ui/json_write.js 存进 flash 再整页刷新 —— 模式徽标变了，必须重渲染）。
func (h *Handle) ReapplyDetailTemplatePreset(c *gin.Context) {
	req := &productdto.DetailTemplateModeReapplyReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	port, err := detailTemplateModePortFor(req.InstanceID)
	if err != nil {
		productDetailTemplateModeFail(c, err)
		return
	}
	if _, err = port.ReapplyPreset(c.Request.Context(), &presentationcontract.ReapplyPresetReq{
		InstanceID: strings.TrimSpace(req.InstanceID),
		ProjectID:  strings.TrimSpace(req.ProjectID),
		TemplateID: strings.TrimSpace(req.TemplateID),
	}); err != nil {
		productDetailTemplateModeFail(c, err)
		return
	}
	response.SuccessWithMessage(c, productMsgText(c, productActionDoneMsg), nil)
}

// RollbackDetailTemplateDocument POST /api/product/detail-template/rollback-document：
// 快照级文档回滚 —— 取历史快照的文档重新发布（实体数据仍取最新）。
//
// 与上面的重新套用预设是**对称的两半**：一个放弃独立文档，一个把某份历史文档立为当前。
// 两者都重建产物，所以成功之后页面必须刷新（产物指针与快照清单都变了）。
func (h *Handle) RollbackDetailTemplateDocument(c *gin.Context) {
	req := &productdto.DetailTemplateModeRollbackReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	port, err := detailTemplateModePortFor(req.InstanceID)
	if err != nil {
		productDetailTemplateModeFail(c, err)
		return
	}
	if _, err = port.RollbackDocument(c.Request.Context(), &presentationcontract.RollbackDocumentReq{
		InstanceID: strings.TrimSpace(req.InstanceID),
		ProjectID:  strings.TrimSpace(req.ProjectID),
		SnapshotID: strings.TrimSpace(req.SnapshotID),
	}); err != nil {
		productDetailTemplateModeFail(c, err)
		return
	}
	response.SuccessWithMessage(c, productMsgText(c, productActionDoneMsg), nil)
}

// detailTemplateWriteDeps 详情页模板页四个写动作的依赖（装配期**后置**注入）。
//
// 为什么不让 Handle 在构造时持有：这四个动作要 contenttemplate 与 presentation 两个契约，
// 而 SetupProductRoutes 只拿得到 database 与 project —— 内容模板与发布服务在
// assembly_publish 那一段才建好（同 BindDetailTemplateModePort 的时序，见 product_router.go 末尾）。
// 未注入 = 装配缺陷，走 errModeUnavailable → 500 归口文案，而不是让请求在空指针上崩。
var detailTemplateWriteDeps *productservice.DetailTemplateDeps

// BindDetailTemplateWriteDeps 注入四个写动作的依赖（装配期调用一次）。
func BindDetailTemplateWriteDeps(deps productservice.DetailTemplateDeps) {
	detailTemplateWriteDeps = &deps
}

func detailTemplateWriteDepsOrErr() (productservice.DetailTemplateDeps, error) {
	if detailTemplateWriteDeps == nil {
		return productservice.DetailTemplateDeps{}, errModeUnavailable
	}
	return *detailTemplateWriteDeps, nil
}

// detailTemplateWriteFail 四个写动作的 JSON 失败出口。
//
// 文案已经由 detailTemplate*Msg 系列收口（presentation / contenttemplate 的 enums → 人话），
// 这里只决定状态码：参数与业务状态问题 400、对象不存在 404。
func detailTemplateWriteFail(c *gin.Context, status int, msg productMsg) {
	response.ErrorWithMessage(c, status, productMsgText(c, msg))
}

// CreateDetailTemplate POST /api/product/detail-template/create：
// 新建一套命名模板（复制指定模板或默认模板的文档），初始版本 v1。
//
// 模板内容编辑不在这里：新建只给出一个可被工作台打开的结构，改文档走 /workbench?template=…。
func (h *Handle) CreateDetailTemplate(c *gin.Context) {
	req := &productdto.DetailTemplateCreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	deps, err := detailTemplateWriteDepsOrErr()
	if err != nil {
		productDetailTemplateModeFail(c, err)
		return
	}
	// binding 只拦空串，拦不住「全是空格」的名字；判据必须在 TrimSpace 之后。
	name := strings.TrimSpace(req.Name)
	if name == "" {
		detailTemplateWriteFail(c, http.StatusBadRequest, productDetailTemplateNameRequiredMsg)
		return
	}
	// 顺序：**先取文档、再建模板**（取不到复制来源就不建空的）。
	if err = productservice.CreateDetailTemplate(c.Request.Context(), deps,
		strings.TrimSpace(req.ProjectID), name, strings.TrimSpace(req.CopyFrom)); err != nil {
		detailTemplateWriteFail(c, http.StatusBadRequest, detailTemplateTemplateMsg(c, err))
		return
	}
	response.SuccessWithMessage(c, productMsgText(c, productActionDoneMsg), nil)
}

// PublishDetailTemplate POST /api/product/detail-template/publish：
// 首次为该商品建立发布实例并发布（URLPath 留空时按站点 URL 规则从商品 slug 派生）。
//
// 两类失败走**两张不同的文案表**：取商品失败（product 的 enums）与建实例失败
// （presentation 的 enums）合成一句会把「商品不存在」说成「还没有详情页实例」。
func (h *Handle) PublishDetailTemplate(c *gin.Context) {
	req := &productdto.DetailTemplatePublishReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	deps, err := detailTemplateWriteDepsOrErr()
	if err != nil {
		productDetailTemplateModeFail(c, err)
		return
	}
	err = productservice.PublishDetailTemplate(c.Request.Context(), deps,
		strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.ProductID),
		strings.TrimSpace(req.TemplateID), strings.TrimSpace(req.URLPath))
	if err != nil {
		var lookup *productservice.DetailTemplateProductLookupError
		if errors.As(err, &lookup) {
			detailTemplateWriteFail(c, http.StatusNotFound, productErrMsg(c, lookup.Err))
			return
		}
		detailTemplateWriteFail(c, http.StatusBadRequest, detailTemplateFacingMsg(c, err))
		return
	}
	response.SuccessWithMessage(c, productMsgText(c, productActionDoneMsg), nil)
}

// ApplyDetailTemplate POST /api/product/detail-template/apply：
// 把商品切换（或确认）到选中的模板并重新发布 —— 产物随模板变化。
//
// projectID 显式回传，不走 presentation 的「工程唯一时取该工程」兜底：多工程站点上
// 兜底会落到另一个工程，改的是别的站点的页面而这里仍回成功。
func (h *Handle) ApplyDetailTemplate(c *gin.Context) {
	req := &productdto.DetailTemplateApplyReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	deps, err := detailTemplateWriteDepsOrErr()
	if err != nil {
		productDetailTemplateModeFail(c, err)
		return
	}
	if err = productservice.ApplyDetailTemplate(c.Request.Context(), deps.Instances,
		strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.ProductID),
		strings.TrimSpace(req.TemplateID)); err != nil {
		detailTemplateWriteFail(c, http.StatusBadRequest, detailTemplateFacingMsg(c, err))
		return
	}
	response.SuccessWithMessage(c, productMsgText(c, productActionDoneMsg), nil)
}

// UpdateDetailTemplateURL POST /api/product/detail-template/update-url：
// 修改商品详情页的线上路径（内容、模板绑定、产物内容完全不动）。
func (h *Handle) UpdateDetailTemplateURL(c *gin.Context) {
	req := &productdto.DetailTemplateUpdateURLReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	deps, err := detailTemplateWriteDepsOrErr()
	if err != nil {
		productDetailTemplateModeFail(c, err)
		return
	}
	newPath := strings.TrimSpace(req.NewPath)
	if newPath == "" {
		detailTemplateWriteFail(c, http.StatusBadRequest, productDetailTemplatePathRequiredMsg)
		return
	}
	if err = productservice.UpdateDetailTemplateURL(c.Request.Context(), deps.Instances,
		strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.ProductID), newPath,
		req.WithRedirect); err != nil {
		detailTemplateWriteFail(c, http.StatusBadRequest, detailTemplateFacingMsg(c, err))
		return
	}
	response.SuccessWithMessage(c, productMsgText(c, productActionDoneMsg), nil)
}

// detailTemplateModePortFor 取双轨能力并校验实例标识（两个端点的共同前置）。
//
// 端口是后置注入的（见 product_router.go 的 BindDetailTemplateModePort）：没注入 = 装配缺陷，
// 走非业务错误出口（500 归口文案，说清楚是哪一类问题）；实例标识空 = 请求不合法 ——
// binding 已拦一次，这里是服务端边界，不假设调用方一定经过模板。
func detailTemplateModePortFor(instanceID string) (ProductDetailTemplateModePort, error) {
	if detailTemplateModePort == nil {
		return nil, errModeUnavailable
	}
	if strings.TrimSpace(instanceID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	return detailTemplateModePort, nil
}

// productDetailTemplateModeFail 双轨写动作的 JSON 失败出口。
//
// 两类失败分档：业务 key（本模块或 presentation 的 enums 常量）按成品文案回报，
// 400 / 404 如实；非业务（装配缺失、驱动报错）交给 ErrorAuto —— 原文只进日志，对外 500
// 归口文案。直接写 err.Error() 两条都不满足：裸 key 运营看不懂，库表细节又不该出去。
func productDetailTemplateModeFail(c *gin.Context, err error) {
	key := productErrKeyOf(err)
	if key == "" {
		response.ErrorAuto(c, http.StatusInternalServerError, productErrScene, err)
		return
	}
	status := http.StatusBadRequest
	if key == presentationenums.ErrNotFound {
		status = http.StatusNotFound
	}
	response.ErrorWithMessage(c, status, productMsgText(c, productErrMsg(c, err)))
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

// detailTemplateDeps 把装配期注入的依赖打成 service 用例的入参（写动作 / 预览共用）。
//
// 为什么不直接让 handler 持有 service 实例：本包测试用 fake 实现 productcontract.ProductService
// 构造 handler（NewProductPageHandle(fake, nil, nil)），handler 一旦要求具体的 service 实例，
// 这些 fake 场景就全部用不了。装配照旧在 product_router.go，这里只把同一批依赖换个形状交出去。
func (h *productPageHandle) detailTemplateDeps() productservice.DetailTemplateDeps {
	return productservice.DetailTemplateDeps{
		Templates: h.templates, Instances: h.instances, Products: h.products, Projects: h.projects,
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
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
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
	res, err := productservice.PreviewDetailTemplate(c.Request.Context(), h.instances,
		c.PostForm("projectId"), c.PostForm("productId"), strings.TrimSpace(c.PostForm("templateId")))
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
// productErrFallbackText / 共享归口）。
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
// SEO 评分：行内面板、抽屉与三个实体的取数入口（商品 / 分类 / 品牌）。
//
// 取数与算分都在 productservice（product_seo_score.go 的 SeoScoreInputOf / SeoTitleIndex /
// SeoEntityScore）—— 三个 POST 端点与抽屉共用**同一份**取数口径，抽屉与行内按钮各写一份的
// 下场是「同一个商品在两处分数不同」。本文件只剩请求绑定、取词、调用与渲染。

// 评分等级文案（颜色 → 当前语言）的**唯一映射**在 seoscore.ScoreGradeText
// （internal/seo/score_grade.go），调用点在下面的 entityScoreView。本文件此前自带一份
// key + 中文兜底表（与本模块 enums 里的 admin.seo.grade.* 常量配套），已按「同一批展示文案
// 只能有一份定义」收编 —— 收编后 product 取到的是 content / project 用的同一批词条
// admin.seo.score.grade.*。
// ==========================================================================
const (
	MsgProductsTitle = "MsgProductsTitle"
)
