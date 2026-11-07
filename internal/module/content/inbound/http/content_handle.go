package contenthttp

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

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
	projectID, ok := h.linkSuggestProjectID(ctx, strings.TrimSpace(c.PostForm("projectId")))
	if !ok {
		response.Success(c, []seoscore.LinkSuggestion{})
		return
	}

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

	suggestions := seoscore.InternalLinkSuggestions(doc, cands, 0)
	if suggestions == nil {
		suggestions = []seoscore.LinkSuggestion{}
	}
	response.Success(c, suggestions)
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
	req, ok := binding.JSON[contentdto.CreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgCreateSuccess, res)
}

func (h *Handle) Update(c *gin.Context) {
	req, ok := binding.JSON[contentdto.UpdateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgUpdateSuccess, res)
}

func (h *Handle) Get(c *gin.Context) {
	req, ok := binding.Form[contentdto.GetReq](c)
	if !ok {
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
	req, ok := binding.Form[contentdto.ListReq](c)
	if !ok {
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
	req, ok := binding.Form[contentdto.DeleteReq](c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDeleteSuccess, nil)
}
