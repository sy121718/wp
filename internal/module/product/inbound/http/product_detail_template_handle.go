package producthttp

import (
	"context"
	"errors"
	"fmt"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	ctoutbound "go_wp/internal/module/product/outbound/contenttemplate"
	dtoutbound "go_wp/internal/module/product/outbound/detailtemplate"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_detail_template_handle.go — 商品详情页模板（issue #14）与 SEO 评分的 **HTTP 层**。
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
// SEO 评分的视图层在 product_score_handle.go（三个实体共用），不在本文件。

// productActionDoneFallback 单条写动作的通用成功回执的中文兜底（key 在
// productenums.ProductsActionDone）。
//
// 不复用 enums 的 MsgCreateSuccess / MsgUpdateSuccess：那几个 key 的值在词条表里是
// 「Blueprint 创建成功」这类**蓝图域**的句子（058/430 早先 seed），商品页用它会显示成
// 「Blueprint 创建成功」。这里另起一条商品域 key（登记前走中文兜底）。
const productActionDoneFallback = "操作已完成"

// productActionDoneMsg 单条写动作（新建 / 修改 / 保存）的成功回执事实。
var productActionDoneMsg = facing.Msg{Key: productenums.ProductsActionDone, Fallback: productActionDoneFallback}

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
// （成功由 ui/write-base.js 存进 flash 再整页刷新 —— 模式徽标变了，必须重渲染）。
func (h *ProductHandle) ReapplyDetailTemplatePreset(c *gin.Context) {
	req, ok := binding.JSON[productdto.DetailTemplateModeReapplyReq](c)
	if !ok {
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
	response.SuccessMsg(c, productActionDoneMsg)
}

// RollbackDetailTemplateDocument POST /api/product/detail-template/rollback-document：
// 快照级文档回滚 —— 取历史快照的文档重新发布（实体数据仍取最新）。
//
// 与上面的重新套用预设是**对称的两半**：一个放弃独立文档，一个把某份历史文档立为当前。
// 两者都重建产物，所以成功之后页面必须刷新（产物指针与快照清单都变了）。
func (h *ProductHandle) RollbackDetailTemplateDocument(c *gin.Context) {
	req, ok := binding.JSON[productdto.DetailTemplateModeRollbackReq](c)
	if !ok {
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
	response.SuccessMsg(c, productActionDoneMsg)
}

// detailTemplateWriteDeps 详情页模板页四个写动作的依赖（装配期**后置**注入）。
//
// 为什么不让 ProductHandle 在构造时持有：这四个动作要 contenttemplate 与 presentation 两个契约，
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
func detailTemplateWriteFail(c *gin.Context, status int, msg facing.Msg) {
	response.ErrorMsg(c, status, msg)
}

// detailTemplateDepsOrFail 取写动作依赖；没装配就地写完 500 并告诉调用方别再往下走。
func detailTemplateDepsOrFail(c *gin.Context) (productservice.DetailTemplateDeps, bool) {
	deps, err := detailTemplateWriteDepsOrErr()
	if err != nil {
		productDetailTemplateModeFail(c, err)
		return productservice.DetailTemplateDeps{}, false
	}
	return deps, true
}

// detailTemplateWriteJSON 四个写动作的共同骨架：绑定 → 取依赖 → 交给业务函数 → 回执。
//
// 业务函数里要自己写完响应（例如名字为空这种请求级校验）时返回 response.ErrHandled，
// 收尾只认「已经写完」而不再叠一条回执。
func detailTemplateWriteJSON[T any](c *gin.Context, run func(req *T, deps productservice.DetailTemplateDeps) error) {
	req, ok := binding.JSON[T](c)
	if !ok {
		return
	}
	deps, ok := detailTemplateDepsOrFail(c)
	if !ok {
		return
	}
	detailTemplateFinish(c, run(req, deps))
}

// detailTemplateFinish 写动作的收尾：成功一句回执；取商品失败 404；其余业务失败 400。
func detailTemplateFinish(c *gin.Context, err error) {
	if err == nil {
		response.SuccessMsg(c, productActionDoneMsg)
		return
	}
	if errors.Is(err, response.ErrHandled) {
		return
	}
	status := http.StatusBadRequest
	var lookup *productservice.DetailTemplateProductLookupError
	if errors.As(err, &lookup) {
		status = http.StatusNotFound
	}
	detailTemplateWriteFail(c, status, detailTemplateErrMsg(err))
}

// CreateDetailTemplate POST /api/product/detail-template/create：
// 新建一套命名模板（复制指定模板或默认模板的文档），初始版本 v1。
//
// 模板内容编辑不在这里：新建只给出一个可被工作台打开的结构，改文档走 /workbench?template=…。
func (h *ProductHandle) CreateDetailTemplate(c *gin.Context) {
	detailTemplateWriteJSON(c, func(req *productdto.DetailTemplateCreateReq, deps productservice.DetailTemplateDeps) error {
		// binding 只拦空串，拦不住「全是空格」的名字；判据必须在 TrimSpace 之后。
		name := strings.TrimSpace(req.Name)
		if name == "" {
			detailTemplateWriteFail(c, http.StatusBadRequest, productDetailTemplateNameRequiredMsg)
			return response.ErrHandled
		}
		// 顺序：**先取文档、再建模板**（取不到复制来源就不建空的）。
		return productservice.CreateDetailTemplate(c.Request.Context(), deps, strings.TrimSpace(req.ProjectID), name, strings.TrimSpace(req.CopyFrom))
	})
}

// PublishDetailTemplate POST /api/product/detail-template/publish：
// 首次为该商品建立发布实例并发布（URLPath 留空时按站点 URL 规则从商品 slug 派生）。
//
// 取商品失败（product 的 enums）与建实例失败（presentation 的 enums）是两类事实：
// 收尾按 DetailTemplateProductLookupError 单独给 404，合成一句会把「商品不存在」
// 说成「还没有详情页实例」。
func (h *ProductHandle) PublishDetailTemplate(c *gin.Context) {
	detailTemplateWriteJSON(c, func(req *productdto.DetailTemplatePublishReq, deps productservice.DetailTemplateDeps) error {
		return productservice.PublishDetailTemplate(c.Request.Context(), deps,
			strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.ProductID),
			strings.TrimSpace(req.TemplateID), strings.TrimSpace(req.URLPath))
	})
}

// ApplyDetailTemplate POST /api/product/detail-template/apply：
// 把商品切换（或确认）到选中的模板并重新发布 —— 产物随模板变化。
//
// projectID 显式回传，不走 presentation 的「工程唯一时取该工程」兜底：多工程站点上
// 兜底会落到另一个工程，改的是别的站点的页面而这里仍回成功。
func (h *ProductHandle) ApplyDetailTemplate(c *gin.Context) {
	detailTemplateWriteJSON(c, func(req *productdto.DetailTemplateApplyReq, deps productservice.DetailTemplateDeps) error {
		return productservice.ApplyDetailTemplate(c.Request.Context(), deps.Instances,
			strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.ProductID),
			strings.TrimSpace(req.TemplateID))
	})
}

// UpdateDetailTemplateURL POST /api/product/detail-template/update-url：
// 修改商品详情页的线上路径（内容、模板绑定、产物内容完全不动）。
func (h *ProductHandle) UpdateDetailTemplateURL(c *gin.Context) {
	detailTemplateWriteJSON(c, func(req *productdto.DetailTemplateUpdateURLReq, deps productservice.DetailTemplateDeps) error {
		newPath := strings.TrimSpace(req.NewPath)
		if newPath == "" {
			detailTemplateWriteFail(c, http.StatusBadRequest, productDetailTemplatePathRequiredMsg)
			return response.ErrHandled
		}
		return productservice.UpdateDetailTemplateURL(c.Request.Context(), deps.Instances,
			strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.ProductID), newPath, req.WithRedirect)
	})
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
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	return detailTemplateModePort, nil
}

// productDetailTemplateModeFail 双轨写动作的 JSON 失败出口。
//
// 两类失败分档：业务 key（本模块或 presentation 的 enums 常量，兜底各自跟着声明处走）按成品文案回报，
// 400 / 404 如实；非业务（装配缺失、驱动报错）交给 ErrorAuto —— 原文只进日志，对外 500
// 归口文案。直接写 err.Error() 两条都不满足：裸 key 运营看不懂，库表细节又不该出去。
func productDetailTemplateModeFail(c *gin.Context, err error) {
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	status := http.StatusBadRequest
	if fact.Key == presentationenums.ErrNotFound {
		status = http.StatusNotFound
	}
	response.ErrorMsg(c, status, facing.Msg{Key: fact.Key, Fallback: fact.Fallback})
}

// 详情页模板与发布实例都以实体类型为键：取值用 productcontract.EntityTypeProduct
//（实体类型注册表里 product 那一档），不在控制器里另留一份 "product" 字面量。

// productDetailTemplatePath 详情页模板页路径（列表行内入口）。
const productDetailTemplatePath = "/admin/products/template"

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
func (h *ProductPageHandle) SetDetailTemplateModePort(port ProductDetailTemplateModePort) {
	h.modePort = port
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetDetailTemplatePort(dtoutbound.New(h.templates, h.instances, port))
	}
}

// SetDetailTemplateDeps 注入「详情页模板」页所需的两份契约（装配期调用）。
//
// 用 setter 而非构造参数：既有装配（含页面测试）按两参数构造商品页 handle，
// 详情模板是增量能力；未注入时页面明确提示，而不是整体不可用。
func (h *ProductPageHandle) SetDetailTemplateDeps(templates contenttemplatecontract.ContentTemplateService,
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
		wiring.SetDetailTemplatePort(dtoutbound.New(templates, instances, h.modePort))
	}
}

// detailTemplateDeps 把装配期注入的依赖打成 service 用例的入参（写动作 / 预览共用）。
//
// 为什么不直接让 handler 持有 service 实例：本包测试用 fake 实现 productcontract.ProductService
// 构造 handler（NewProductPageHandle(fake, nil, nil)），handler 一旦要求具体的 service 实例，
// 这些 fake 场景就全部用不了。装配照旧在 product_router.go，这里只把同一批依赖换个形状交出去。
func (h *ProductPageHandle) detailTemplateDeps() productservice.DetailTemplateDeps {
	return productservice.DetailTemplateDeps{
		Templates: h.templates, Instances: h.instances, Products: h.products, Projects: h.projects,
	}
}

// ProductDetailTemplatePage GET /admin/products/template：某商品的详情页模板选择与预览页。
//
// 取数失败**不在这里出口**：整页 GET 只有末尾那一次渲染（成功 / 失败同模板同页壳，
// 差别只有状态码 / no-store / 失败槽）—— 契约见 docs/rules/template-boundary.md §整页 GET 的失败出口。
//
// 这一屏的两个出口判断留在控制器，因为它们都是**HTTP 出口**而不是取数结论：
//
//	· 依赖未装配 → 200 的就地说明（不是装载失败：区别是「没读到」与「这条能力没接上」）；
//	· 还没选商品 → 302 回商品列表（本页没有「还没选商品」的就地形态）。
func (h *ProductPageHandle) ProductDetailTemplatePage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductDetailTemplatePageReq](c)
	if !ok {
		return
	}
	h.renderDetailTemplatePage(c, req)
}

// renderDetailTemplatePage 详情页模板页的三条落地分支（未装配 / 取数失败 / 没选商品）。
//
// 未装配（缺内容模板 / 自动发布契约）是**装配缺陷的可见提示**，不是装载失败：
// 与「没读到数据」分开，仍然是 200 的就地说明（先于取数判断 —— 否则两个出口会互相盖住）。
func (h *ProductPageHandle) renderDetailTemplatePage(c *gin.Context, req *productdto.ProductDetailTemplatePageReq) {
	if h.templates == nil || h.instances == nil {
		pageview.Render(c, detailTemplateUnassembledView())
		return
	}
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误。
	view, err := h.reader.ProductDetailTemplatePage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	if view.NoProduct {
		// 侧栏菜单直接进本页时没有 product 参数：回商品列表挑一件，而不是渲染一个空壳页。
		c.Redirect(http.StatusFound, productListPath)
		return
	}
	pageview.Render(c, view)
}

// detailTemplateUnassembledView 模板能力未装配时的视图（200，就地说明）。
//
// Ready 为假 → 模板走「详情页模板能力未装配」那一块；**不置失败事实** —— 那是
// 「取数没读到」的槽，两者混用会让「没装配」被说成「数据读不出来」，而前者要人去查装配。
func detailTemplateUnassembledView() productdto.ProductDetailTemplatePageResp {
	view := detailTemplateLoadFailureView(http.StatusOK)
	return view
}

// detailTemplateLoadFailureView 失败档的视图（给齐模板必需键，避免失败分支只设了错误键）。
func detailTemplateLoadFailureView(status int) productdto.ProductDetailTemplatePageResp {
	return productdto.ProductDetailTemplatePageResp{
		Projects:  []projectdto.ProjectResp{},
		Templates: []productdto.DetailTemplateOptionResp{},
	}
}

// ProductDetailTemplatePreview POST /admin/products/template/preview：
// 发布前预览模板渲染效果 —— 只读渲染，直接把构建结果的 HTML 输出到新标签页；
// 响应头带上实际使用的模板与版本，便于核对「预览的是哪一套」。
func (h *ProductPageHandle) ProductDetailTemplatePreview(c *gin.Context) {
	if h.instances == nil {
		// 装配缺陷：预览没有可渲染的契约，出口交给统一错误形状（对外是归口文案，原文只进日志）。
		pageview.Fail(c, errors.New(errTemplateDepsMissing))
		return
	}
	req, ok := binding.Form[productdto.DetailTemplatePreviewReq](c)
	if !ok {
		return
	}
	res, err := productservice.PreviewDetailTemplate(c.Request.Context(), h.instances,
		req.ProjectID, req.ProductID, req.TemplateID)
	if err != nil {
		// 「预览失败」对作者是可用信息，原因（模板不存在 / 文档非法 / 渲染出错）只进日志。
		pageview.Fail(c, err)
		return
	}
	if res.TemplateName != "" {
		c.Header("X-Preview-Template", fmt.Sprintf("%s@v%d", res.TemplateName, res.TemplateVersion))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
	return
}

// detailTemplateErrMsg 把业务错误收敛成一条事实：key 与兜底都取自声明处。
//
// 这里没有「本页的错误来自哪个对端」的补丁表了：presentation 与 contenttemplate 的 enums
// 现在各自带 errFallbacks（同 membership / content / navigation 的形态），FactOf 给出的事实
// 本身就带着中文兜底，页面拿到的是一句能读的话，而不是 ErrPathOccupied 这种标识符。
func detailTemplateErrMsg(err error) facing.Msg {
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		return facing.Msg{}
	}
	return facing.Msg{Key: fact.Key, Fallback: fact.Fallback}
}

// errTemplateDepsMissing 详情模板契约未装配时的提示（装配缺陷，页面可见）。
//
// 中文原文**兼作取词兜底**（与 product_bulk_handle.go 的 productBulkText 同形）：词条缺失时页面显示
// 这句话，而不是裸 key（页面上出现 `admin.product_detail_template.depsMissing` 这种串，
// 运营只会以为后台坏了）。
const errTemplateDepsMissing = "详情页模板能力未装配（缺少内容模板或自动发布契约）"

// detailTemplateDepsMissingMsg 「依赖未装配」提示的**事实**（key + 中文兜底）。
// key 在 productenums.ProductDetailTemplateDepsMissing（迁移 407 登记中英词条）。
var detailTemplateDepsMissingMsg = facing.Msg{Key: productenums.ProductDetailTemplateDepsMissing, Fallback: errTemplateDepsMissing}

// 本页的两个参数级提示的中文兜底（key 在 productenums.ProductDetailTemplate*Required）。
const (
	productDetailTemplateNameRequiredFallback = "模板名不能为空"
	productDetailTemplatePathRequiredFallback = "请填写新的访问路径。"
)

// 两个参数级提示的**事实**（模板在渲染期取词，控制器不再拼成品串）。
var (
	productDetailTemplateNameRequiredMsg = facing.Msg{
		Key: productenums.ProductDetailTemplateNameRequired, Fallback: productDetailTemplateNameRequiredFallback,
	}
	productDetailTemplatePathRequiredMsg = facing.Msg{
		Key: productenums.ProductDetailTemplatePathRequired, Fallback: productDetailTemplatePathRequiredFallback,
	}
)

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
