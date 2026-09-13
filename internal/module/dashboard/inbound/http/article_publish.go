package dashboardhttp

// article_publish.go — 文章详情页的发布区块（状态展示 + 首次发布 + 重新发布）。
//
// 文章写完要能上线才算闭环，但上线的两个前提可能在装配里缺失，所以这一块全部
// 按「能力不足就明确说不」来做，不做假承诺：
//
//   - 未注入发布能力（instances == nil）→ 只显示状态位，不渲染任何表单；
//   - 没有 article 类型的内容模板 → **不渲染发布表单**，改给一句可执行的出口。
//     渲染一个必然失败的下拉比不给入口更糟：运营点一次、失败一次，最后怀疑的是系统。
//
// 权限点复用商品详情模板页那两条（/api/presentation/create 与 /api/presentation/rebuild），
// 不新增权限点：发布行为完全相同，只是实体类型不同。

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
)

// 发布区块的提示文案（同样登记在白名单里，因为它们会进 ?ok= / ?err=）。
const (
	articlePublishedText      = "已发布。文章详情页已上线，访问面立即可见。"
	articleRebuiltText        = "已重新发布。原路径的产物已更新。"
	articleNoProjectText      = "请先选择这篇文章属于哪个站点工程。"
	articleNoURLPathText      = "请填写文章详情页的访问路径。"
	articleNoTemplatePickText = "请选择一套文章详情模板。"
	// articleNoTemplateHint 没有 article 模板时的出口说明（不含任何按钮）。
	articleNoTemplateHint = "这个站还没有「文章详情模板」（entityType=article 的内容模板），" +
		"所以现在没有东西可以渲染这篇文章。模板的建立与内容编辑链路目前没有后台入口，" +
		"需要先建一套 article 类型的内容模板，再回来发布。"
)

// articlePublishFacingMessages 发布流程可展示的文案白名单。
//
// 与文章页那份分开：presentation 的 ErrNotFound 指「实例不存在」，content 的同名常量
// 指「文章不存在」—— 同一个 key 在两处含义不同，合并成一张表必然吃掉一边。
var articlePublishFacingMessages = map[string]string{
	presentationenums.ErrInvalidParam:         "发布参数不完整，请检查工程、路径与模板。",
	presentationenums.ErrNotFound:             "这篇文章还没有详情页实例，先发布一次。",
	presentationenums.ErrNoTemplate:           "该类型没有可用的内容模板，先建一套文章详情模板。",
	presentationenums.ErrEntityMissing:        "文章不存在，可能已被删除。",
	presentationenums.ErrBuildFailed:          "构建失败，请检查模板与文章正文后重试。",
	presentationenums.ErrProjectRequired:      "站点里有多个工程，请显式选择这篇所属的工程。",
	presentationenums.ErrProjectNotFound:      "选择的站点工程不存在，请刷新后重试。",
	presentationenums.ErrRegistryMissing:      "实体类型注册表未装配（装配缺陷），请联系管理员。",
	presentationenums.ErrTemplateTypeMismatch: "这套模板不是文章类型的，换一套再试。",
	articlePublishedText:                      articlePublishedText,
	articleRebuiltText:                        articleRebuiltText,
	articleNoProjectText:                      articleNoProjectText,
	articleNoURLPathText:                      articleNoURLPathText,
	articleNoTemplatePickText:                 articleNoTemplatePickText,
}

// ArticlePublish 首次发布文章详情页（POST /admin/articles/publish）。
func (h *articlePageHandle) ArticlePublish(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	urlPath := strings.TrimSpace(c.PostForm("urlPath"))
	templateID := strings.TrimSpace(c.PostForm("templateId"))

	if h.instances == nil {
		articleRedirectEdit(c, id, "", "发布能力未装配，请联系管理员。")
		return
	}
	if projectID == "" {
		articleRedirectEdit(c, id, "", articleNoProjectText)
		return
	}
	if urlPath == "" {
		articleRedirectEdit(c, id, "", articleNoURLPathText)
		return
	}
	if !strings.HasPrefix(urlPath, "/") {
		// 路径必须从根开始：CreateInstance 直接把它写进 URL 占用表，
		// 一个不带前导斜杠的路径会占用一个永远打不开的名额。
		urlPath = "/" + urlPath
	}
	if _, err := h.instances.CreateInstance(c.Request.Context(), &presentationdto.CreateInstanceReq{
		EntityType: articleEntityType, EntityID: id,
		URLPath: urlPath, ProjectID: projectID, TemplateID: templateID,
	}); err != nil {
		articleRedirectEdit(c, id, "", articlePublishFacingError(c, err))
		return
	}
	articleRedirectEdit(c, id, articlePublishedText, "")
}

// ArticleRebuild 重新发布（POST /admin/articles/rebuild）。
//
// 沿用实例当前绑定的模板重建；文章内容本身改了以后由依赖扇出自动标记待重建，
// 这里的按钮是「我想立刻看到结果」的手动出口。
func (h *articlePageHandle) ArticleRebuild(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if h.instances == nil {
		articleRedirectEdit(c, id, "", "发布能力未装配，请联系管理员。")
		return
	}
	if _, err := h.instances.Rebuild(c.Request.Context(), &presentationdto.RebuildReq{EntityID: id}); err != nil {
		articleRedirectEdit(c, id, "", articlePublishFacingError(c, err))
		return
	}
	articleRedirectEdit(c, id, articleRebuiltText, "")
}

// articlePublishView 组装发布区块渲染数据（纯函数，取数在 articlePublishViewData）。
//
// id 为空（新建中）时不查任何东西：还没有实体，发布无从谈起。
func articlePublishView(ctx context.Context, h *articlePageHandle, id, slug string, projectOptions []gin.H) gin.H {
	if strings.TrimSpace(id) == "" {
		return gin.H{
			"PublishConfigured": false,
			"PublishHint":       "先保存这篇文章，再回来看发布状态。",
		}
	}
	if h.instances == nil {
		return gin.H{
			"PublishConfigured": false,
			"PublishHint":       "发布能力未装配（装配缺陷），本页只显示文章内容。",
		}
	}

	out := gin.H{"PublishConfigured": true}
	inst, err := h.instances.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: articleEntityType, EntityID: id,
	})
	if err == nil && inst != nil && strings.TrimSpace(inst.URLPath) != "" {
		out["Published"] = true
		out["URLPath"] = inst.URLPath
		out["PublicURL"] = articlePublicURL(inst.URLPath)
		out["Stale"] = inst.Stale
		out["Status"] = inst.Status
		return out
	}
	out["Published"] = false
	out["DefaultURLPath"] = articleDefaultURLPath(slug)
	// 工程列表由调用方查一次后传进来（同一屏里的发布区块与导入区块都要它）。
	out["Projects"] = projectOptions
	out["Templates"] = articleTemplateOptions(ctx, h)
	out["HasTemplates"] = len(out["Templates"].([]gin.H)) > 0
	out["NoTemplateHint"] = articleNoTemplateHint
	return out
}

// articleProjectOptions 工程下拉（发布时必须落到一个工程：实例表的 project_id 非空）。
func articleProjectOptions(ctx context.Context, h *articlePageHandle) []gin.H {
	out := []gin.H{}
	if h.projects == nil {
		return out
	}
	list, err := h.projects.List(ctx)
	if err != nil {
		return out
	}
	for _, p := range list {
		out = append(out, gin.H{"ID": p.ID, "Name": p.Name})
	}
	return out
}

// articleTemplateOptions 文章类型的内容模板下拉（空表 = 这个站还没有文章详情模板）。
func articleTemplateOptions(ctx context.Context, h *articlePageHandle) []gin.H {
	out := []gin.H{}
	if h.templates == nil {
		return out
	}
	list, err := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: articleEntityType})
	if err != nil {
		return out
	}
	for _, t := range list {
		out = append(out, gin.H{"ID": t.ID, "Name": t.Name, "Version": t.DraftVersion})
	}
	return out
}

// articlePublishFacingError 发布错误 → 可展示文案（先查发布白名单，再查文章白名单）。
func articlePublishFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	if msg, ok := articlePublishFacingMessages[raw]; ok {
		return msg
	}
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		if msg, ok := articlePublishFacingMessages[strings.TrimSpace(raw[:idx])]; ok {
			return msg
		}
	}
	if msg := articleFacingText(raw); msg != "" {
		return msg
	}
	return translateFor(c)(dashboardenums.MsgInternalError, "系统内部错误，请稍后重试")
}

// articlePublishURL 发布 / 重建表单的动作地址（当前页路径，集中一处便于改名）。
const (
	articlePublishPath  = "/admin/articles/publish"
	articleRebuildPath  = "/admin/articles/rebuild"
	articleListPath     = "/admin/articles"
	articleEditPagePath = "/admin/articles/edit"
)
