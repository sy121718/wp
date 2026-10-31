package navigationhttp

import (
	"bytes"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

var navigationDeleteSingleKeys = map[string]bool{"id": true}

var navigationDeleteBulkKeys = map[string]bool{"ids": true}

type navigationBulkDeleteItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

type navigationBulkDeleteResult struct {
	Deleted int                        `json:"deleted"`
	Skipped int                        `json:"skipped"`
	Items   []navigationBulkDeleteItem `json:"items"`
}

func navigationDeleteHasKey(raw map[string]json.RawMessage, key string) bool {
	_, ok := raw[key]
	return ok
}

func navigationDeleteShapeOK(raw map[string]json.RawMessage, allow map[string]bool) bool {
	for key := range raw {
		if !allow[key] {
			return false
		}
	}
	return true
}

func navigationDeleteBulkIDsOf(raw json.RawMessage) ([]string, bool) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
			return nil, false
		}
		var text string
		if err := json.Unmarshal(item, &text); err != nil {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

func navigationBulkTooManyMessage(err error) string {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		return bulkids.MsgTooMany
	}
	return fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, tooMany.Max, tooMany.Count)
}

func (h *Handle) deleteBulk(c *gin.Context, raw map[string]json.RawMessage) {
	params, ok := navigationDeleteBulkIDsOf(raw["ids"])
	if !ok {
		response.ParamError(c)
		return
	}
	ids, nerr := bulkids.Normalize(params)
	if nerr != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, navigationBulkTooManyMessage(nerr))
		return
	}
	res := navigationBulkDeleteResult{Items: []navigationBulkDeleteItem{}}
	for _, id := range ids {
		item := navigationBulkDeleteItem{ID: id}
		if derr := h.svc.Delete(c.Request.Context(), &navigationdto.DeleteReq{ID: id}); derr != nil {
			logger.Scene("navigation").With("id", id).Error(derr, "批量删除导航项失败")
			item.Message = navigationReasonText(c, derr)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		item.Deleted = true
		res.Deleted++
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}
