package producthttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go_wp/internal/adminui"
	contentcontract "go_wp/internal/module/content/contract"
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
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
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
//	· SEO 评分：输入构造、标题索引、算分与冲突清单的组织 →
//	  internal/module/product/service/product_seo_score.go。
//
// 评分规则本体在 internal/seo/scoring：service 那一侧只做**输入**，本文件只做**视图**。

// 编译期断言：service 用例声明的两个窄端口由本包这两个契约面满足 ——
// 端口是「评分输入 / 模板编排真正需要什么」的形状，契约改宽改窄时这里先报错。
var (
	_ productservice.ProductReadPort            = (productcontract.ProductService)(nil)
	_ productservice.DetailTemplateInstancePort = (ProductDetailTemplatePort)(nil)
)

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
	// 顺序与拆分前一致（dd 6.3 ⑤）：**先取文档、再建模板**（取不到复制来源就不建空的）。
	// 两处失败走同一张 contenttemplate 文案表，故 service 只回一个 error。
	if err := productservice.CreateDetailTemplate(c.Request.Context(), h.detailTemplateDeps(),
		projectID, name, copyFrom); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateTemplateMsg(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneMsg)
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
	// 编排（URLPath 为空时先按站点 URL 规则派生路径）在 service，顺序与拆分前一致（dd 6.3 ⑤⑥）：
	// 先取商品拿 slug，取不到就直接报错，不继续建实例。
	// 路径派生口径见 internal/siteurl：这里只是把表单预填好，用户填了就用用户的。
	err := productservice.PublishDetailTemplate(c.Request.Context(), h.detailTemplateDeps(),
		projectID, productID,
		strings.TrimSpace(c.PostForm("templateId")), strings.TrimSpace(c.PostForm("urlPath")))
	if err != nil {
		// 两类失败走**两张不同的文案表**：取商品失败（product 的 enums）与建实例失败
		// （presentation 的 enums）拆分前各自调用 productErrMsg / detailTemplateFacingMsg。
		// 合成一句会把「商品不存在」说成「这个商品还没有详情页实例」—— 用户据此会去发布一次。
		var lookup *productservice.DetailTemplateProductLookupError
		if errors.As(err, &lookup) {
			productDetailTemplateJump(c, false, projectID, productID, productErrMsg(c, lookup.Err))
			return
		}
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
	// 规范化、唯一性检查与冲突行为都在 presentation 侧（service 只透传 projectID / NewPath /
	// WithRedirect）。工程作用域必须由这里传下去：它来自表单里用户选中的工程，是页面上
	// 「当前站点」的唯一来源，presentation 侧的「工程唯一时取该工程」兜底在多工程站点上
	// 会落到另一个工程（改的是别的站点的路径，页面仍回成功）。
	if err := productservice.UpdateDetailTemplateURL(c.Request.Context(), h.instances, projectID, productID, newPath,
		c.PostForm("withRedirect") != ""); err != nil {
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
	// projectID 同 ProductDetailTemplateUpdateURL：工程作用域由页面传入，不走 presentation 的兜底。
	if err := productservice.ApplyDetailTemplate(c.Request.Context(), h.instances, projectID, productID,
		strings.TrimSpace(c.PostForm("templateId"))); err != nil {
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

// requestScoreLang 评分使用的语言（后台 Cookie / Accept-Language，SEO-001）。
func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// SetSeoTitleSources 注入全站标题索引所需的另外两份只读契约（装配期调用，可空）。
//
// 页面与文章是同一个站点的其它页面，做 title 唯一性时必须在同一份索引里 ——
// 只比商品域的话，商品标题与文章标题撞车照样检不出来。两份契约都可空：
// 未注入时索引退化成商品 / 分类 / 品牌三域，评分本身照常（缺依赖不该变成 500）。
func (h *productPageHandle) SetSeoTitleSources(pages pagecontract.PageService, contents contentcontract.ContentService) {
	h.seoPages = pages
	h.seoContents = contents
}

// seoTitleSources 标题索引所需的另外两份只读契约（未注入时是零值 = 索引退化成三域）。
func (h *productPageHandle) seoTitleSources() productservice.SeoTitleExtraSources {
	return productservice.SeoTitleExtraSources{Pages: h.seoPages, Contents: h.seoContents}
}

// scoreInputOf 取评分输入 —— 本包唯一的取数入口（三个 POST 端点与抽屉共用）。
//
// 取数与算分都在 productservice（SeoScoreInputOf）：抽屉与行内按钮各写一份取数口径的下场
// 是「同一个商品在两处分数不同」。这里只做 HTTP 该做的事：拿请求语言、把依赖交进去、
// 把空态的词取好。
//
// in == nil 表示**没取到实体**：可能是数据缺失（没传 id / 库里没有），也可能是读取失败
// （res.Err 非空）。两者的现状出口都是那句空态文案（与拆分前逐字一致），但**绝不是 0 分**；
// 判据与将来换出口（例如读取失败回 500）的位置都在 service 的 SeoScoreInputResult 注释里。
func (h *productPageHandle) scoreInputOf(c *gin.Context, kind scoring.PageKind,
	id, projectID string) (in *scoring.EntityPageInput, selfID string, emptyView productdto.ScoreView) {
	res := productservice.SeoScoreInputOf(c.Request.Context(), h.products, h.instances,
		kind, id, projectID, requestScoreLang(c))
	if res.Input == nil {
		return nil, "", scoreEmptyView(c, res.Empty)
	}
	return res.Input, res.SelfID, productdto.ScoreView{}
}

// scoreEmptyView 空态片段视图：文案是「key + 中文兜底」的事实（service 给出），取值在 HTTP 层。
func scoreEmptyView(c *gin.Context, empty productservice.SeoScoreEmpty) productdto.ScoreView {
	return productdto.ScoreView{Empty: shell.TranslateFor(c)(empty.Key, empty.Fallback)}
}

// ProductScorePanel 商品详情页的编辑期评分（POST /admin/products/seo-score）。
func (h *productPageHandle) ProductScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.scoreInputOf(c, scoring.KindProduct,
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
	in, selfID, emptyView := h.scoreInputOf(c, scoring.KindCategory,
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
	in, selfID, emptyView := h.scoreInputOf(c, scoring.KindBrand,
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
//
// 算分与冲突判定本身都在 productservice（SeoEntityScore / SeoTitleIndex）：
// 这里只把请求侧的取词函数与依赖交进去，再组装片段视图。
func (h *productPageHandle) entityScoreViewOf(ctx context.Context, c *gin.Context,
	in *scoring.EntityPageInput, projectID, selfID string) productdto.ScoreView {
	tr := shell.TranslateFor(c)
	res, dups := productservice.SeoEntityScore(in,
		productservice.SeoTitleIndex(ctx, h.products, h.seoTitleSources(), projectID, tr), selfID)
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
	in, selfID, emptyView := h.scoreInputOf(c, scoring.KindProduct, id, projectID)
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
//
// 实现在 productservice（product_seo_score.go 的 PageDraftTitle），这里留一行委托：本包测试
// 直接调用这个标识符（product_seo_score_test.go），同包改名等于改测试契约。
func pageDraftTitle(doc json.RawMessage) string { return productservice.PageDraftTitle(doc) }

// entityPlainText 商品描述（jsonb）→ 纯文本（评分与前台展示口径一致）。
//
// 同 pageDraftTitle：实现在 productservice.EntityPlainText，本处是给本包测试留的委托。
func entityPlainText(raw json.RawMessage) string { return productservice.EntityPlainText(raw) }
