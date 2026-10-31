package adminhttp

import (
	"errors"
	"fmt"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strings"

	adminenums "go_wp/internal/module/admin/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var adminI18nHandleInst = NewAdminI18nEntryHandle()

func adminI18nHandle() *adminI18nEntryHandle { return adminI18nHandleInst }

type adminI18nWriteAction string

const (
	adminI18nActionCreate adminI18nWriteAction = "create"
	adminI18nActionUpdate adminI18nWriteAction = "update"
	adminI18nActionDelete adminI18nWriteAction = "delete"

	adminI18nActionBulk adminI18nWriteAction = "bulkDelete"
)

var adminI18nWriteAllowedKeys = map[adminI18nWriteAction]map[string]bool{
	adminI18nActionCreate: {"action": true, "key": true, "lang": true, "value": true, "category": true, "remark": true},
	adminI18nActionUpdate: {"action": true, "key": true, "lang": true, "value": true, "category": true, "remark": true},
	adminI18nActionDelete: {"action": true, "key": true, "lang": true},
	adminI18nActionBulk:   {"ids": true},
}

type adminI18nWriteReq struct {
	Action   adminI18nWriteAction
	Key      string
	Lang     string
	Value    string
	Category string
	Remark   string
	IDs      []string
}

type adminI18nWriteResult struct {
	Key  string `json:"key"`
	Lang string `json:"lang"`
}

type adminI18nWriteBulkItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

type adminI18nWriteBulkResult struct {
	Deleted int                      `json:"deleted"`
	Skipped int                      `json:"skipped"`
	Items   []adminI18nWriteBulkItem `json:"items"`
}

func adminI18nWriteIsExplicitNull(body json.RawMessage) bool {
	return len(body) > 0 && strings.TrimSpace(string(body)) == "null"
}

func adminI18nWriteStringOf(raw map[string]json.RawMessage, key string) (string, error) {
	body, ok := raw[key]
	if !ok {
		return "", nil
	}
	if adminI18nWriteIsExplicitNull(body) {
		return "", fmt.Errorf("字段 %s 是 null（应省略或给字符串）", key)
	}
	var value string
	if err := json.Unmarshal(body, &value); err != nil {
		return "", fmt.Errorf("字段 %s 不是字符串", key)
	}
	return value, nil
}

func adminI18nWriteIDsOf(raw map[string]json.RawMessage, key string) ([]string, error) {
	body, ok := raw[key]
	if !ok {
		return nil, fmt.Errorf("批量档缺 %s", key)
	}
	if adminI18nWriteIsExplicitNull(body) {
		return nil, fmt.Errorf("字段 %s 是 null（应为字符串数组）", key)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("字段 %s 不是字符串数组", key)
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		if adminI18nWriteIsExplicitNull(item) {
			return nil, fmt.Errorf("字段 %s 第 %d 项是 null", key, i+1)
		}
		var value string
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, fmt.Errorf("字段 %s 第 %d 项不是字符串", key, i+1)
		}
		out = append(out, value)
	}
	return out, nil
}

func adminI18nWriteReqOf(c *gin.Context) (adminI18nWriteReq, bool) {
	rawMap, err := binding.ParseJSON[map[string]json.RawMessage](c)
	if err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	raw := *rawMap

	var req adminI18nWriteReq
	action := ""
	if body, ok := raw["action"]; ok {
		if adminI18nWriteIsExplicitNull(body) {
			adminBindFail(c, errors.New("action 是 null（应省略或给字符串）"))
			return adminI18nWriteReq{}, false
		}
		if err := json.Unmarshal(body, &action); err != nil {
			adminBindFail(c, errors.New("action 不是字符串"))
			return adminI18nWriteReq{}, false
		}
		req.Action = adminI18nWriteAction(action)
	} else {

		req.Action = adminI18nActionBulk
	}

	allowed, ok := adminI18nWriteAllowedKeys[req.Action]
	if !ok {
		adminBindFail(c, fmt.Errorf("未知 action：%q", action))
		return adminI18nWriteReq{}, false
	}
	for key := range raw {
		if !allowed[key] {
			adminBindFail(c, fmt.Errorf("动作 %s 不接受字段 %q", req.Action, key))
			return adminI18nWriteReq{}, false
		}
	}

	if req.Action == adminI18nActionBulk {
		ids, err := adminI18nWriteIDsOf(raw, "ids")
		if err != nil {
			adminBindFail(c, err)
			return adminI18nWriteReq{}, false
		}
		req.IDs = ids
		return req, true
	}

	if req.Key, err = adminI18nWriteStringOf(raw, "key"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	if req.Lang, err = adminI18nWriteStringOf(raw, "lang"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	req.Key, req.Lang = strings.TrimSpace(req.Key), strings.TrimSpace(req.Lang)
	if req.Action == adminI18nActionDelete {
		if missing := adminI18nDeleteMissingMsg(req.Key, req.Lang); missing != "" {
			r.ErrorWithMessage(c, http.StatusBadRequest, missing)
			return adminI18nWriteReq{}, false
		}
		return req, true
	}

	if req.Value, err = adminI18nWriteStringOf(raw, "value"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	if missing := adminI18nSaveMissingMsg(req.Key, req.Lang, req.Value); missing != "" {
		r.ErrorWithMessage(c, http.StatusBadRequest, missing)
		return adminI18nWriteReq{}, false
	}
	if req.Category, err = adminI18nWriteStringOf(raw, "category"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	if req.Remark, err = adminI18nWriteStringOf(raw, "remark"); err != nil {
		adminBindFail(c, err)
		return adminI18nWriteReq{}, false
	}
	return req, true
}

func (h *adminI18nEntryHandle) I18nEntryWrite(c *gin.Context) {
	req, ok := adminI18nWriteReqOf(c)
	if !ok {
		return
	}
	switch req.Action {
	case adminI18nActionCreate, adminI18nActionUpdate:
		h.i18nWriteUpsert(c, req)
	case adminI18nActionDelete:
		h.i18nWriteDelete(c, req)
	case adminI18nActionBulk:
		h.i18nWriteBulkDelete(c, req)
	}
}

func (h *adminI18nEntryHandle) i18nWriteUpsert(c *gin.Context, req adminI18nWriteReq) {
	entry := i18n.Entry{
		Key: req.Key, Lang: req.Lang, Value: req.Value,
		Category: strings.TrimSpace(req.Category), Remark: strings.TrimSpace(req.Remark), Status: 1,
	}
	var err error
	if req.Action == adminI18nActionUpdate {
		err = i18n.UpdateEntry(c.Request.Context(), entry)
	} else {
		err = i18n.SaveEntry(c.Request.Context(), entry)
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {

			r.ErrorWithMessage(c, http.StatusNotFound, adminenums.MsgBadRequest)
			return
		}
		adminFail(c, err)
		return
	}

	h.markI18nStale(c)
	r.Success(c, adminI18nWriteResult{Key: req.Key, Lang: req.Lang})
}

func (h *adminI18nEntryHandle) i18nWriteDelete(c *gin.Context, req adminI18nWriteReq) {
	if err := i18n.DeleteEntry(c.Request.Context(), req.Key, req.Lang); err != nil {
		adminFail(c, err)
		return
	}

	h.markI18nStale(c)
	r.Success(c)
}

func (h *adminI18nEntryHandle) i18nWriteBulkDelete(c *gin.Context, req adminI18nWriteReq) {
	ids, nerr := bulkids.Normalize(req.IDs)
	if nerr != nil {
		r.ErrorWithMessage(c, http.StatusBadRequest, adminI18nBulkTooManyMessage(nerr))
		return
	}
	res := adminI18nWriteBulkResult{Items: []adminI18nWriteBulkItem{}}
	for _, raw := range ids {
		item := adminI18nWriteBulkItem{ID: raw}
		key, lang, ok := strings.Cut(strings.TrimSpace(raw), adminI18nEntryKeySeparator)
		key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
		if !ok || key == "" || lang == "" {

			item.Message = r.TranslateMessage(c, adminenums.ErrI18nKeyEmpty)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
			item.Message = adminErrParam(c, err)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		item.Deleted = true
		res.Deleted++
		res.Items = append(res.Items, item)
	}

	if res.Deleted > 0 {
		h.markI18nStale(c)
	}
	r.Success(c, res)
}

func adminI18nBulkTooManyMessage(err error) string {
	max, count := bulkids.MaxIDs, 0
	var tooMany *bulkids.IDsError
	if errors.As(err, &tooMany) {
		max, count = tooMany.Max, tooMany.Count
	}
	return fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, max, count)
}
