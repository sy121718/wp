// product_detail_template_handle.go — 商品「详情页模板」选择与预览（issue #14）。
//
// 定位（spec #2「商品展示资产的落点」延伸）：商品详情页是**内容模板（完整文档层）**。
// issue #6 落地了一种类级默认模板（迁移 085），本票把「用哪套模板」变成商品可见的选择：
//
//	· 同一实体类型（product）下可建多套**命名模板**，每套各自版本化
//	  （模板与版本是 contenttemplate 模块的两层：换一套模板换 TemplateID，
//	   改版式产生新版本，本页只做创建与查看；模板 Document 编辑走
//	   /workbench?template={templateId}（contenttemplate 模块，非商品页工作台））；
//	· 商品发布时可指定使用某套模板（首次发布 = create 带 templateId，
//	   已发布 = rebuild 带 templateId 切换绑定）；
//	· 发布前可预览：预览走 presentation.PreviewInstance（只读渲染，不落库不激活），
//	  页面把渲染结果直接输出到新标签页，看到的就是发布时会产出的字节；
//	· 未指定模板的实例仍按实体类型取默认模板（既有行为不变）。
//
// 交互遵循后台规范：GET 渲染完整页，POST 处理完 302 回页面（原生表单 + csrf_token
// 隐藏域），只用 GET/POST；预览表单 target=_blank 打开渲染结果。
package dashboardhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/siteurl"
)

// productEntityType 商品详情模板的实体类型（与 product 模块注册进实体类型注册表的
// 类型名逐字一致；模板与发布实例都以它为键）。
const productEntityType = "product"

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
	GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error)
	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)
	// UpdateURL 改 URL（发布后换路径）：新路径激活 + 旧路径 301 / 取消激活。
	UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error)
}

// SetDetailTemplateDeps 注入「详情页模板」页所需的两份契约（装配期调用）。
//
// 用 setter 而非构造参数：既有装配（含页面测试）按两参数构造商品页 handle，
// 详情模板是增量能力；未注入时页面明确提示，而不是整体不可用。
func (h *productPageHandle) SetDetailTemplateDeps(templates contenttemplatecontract.ContentTemplateService,
	instances ProductDetailTemplatePort) {
	h.templates = templates
	h.instances = instances
}

// ProductDetailTemplatePage GET /admin/products/template：某商品的详情页模板选择与预览页。
func (h *productPageHandle) ProductDetailTemplatePage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		pageError(c, "product_detail_template", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	productID := strings.TrimSpace(c.Query("product"))
	data := gin.H{
		"title": "商品详情页模板", "menu": "products",
		"Projects": projects, "SelectedProject": selected,
		"Err": strings.TrimSpace(c.Query("err")),
	}
	if h.templates == nil || h.instances == nil {
		c.HTML(http.StatusOK, "admin/product_detail_template.html",
			withCSRF(c, withDetailTemplateMissing(data)))
		return
	}
	if productID == "" {
		c.Redirect(http.StatusFound, "/admin/products?project="+selected)
		return
	}
	product, err := h.products.Get(ctx, &productdto.GetReq{ID: productID})
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+selected+"&err="+err.Error())
		return
	}
	// 模板清单（多套命名模板）与当前默认模板：默认模板 = 该类型当前解析到的那套，
	// 实例未显式绑定模板时商品就发布在它上面。
	rows, err := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: productEntityType})
	if err != nil {
		pageError(c, "product_detail_template", err)
		return
	}
	defaultID := ""
	if tpl, rerr := h.templates.ResolveTemplate(ctx, productEntityType); rerr == nil {
		defaultID = tpl.TemplateID
	}
	boundID := ""
	instanceExists := false
	instanceStatus := ""
	instanceURL := ""
	if inst, ierr := h.instances.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: productEntityType, EntityID: productID,
	}); ierr == nil && inst != nil {
		instanceExists, boundID = true, inst.TemplateID
		instanceStatus, instanceURL = inst.Status, inst.URLPath
	}
	templates := make([]gin.H, 0, len(rows))
	for _, t := range rows {
		templates = append(templates, gin.H{
			"ID": t.ID, "Name": t.Name, "DraftVersion": t.DraftVersion,
			"UpdatedAt": t.UpdatedAt,
			"IsBound":   t.ID == boundID,
			"IsDefault": t.ID == defaultID,
		})
	}
	if !instanceExists {
		boundID = defaultID
	}
	data["Product"] = gin.H{
		"ID": product.ID, "Name": product.Name, "Slug": product.Slug,
		// 表单预填的发布路径：按选中工程的 URL 规则派生（用户可改）。
		"URLPath": siteDetailPath(ctx, h.projects, selected, siteurl.KindProduct, product.Slug, product.ID),
	}
	data["Templates"] = templates
	data["TemplateCount"] = len(templates)
	data["BoundTemplateID"] = boundID
	data["DefaultTemplateID"] = defaultID
	data["InstanceExists"] = instanceExists
	data["InstanceStatus"] = instanceStatus
	data["InstanceURL"] = instanceURL
	data["Ready"] = true
	c.HTML(http.StatusOK, "admin/product_detail_template.html", withCSRF(c, data))
}

// withDetailTemplateMissing 未注入模板契约时的页面数据（装配缺陷的可见提示）。
func withDetailTemplateMissing(data gin.H) gin.H {
	data["Ready"] = false
	return data
}

// productDetailPath 商品详情页的默认 URL（与商品 slug 一致；发布实例按它激活静态产物）。
// ProductDetailTemplateCreate POST /admin/products/template/create：
// 新建一套命名模板（复制指定模板或当前默认模板的文档），初始版本 v1。
//
// 不在本页做模板可视化编辑：模板内容编辑走 /workbench?template=…；本页解决的是
// 「同一个商品类型下有多套命名模板可选、各自版本化」的落地与选择。
func (h *productPageHandle) ProductDetailTemplateCreate(c *gin.Context) {
	if h.templates == nil {
		c.Redirect(http.StatusFound, productDetailTemplatePath+"?err="+errTemplateDepsMissing)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	name := strings.TrimSpace(c.PostForm("name"))
	copyFrom := strings.TrimSpace(c.PostForm("copyFrom"))
	if name == "" {
		c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, "模板名不能为空"))
		return
	}
	doc, err := h.templateDocument(c.Request.Context(), projectID, copyFrom)
	if err != nil {
		c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, err.Error()))
		return
	}
	if _, err = h.templates.Create(c.Request.Context(), &contenttemplatedto.CreateReq{
		EntityType: productEntityType, Name: name, DraftDocument: doc, ProjectID: projectID,
	}); err != nil {
		c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, err.Error()))
		return
	}
	c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, ""))
}

// templateDocument 取「复制来源」的模板文档：指定模板优先，缺省取该类型默认模板。
func (h *productPageHandle) templateDocument(ctx context.Context, projectID, templateID string) (doc json.RawMessage, err error) {
	id := strings.TrimSpace(templateID)
	if id == "" {
		tpl, rerr := h.templates.ResolveTemplate(ctx, productEntityType)
		if rerr != nil {
			return nil, rerr
		}
		id = tpl.TemplateID
	}
	res, gerr := h.templates.Get(ctx, &contenttemplatedto.GetReq{ID: id})
	if gerr != nil {
		return nil, gerr
	}
	return res.DraftDocument, nil
}

// ProductDetailTemplatePublish POST /admin/products/template/publish：
// 首次为该商品建立发布实例并发布（可指定模板），激活静态产物。
func (h *productPageHandle) ProductDetailTemplatePublish(c *gin.Context) {
	if h.instances == nil {
		c.Redirect(http.StatusFound, productDetailTemplatePath+"?err="+errTemplateDepsMissing)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	req := &presentationdto.CreateInstanceReq{
		ProjectID: projectID, EntityType: productEntityType, EntityID: productID,
		TemplateID: strings.TrimSpace(c.PostForm("templateId")),
		URLPath:    strings.TrimSpace(c.PostForm("urlPath")),
	}
	if req.URLPath == "" {
		product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ID: productID})
		if err != nil {
			c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, err.Error()))
			return
		}
		// 路径按站点 URL 规则派生（SiteSettings.urlPatterns，未配置则用 siteurl 的默认模式）：
		// 这里只是把表单预填好，用户填了就用用户的 —— 见 internal/siteurl 的三条口径。
		req.URLPath = siteDetailPath(c.Request.Context(), h.projects, projectID,
			siteurl.KindProduct, product.Slug, productID)
	}
	if _, err := h.instances.CreateInstance(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, detailTemplateFacingError(err)))
		return
	}
	c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, ""))
}

// ProductDetailTemplateUpdateURL POST /admin/products/template/url：
// 修改商品详情页的线上路径（改 URL）。
//
// 内容（商品字段、模板绑定、产物内容）完全不动 —— 路径是站点事实而不是内容
// 的一部分：改它的代价是重建产物 + 处置旧链接，不是重新编辑商品。
func (h *productPageHandle) ProductDetailTemplateUpdateURL(c *gin.Context) {
	if h.instances == nil {
		c.Redirect(http.StatusFound, productDetailTemplatePath+"?err="+errTemplateDepsMissing)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	newPath := strings.TrimSpace(c.PostForm("newPath"))
	if newPath == "" {
		c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, "请填写新的访问路径。"))
		return
	}
	if _, err := h.instances.UpdateURL(c.Request.Context(), &presentationdto.UpdateURLReq{
		EntityType:   productEntityType,
		EntityID:     productID,
		NewPath:      newPath,
		WithRedirect: c.PostForm("withRedirect") != "",
	}); err != nil {
		c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, detailTemplateFacingError(err)))
		return
	}
	c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, ""))
}

// ProductDetailTemplateApply POST /admin/products/template/apply：
// 把商品切换（或确认）到选中的模板并重新发布 —— 产物随模板变化（验收 4）。
func (h *productPageHandle) ProductDetailTemplateApply(c *gin.Context) {
	if h.instances == nil {
		c.Redirect(http.StatusFound, productDetailTemplatePath+"?err="+errTemplateDepsMissing)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if _, err := h.instances.Rebuild(c.Request.Context(), &presentationdto.RebuildReq{
		EntityID: productID, TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	}); err != nil {
		c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, detailTemplateFacingError(err)))
		return
	}
	c.Redirect(http.StatusFound, h.detailTemplateBackURL(projectID, productID, ""))
}

// ProductDetailTemplatePreview POST /admin/products/template/preview：
// 发布前预览模板渲染效果 —— 只读渲染，直接把构建结果的 HTML 输出到新标签页；
// 响应头带上实际使用的模板与版本，便于核对「预览的是哪一套」。
func (h *productPageHandle) ProductDetailTemplatePreview(c *gin.Context) {
	if h.instances == nil {
		c.String(http.StatusBadRequest, "%s", errTemplateDepsMissing)
		return
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationdto.PreviewInstanceReq{
		ProjectID: c.PostForm("projectId"), EntityType: productEntityType,
		EntityID: c.PostForm("productId"), TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	})
	if err != nil {
		// 「预览失败」对作者是可用信息，原因（模板不存在 / 文档非法 / 渲染出错）只进日志。
		pageErrorBadRequest(c, "product_detail_template", err)
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

// detailTemplateFacingError 把 presentation 的错误翻译成可展示文案。
// 拿不到映射时原样返回：宁可显示原始错误，也不要吞掉一个没见过的失败原因。
func detailTemplateFacingError(err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	if msg, ok := detailTemplateFacingMessages[raw]; ok {
		return msg
	}
	// service 会用 fmt.Errorf("%s: %w", enumsKey, err) 包装，取冒号前的 key 再查一次。
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		if msg, ok := detailTemplateFacingMessages[strings.TrimSpace(raw[:idx])]; ok {
			return msg
		}
	}
	return raw
}

// detailTemplateBackURL 详情页模板页的回跳地址（带工程与商品，错误经查询串回显）。
func (h *productPageHandle) detailTemplateBackURL(projectID, productID, errMsg string) string {
	url := productDetailTemplatePath + "?project=" + projectID + "&product=" + productID
	if strings.TrimSpace(errMsg) != "" {
		url += "&err=" + errMsg
	}
	return url
}

// errTemplateDepsMissing 详情模板契约未装配时的提示（装配缺陷，页面可见）。
const errTemplateDepsMissing = "详情页模板能力未装配（缺少内容模板或自动发布契约）"
