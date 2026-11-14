package contenthttp

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	presentationcontract "go_wp/internal/module/presentation/contract"
	"go_wp/pkg/binding"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	seoscore "go_wp/pkg/seo/scoring"
)

const articleLinkIndexSize = 100

func (h *articlePageHandle) SetArticleLinkLocator(loc presentationcontract.PublishedEntityLocator) {
	h.linkLocator = loc
}

func (h *articlePageHandle) ArticleLinkSuggestions(c *gin.Context) {
	ctx := c.Request.Context()
	form, ok := bindArticleLinkRequest(c)
	if !ok {
		response.Success(c, []seoscore.LinkSuggestion{})
		return
	}
	projectID, ok := h.linkSuggestProjectID(ctx, form.ProjectID)
	if !ok {
		response.Success(c, []seoscore.LinkSuggestion{})
		return
	}
	suggestions := articleLinkSuggestions(ctx, h, projectID, form)
	if suggestions == nil {
		suggestions = []seoscore.LinkSuggestion{}
	}
	response.Success(c, suggestions)
}

// articleLinkSuggestions 是内链建议的取数组合：候选文章 → 已发布路径 → 建议。
//
// 这是查询编排，不是入口：控制器只绑定、调用与写响应（分层判据见 AGENTS.md）。
func articleLinkSuggestions(ctx context.Context, h *articlePageHandle, projectID string,
	form *contentdto.ArticleLinkReq) []seoscore.LinkSuggestion {
	rows, err := h.contents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: articleLinkIndexSize})
	if err != nil {
		return nil
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
		if paths, perr := h.linkLocator.PublishedEntityPaths(ctx, projectID, "article", "", ids); perr == nil {
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

	linked := make(map[string]bool)
	for _, u := range strings.FieldsFunc(form.Linked, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if u = strings.TrimSpace(u); u != "" {
			linked[u] = true
		}
	}
	return seoscore.InternalLinkSuggestions(&seoscore.LinkDocument{
		SelfID: form.ID, FocusKeyword: form.FocusKeyword, LinkedURLs: linked,
	}, cands, 0)
}

// bindArticleLinkRequest 绑定内链建议表单；失败按「没有建议」的既有口径兜底。
func bindArticleLinkRequest(c *gin.Context) (*contentdto.ArticleLinkReq, bool) {
	var form contentdto.ArticleLinkReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		return nil, false
	}
	form.Normalize()
	return &form, true
}

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

type Handle struct {
	svc contentcontract.ContentService

	collections core.CollectionSchemaProvider
}

func NewHandle(svc contentcontract.ContentService) *Handle { return &Handle{svc: svc} }

func (h *Handle) SetCollectionSchemas(p core.CollectionSchemaProvider) { h.collections = p }

func (h *Handle) Create(c *gin.Context) {
	var req contentdto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgCreateSuccess, res)
}

func (h *Handle) Update(c *gin.Context) {
	var req contentdto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgUpdateSuccess, res)
}

func (h *Handle) Get(c *gin.Context) {
	req, err := binding.ParseForm[contentdto.GetReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDetailSuccess, res)
}

func (h *Handle) List(c *gin.Context) {
	req, err := binding.ParseForm[contentdto.ListReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgListSuccess, list)
}

func (h *Handle) Collections(c *gin.Context) {
	if h.collections != nil {
		items, err := h.collections.CollectionSchemas(c.Request.Context())
		if err != nil {
			response.ErrorAuto(c, http.StatusBadRequest, "content", err)
			return
		}
		response.SuccessWithMessage(c, contentenums.MsgCollectionsSuccess,
			gin.H{"collections": collectionSchemasForJSON(c, items)})
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
		gin.H{"collections": collectionSchemasForJSON(c, items)})
}

// collectionSchemasForJSON 交编辑器 JSON 消费方的集合目录。
//
// 这里的 Label 是**给编辑器 JS 的显示名**，不是页面文案：/api/content/collections 是
// 明确 JSON 数据接口（编辑器按集合名渲染候选），没有模板渲染通道。因此按 JSON 出口
// 的取词口（response.MessageOf 同源的事实）构造 Label，而不是在控制器里翻成品句。
func collectionSchemasForJSON(c *gin.Context, items []core.CollectionSchema) []core.CollectionSchema {
	out := make([]core.CollectionSchema, len(items))
	copy(out, items)
	for i := range out {
		if strings.TrimSpace(out[i].LabelKey) == "" {
			continue
		}
		out[i].Label = response.MsgText(c, facing.Msg{
			Key: out[i].LabelKey, Fallback: out[i].Label,
		})
	}
	return out
}

func (h *Handle) Delete(c *gin.Context) {
	req, err := binding.ParseForm[contentdto.DeleteReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDeleteSuccess, nil)
}
