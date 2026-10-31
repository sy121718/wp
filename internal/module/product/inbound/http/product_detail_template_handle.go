package producthttp

import (
	"context"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	ctoutbound "go_wp/internal/module/product/outbound/contenttemplate"
	dtoutbound "go_wp/internal/module/product/outbound/detailtemplate"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 详情模板写接口只做绑定、调用商品用例与统一 JSON 响应。
var productActionDoneMsg = productenums.ActionDoneMessage()

var (
	_ productservice.ProductReadPort            = (productcontract.ProductService)(nil)
	_ productservice.DetailTemplateInstancePort = (ProductDetailTemplatePort)(nil)
)

// SetDetailTemplateWriteDeps 为当前处理器注入详情模板依赖；只在路由装配期调用。
func (h *ProductHandle) SetDetailTemplateWriteDeps(deps productservice.DetailTemplateDeps) {
	h.templateDeps = deps
}

// SetDetailTemplateModePort 为当前处理器注入双轨能力；不同应用实例互不覆盖。
func (h *ProductHandle) SetDetailTemplateModePort(port presentationcontract.DetailTemplateModePort) {
	h.modePort = port
}

func (h *ProductHandle) ReapplyDetailTemplatePreset(c *gin.Context) {
	req, ok := binding.JSON[productdto.DetailTemplateModeReapplyReq](c)
	if !ok {
		return
	}
	detailTemplateFinish(c, productservice.ReapplyDetailTemplatePreset(c.Request.Context(), h.modePort, req))
}

func (h *ProductHandle) RollbackDetailTemplateDocument(c *gin.Context) {
	req, ok := binding.JSON[productdto.DetailTemplateModeRollbackReq](c)
	if !ok {
		return
	}
	detailTemplateFinish(c, productservice.RollbackDetailTemplateDocument(c.Request.Context(), h.modePort, req))
}

func detailTemplateFinish(c *gin.Context, err error) {
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.SuccessMsg(c, productActionDoneMsg)
}

func (h *ProductHandle) CreateDetailTemplate(c *gin.Context) {
	req, ok := binding.JSON[productdto.DetailTemplateCreateReq](c)
	if !ok {
		return
	}
	detailTemplateFinish(c, productservice.CreateDetailTemplate(c.Request.Context(), h.templateDeps, req.ProjectID, req.Name, req.CopyFrom))
}

func (h *ProductHandle) PublishDetailTemplate(c *gin.Context) {
	req, ok := binding.JSON[productdto.DetailTemplatePublishReq](c)
	if !ok {
		return
	}
	detailTemplateFinish(c, productservice.PublishDetailTemplate(c.Request.Context(), h.templateDeps, req.ProjectID, req.ProductID, req.TemplateID, req.URLPath))
}

func (h *ProductHandle) ApplyDetailTemplate(c *gin.Context) {
	req, ok := binding.JSON[productdto.DetailTemplateApplyReq](c)
	if !ok {
		return
	}
	detailTemplateFinish(c, productservice.ApplyDetailTemplate(c.Request.Context(), h.templateDeps.Instances, req.ProjectID, req.ProductID, req.TemplateID))
}

func (h *ProductHandle) UpdateDetailTemplateURL(c *gin.Context) {
	req, ok := binding.JSON[productdto.DetailTemplateUpdateURLReq](c)
	if !ok {
		return
	}
	detailTemplateFinish(c, productservice.UpdateDetailTemplateURL(c.Request.Context(), h.templateDeps.Instances, req.ProjectID, req.ProductID, req.NewPath, req.WithRedirect))
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

// ProductDetailTemplatePage 只命名传递现成商品、模板目录与发布实例。
func (h *ProductPageHandle) ProductDetailTemplatePage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductDetailTemplatePageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ID: req.ProductID, ProjectID: scope.ProjectID})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	candidates, err := h.reader.QueryDetailTemplates(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	defaultID, err := h.reader.QueryDefaultDetailTemplate(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	bound, err := h.reader.QueryBoundDetailTemplate(c.Request.Context(), scope.ProjectID, product.ID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	path := h.reader.ProductDetailPath(c.Request.Context(), scope.ProjectID, product)
	pageview.RenderTemplate(c, ProductDetailTemplatePageTemplate, pageview.Objects(
		pageview.Object("Scope", scope), pageview.Object("Product", product),
		pageview.Object("Templates", candidates), pageview.Object("DefaultTemplateID", defaultID),
		pageview.Object("Bound", bound), pageview.Object("DetailPath", path),
		pageview.Object("Available", h.templates != nil && h.instances != nil),
	))
}

// ProductDetailTemplatePreview 成功传递构建产物字节，失败由 JSON 出口返回声明处状态。
func (h *ProductPageHandle) ProductDetailTemplatePreview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, ok := binding.Form[productdto.DetailTemplatePreviewReq](c)
	if !ok {
		return
	}
	res, err := productservice.PreviewProductDetailTemplate(c.Request.Context(), h.products, h.instances, req.ProjectID, req.ProductID, req.TemplateID)
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

const MsgProductsTitle = "MsgProductsTitle"
