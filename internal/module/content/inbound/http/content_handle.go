package contenthttp

// 定位与评分侧栏一致：**只读计算**，不写库、不写产物，不叠加 Casbin 权限点
// （页面组已有 Session + CSRF）。本文件只做数据装配：候选从哪来、按什么过滤，
// 全部交给 scoring.InternalLinkSuggestions 纯函数 —— 这里不重复实现过滤/排序逻辑。
//
// 端点：POST /admin/articles/link-suggestions（JSON 返回，前端点击建议即可拿到
// 可插入的标题 + URL）。
//
// 候选来源与「为什么不可能产生死链」：
//
//   - 候选 = presentation 自动发布实例（entityType=article）中**已上线**的文章。
//     判据是 PublishedEntityLocator.PublishedEntityPaths：它只返回 active 指针
//     非空的实例的最终访问路径（含语言前缀），未发布 / 已删除 / 查不到的实体
//     根本不会出现在结果里 —— 没有「有路径但页面不存在」的中间态。
//   - 路径是访问面真实服务的线上路径（实例发布时激活、删除时清理），不是编辑期
//     拼出来的近似值；文章改 URL 会同步更新 url_path，因此推荐出去的路径就是
//     访客点开能到的地址。
//   - 手工页面（page 模块）的已发布路径目前没有只读端口可从 dashboard 拿
//     （pagecontract 只有 ListDrafts），本端点候选域先收敛到文章域；页面域接入
//     需要在 page 契约新增收窄只读方法，属另一个改动。

// Package contenthttp content 模块 HTTP 入口（0-A2）。

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/content/dto"
	"go_wp/internal/module/content/enums"
	"go_wp/internal/module/presentation/contract"
	seoscore "go_wp/internal/seo/scoring"
	"go_wp/internal/shell"
	"go_wp/pkg/response"
)

// articleLinkIndexSize 内链候选的文章取样上限（与标题索引同一量级：编辑期要的是点一下就出结果）。
const articleLinkIndexSize = 100

// SetArticleLinkLocator 注入「实体 → 已上线路径」解析端口（装配期调用；可空）。
//
// 未注入时本能力整体降级：接口返回空建议列表而不是 500 —— 缺依赖不该让评分侧栏不可用。
func (h *articlePageHandle) SetArticleLinkLocator(loc presentationcontract.PublishedEntityLocator) {
	h.linkLocator = loc
}

// ArticleLinkSuggestions 内链建议（POST /admin/articles/link-suggestions）。
//
// 表单：id（当前文章）、projectId（可空，空则取唯一工程）、focusKeyword（当前
// 未保存的焦点关键词）、linked（正文已有内链目标，换行分隔 —— 已链接的不重复推荐）。
func (h *articlePageHandle) ArticleLinkSuggestions(c *gin.Context) {
	ctx := c.Request.Context()
	projectID, ok := h.linkSuggestProjectID(ctx, strings.TrimSpace(c.PostForm("projectId")))
	if !ok {
		response.Success(c, []seoscore.LinkSuggestion{})
		return
	}

	// 候选集：全部文章 → 已上线解析 → 只留有真实路径的。解析一次批量调，
	// 不逐篇查实例（评分侧栏要的是一次点击出结果）。
	rows, err := h.contents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: articleLinkIndexSize})
	if err != nil {
		response.Success(c, []seoscore.LinkSuggestion{})
		return
	}
	ids := make([]string, 0, len(rows))
	rowByd := make(map[string]*contentdto.ContentResp, len(rows))
	for _, r := range rows {
		if r == nil || r.ID == "" {
			continue
		}
		ids = append(ids, r.ID)
		rowByd[r.ID] = r
	}
	cands := make([]seoscore.LinkCandidate, 0, len(ids))
	if h.linkLocator != nil && len(ids) > 0 {
		paths, perr := h.linkLocator.PublishedEntityPaths(ctx, projectID, "article", "", ids)
		if perr == nil {
			for id, path := range paths {
				r := rowByd[id]
				if r == nil {
					continue
				}
				cands = append(cands, seoscore.LinkCandidate{
					ID:           id,
					Title:        firstNonEmptyString(contentText(r.Data, "title"), r.Slug),
					URL:          path,
					FocusKeyword: contentText(r.Data, "focusKeyword"),
				})
			}
		}
	}

	// 当前文档上下文：焦点关键词来自未保存的表单（编辑者刚改完还没保存也要能出建议）；
	// 已链接集合由前端把正文里现存的链接目标回传，入口只负责拆成集合。
	linked := make(map[string]bool)
	for _, u := range strings.FieldsFunc(c.PostForm("linked"), func(r rune) bool { return r == '\n' || r == '\r' }) {
		if u = strings.TrimSpace(u); u != "" {
			linked[u] = true
		}
	}
	doc := &seoscore.LinkDocument{
		SelfID:       strings.TrimSpace(c.PostForm("id")),
		FocusKeyword: strings.TrimSpace(c.PostForm("focusKeyword")),
		LinkedURLs:   linked,
	}
	// 文章域没有分类 / 标签字段（content 字段白名单如此），重叠度按焦点关键词计算；
	// Tags / Categories 留空是事实陈述而不是遗漏，纯函数的标签 / 分类权重为它们保留。
	suggestions := seoscore.InternalLinkSuggestions(doc, cands, 0)
	if suggestions == nil {
		suggestions = []seoscore.LinkSuggestion{}
	}
	response.Success(c, suggestions)
}

// linkSuggestProjectID 解析建议所属工程：表单优先，缺省回落唯一工程。
//
// 文章实体本身不带工程归属（contents 无 project_id），而已上线路径按工程解析 ——
// 多工程部署时前端必须带 projectId；单工程部署缺省取那一个，编辑侧不用关心。
// 解析不出（多工程且未指定）返回 false，端点降级为空建议而不是猜一个工程。
func (h *articlePageHandle) linkSuggestProjectID(ctx context.Context, formProjectID string) (string, bool) {
	if formProjectID != "" {
		return formProjectID, true
	}
	if h.projects == nil {
		return "", false
	}
	list, err := h.projects.List(ctx)
	if err != nil || len(list) != 1 {
		return "", false
	}
	return list[0].ID, true
}

// Handle content 接口处理器。
type Handle struct {
	svc contentcontract.ContentService
	// collections 集合源元数据聚合端口（装配期注入的集合源注册表，可空）。
	//
	// 集合源不再只属于内容模块（商品也是集合源，issue #9）：本接口是工作台与
	// 内置组件拿集合源元数据的唯一入口，必须返回全量集合源而不是本模块那几条。
	collections core.CollectionSchemaProvider
}

// NewHandle 构造。
func NewHandle(svc contentcontract.ContentService) *Handle { return &Handle{svc: svc} }

// SetCollectionSchemas 注入集合源元数据聚合端口（装配期调用）。
// 传入 nil 时退回「只报告本模块的集合源」（单模块场景）。
func (h *Handle) SetCollectionSchemas(p core.CollectionSchemaProvider) { h.collections = p }

// Create 新建内容。
func (h *Handle) Create(c *gin.Context) {
	req := &contentdto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgCreateSuccess, res)
}

// Update 更新内容。
func (h *Handle) Update(c *gin.Context) {
	req := &contentdto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgUpdateSuccess, res)
}

// Get 内容详情。
func (h *Handle) Get(c *gin.Context) {
	req := &contentdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDetailSuccess, res)
}

// List 内容列表。
func (h *Handle) List(c *gin.Context) {
	req := &contentdto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgListSuccess, list)
}

// Collections 集合源元数据（字段白名单 + 过滤维度 + 排序键）：
// 内置组件构建期校验与工作台字段下拉的唯一来源。
//
// 优先走装配期注入的集合源注册表（全量集合源，含商品等其它领域模块）；
// 未注入时退回本模块服务（只有内容集合源）。
func (h *Handle) Collections(c *gin.Context) {
	if h.collections != nil {
		items, err := h.collections.CollectionSchemas(c.Request.Context())
		if err != nil {
			response.ErrorAuto(c, http.StatusBadRequest, "content", err)
			return
		}
		response.SuccessWithMessage(c, contentenums.MsgCollectionsSuccess,
			gin.H{"collections": localizeCollectionLabels(c, items)})
		return
	}
	provider, ok := h.svc.(core.CollectionSchemaProvider)
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrCollectionUnsupported)
		return
	}
	items, err := provider.CollectionSchemas(c.Request.Context())
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgCollectionsSuccess,
		gin.H{"collections": localizeCollectionLabels(c, items)})
}

// localizeCollectionLabels 集合源展示名取词（LabelKey 为空 = Label 已是终值）。
//
// 为什么取词只在出口：service 拿不到请求语言，而**构建期**那条路径（组件按 schema
// 校验字段白名单）只看 Source / Fields，从不读 Label —— 在那里翻译既做不到也没必要。
// 复制一份再改，不动 provider 返回的底层数组（避免将来某个 provider 缓存了切片时被就地改写）。
func localizeCollectionLabels(c *gin.Context, items []core.CollectionSchema) []core.CollectionSchema {
	out := make([]core.CollectionSchema, len(items))
	copy(out, items)
	tr := shell.TranslateFor(c)
	for i := range out {
		if strings.TrimSpace(out[i].LabelKey) == "" {
			continue
		}
		out[i].Label = tr(out[i].LabelKey, out[i].Label)
	}
	return out
}

// Delete 删除内容。
func (h *Handle) Delete(c *gin.Context) {
	req := &contentdto.DeleteReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDeleteSuccess, nil)
}
