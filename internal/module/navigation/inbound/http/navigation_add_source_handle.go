package navigationhttp

import (
	"bytes"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/logger"
	pageview "go_wp/pkg/view"
)

const navigationAddSourceAction = "addSource"

var navigationAddSourceKeys = map[string]bool{
	"action": true, "projectId": true, "kind": true, "sourceType": true, "ids": true,
}

const navigationAddSourceNoPathKey = navigationenums.NoPublicPath

type navigationAddSourceItem struct {
	ID      string `json:"id"`
	Added   bool   `json:"added"`
	Message string `json:"message,omitempty"`
}

type navigationAddSourceResult struct {
	Added   int                       `json:"added"`
	Skipped int                       `json:"skipped"`
	Items   []navigationAddSourceItem `json:"items"`
}

func navigationAddSourceOptionalString(raw map[string]json.RawMessage, key string) (string, bool) {
	value, exists := raw[key]
	if !exists {
		return "", true
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	return strings.TrimSpace(text), true
}

// CreateFromSource 是「从页面/内容等来源批量加入菜单」的独立端点：
// 一个端点一种确定协议，输入是标准表单（projectId / kind / sourceType / ids）。
func (h *Handle) CreateFromSource(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// 请求体上限：批量 ids 可能很长，超限整批拒绝而不是部分执行。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, navigationWriteMaxBodyBytes)
	var form navigationdto.AddSourceForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	projectID := strings.TrimSpace(form.ProjectID)
	sourceType := strings.TrimSpace(form.SourceType)
	kind := normalizeNavKind(form.Kind)
	ids, nerr := bulkids.Normalize(form.IDs)
	if nerr != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, nerr)
		return
	}
	if len(ids) == 0 {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}

	ctx := c.Request.Context()
	groups, gerr := h.svc.SourceGroups(ctx, projectID)
	if gerr != nil {
		logger.Scene(navigationErrScene).With("project", projectID).Error(gerr, "加载导航来源候选失败")
		pageview.FailScene(c, navigationErrScene, gerr)
		return
	}

	candidates := make(map[string]navigationcontract.SourceCandidate, len(ids))
	for _, g := range groups {
		if g.Type != sourceType {
			continue
		}
		for _, it := range g.Items {
			candidates[it.ID] = it
		}
	}

	res := navigationAddSourceResult{Items: []navigationAddSourceItem{}}
	for _, id := range ids {
		item := navigationAddSourceItem{ID: id}
		candidate, found := candidates[id]
		if !found {

			item.Message = navigationReasonKeyText(c, navigationenums.ErrNotFound)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		if candidate.URL == "" {
			item.Message = navigationReasonKeyText(c, navigationAddSourceNoPathKey)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		title := candidate.Title
		if title == "" {
			title = candidate.Label
		}
		sourceID := candidate.ID
		if _, cerr := h.svc.Create(ctx, &navigationdto.CreateReq{
			ProjectID: projectID, Title: title, Path: candidate.URL, Kind: kind,
			SourceType: sourceType, SourceID: &sourceID,
		}); cerr != nil {

			logger.Scene(navigationErrScene).With("sourceType", sourceType).With("sourceId", id).
				Error(cerr, "来源实体加入菜单失败")
			item.Message = navigationReasonText(c, cerr)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		item.Added = true
		res.Added++
		res.Items = append(res.Items, item)
	}
	logger.Scene(navigationErrScene).With("project", projectID).With("kind", kind).
		With("sourceType", sourceType).With("added", res.Added).With("skipped", res.Skipped).
		Info("来源实体加入导航菜单")
	c.HTML(http.StatusOK, "admin/navigation/navigation_add_source_result.html", pageview.Objects(
		pageview.Object("Added", res.Added),
		pageview.Object("Skipped", res.Skipped),
		pageview.Object("Items", res.Items),
	))
}
