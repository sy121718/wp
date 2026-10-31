package mailhttp

import (
	"errors"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/response"
)

func mailContactOperator(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

var (
	mailContactSaveKeys   = map[string]bool{"id": true, "email": true, "name": true, "tags": true, "source": true, "consent_source": true, "status": true}
	mailContactDeleteKeys = map[string]bool{"id": true, "ids": true}
	mailContactTagKeys    = map[string]bool{"ids": true, "add": true, "remove": true}
)

type mailContactWriteResult struct {
	ID uint64 `json:"id"`
}

type mailContactBulkItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

type mailContactBulkResult struct {
	Deleted int                   `json:"deleted"`
	Skipped int                   `json:"skipped"`
	Items   []mailContactBulkItem `json:"items"`
}

type mailContactTagItem struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated"`
	Message string `json:"message,omitempty"`
}

type mailContactTagResult struct {
	Updated int                  `json:"updated"`
	Skipped int                  `json:"skipped"`
	Items   []mailContactTagItem `json:"items"`
}

func mailContactIsExplicitNull(body json.RawMessage) bool {
	return len(body) > 0 && strings.TrimSpace(string(body)) == "null"
}

func mailContactBody(c *gin.Context, allowed map[string]bool) (map[string]json.RawMessage, bool) {
	rawMap, err := binding.ParseJSON[map[string]json.RawMessage](c)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return nil, false
	}
	raw := *rawMap
	for key, body := range raw {
		if !allowed[key] || mailContactIsExplicitNull(body) {
			response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
			return nil, false
		}
	}
	return raw, true
}

func mailContactStringOf(raw map[string]json.RawMessage, key string) (string, error) {
	body, ok := raw[key]
	if !ok {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(body, &value); err != nil {
		return "", errors.New(key + " 不是字符串")
	}
	return value, nil
}

func mailContactUintOf(raw map[string]json.RawMessage, key string) (uint64, error) {
	body, ok := raw[key]
	if !ok {
		return 0, nil
	}

	text := strings.TrimSpace(string(body))
	if text == "" || text[0] < '0' || text[0] > '9' {
		return 0, errors.New(key + " 不是非负整数")
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, errors.New(key + " 不是非负整数")
	}
	return value, nil
}

func mailContactIDsOf(raw map[string]json.RawMessage) ([]string, error) {
	body, ok := raw["ids"]
	if !ok {
		return nil, errors.New("缺 ids")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, errors.New("ids 不是数组")
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		if mailContactIsExplicitNull(item) {
			return nil, errors.New("ids 第 " + strconv.Itoa(i+1) + " 项是 null")
		}
		var value string
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, errors.New("ids 第 " + strconv.Itoa(i+1) + " 项不是字符串")
		}
		out = append(out, value)
	}
	return out, nil
}

func mailContactNormalizeIDs(c *gin.Context, raw map[string]json.RawMessage) ([]string, bool) {
	ids, err := mailContactIDsOf(raw)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return nil, false
	}
	normalized, nerr := bulkids.Normalize(ids)
	if nerr != nil {

		response.ErrorWithMessage(c, http.StatusBadRequest, mailBulkIDsText(c, nerr))
		return nil, false
	}
	return normalized, true
}

func (h *Handle) ContactSave(c *gin.Context) {
	raw, ok := mailContactBody(c, mailContactSaveKeys)
	if !ok {
		return
	}
	id, err := mailContactUintOf(raw, "id")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	email, err := mailContactStringOf(raw, "email")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	name, err := mailContactStringOf(raw, "name")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	tags, err := mailContactStringOf(raw, "tags")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	source, err := mailContactStringOf(raw, "source")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	consentSource, err := mailContactStringOf(raw, "consent_source")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	status, err := mailContactStringOf(raw, "status")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}

	req := &maildto.SaveContactReq{
		ID:            id,
		Email:         email,
		Name:          name,
		Tags:          splitTagInput(tags),
		Source:        source,
		ConsentSource: consentSource,
		Status:        status,
		OperatorID:    mailContactOperator(c),
	}
	if req.ID > 0 {
		if err := h.svc.UpdateContact(c.Request.Context(), req); err != nil {
			response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
			return
		}
		response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, mailContactWriteResult{ID: req.ID})
		return
	}
	created, err := h.svc.CreateContact(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, mailContactWriteResult{ID: created})
}

func (h *Handle) ContactDelete(c *gin.Context) {
	raw, ok := mailContactBody(c, mailContactDeleteKeys)
	if !ok {
		return
	}
	_, hasIDs := raw["ids"]
	_, hasID := raw["id"]
	if hasIDs == hasID {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	operator := mailContactOperator(c)

	if hasID {
		id, err := mailContactUintOf(raw, "id")
		if err != nil || id == 0 {
			response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
			return
		}
		deleted, err := h.svc.DeleteContacts(c.Request.Context(), &maildto.DeleteContactsReq{
			IDs: []uint64{id}, OperatorID: operator,
		})
		if err != nil {
			response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
			return
		}
		if deleted == 0 {

			response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrContactNotFound)
			return
		}
		response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
		return
	}

	ids, ok := mailContactNormalizeIDs(c, raw)
	if !ok {
		return
	}
	res := mailContactBulkResult{Items: make([]mailContactBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := mailContactBulkItem{ID: rawID}
		contactID, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || contactID == 0:
			item.Message = response.TranslateMessage(c, mailenums.ErrInvalidParam)
		default:
			deleted, derr := h.svc.DeleteContacts(ctx, &maildto.DeleteContactsReq{
				IDs: []uint64{contactID}, OperatorID: operator,
			})
			switch {
			case derr != nil:
				item.Message = mailErrPageText(c, derr)
			case deleted == 0:

				item.Message = response.TranslateMessage(c, mailenums.ErrContactNotFound)
			default:
				item.Deleted = true
			}
		}
		if item.Deleted {
			res.Deleted++
		} else {
			res.Skipped++
		}
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}

func (h *Handle) ContactTag(c *gin.Context) {
	raw, ok := mailContactBody(c, mailContactTagKeys)
	if !ok {
		return
	}
	add, err := mailContactStringOf(raw, "add")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	remove, err := mailContactStringOf(raw, "remove")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	ids, ok := mailContactNormalizeIDs(c, raw)
	if !ok {
		return
	}
	addTags, removeTags := splitTagInput(add), splitTagInput(remove)
	if len(addTags) == 0 && len(removeTags) == 0 {

		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrContactTagEmpty)
		return
	}
	operator := mailContactOperator(c)
	res := mailContactTagResult{Items: make([]mailContactTagItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		contactID, perr := strconv.ParseUint(rawID, 10, 64)
		if perr != nil || contactID == 0 {
			res.Skipped++
			res.Items = append(res.Items, mailContactTagItem{
				ID: rawID, Message: response.TranslateMessage(c, mailenums.ErrInvalidParam),
			})
			continue
		}
		changed, skipped, terr := h.svc.TagContacts(ctx, &maildto.TagContactsReq{
			IDs:        []uint64{contactID},
			Add:        addTags,
			Remove:     removeTags,
			OperatorID: operator,
		})
		switch {
		case terr != nil:
			res.Skipped++
			res.Items = append(res.Items, mailContactTagItem{ID: rawID, Message: mailErrPageText(c, terr)})
		case skipped > 0 || changed == 0:

			res.Skipped++
			res.Items = append(res.Items, mailContactTagItem{
				ID: rawID, Message: response.TranslateMessage(c, mailenums.ErrContactNotFound),
			})
		default:
			res.Updated++
			res.Items = append(res.Items, mailContactTagItem{ID: rawID, Updated: true})
		}
	}
	response.Success(c, res)
}
