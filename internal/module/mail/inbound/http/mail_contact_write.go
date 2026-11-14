package mailhttp

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/binding"
	bulkids "go_wp/pkg/bulkids"
	pageview "go_wp/pkg/view"
)

func mailContactOperator(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

func mailContactUintOfText(text string) (uint64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	if text[0] < '0' || text[0] > '9' {
		return 0, errors.New("id 不是非负整数")
	}
	return strconv.ParseUint(text, 10, 64)
}

func (h *Handle) ContactSave(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[maildto.MailContactSaveForm](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	id, err := mailContactUintOfText(form.ID)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	req := &maildto.SaveContactReq{
		ID:            id,
		Email:         form.Email,
		Name:          form.Name,
		Tags:          splitTagInput(form.Tags),
		Source:        form.Source,
		ConsentSource: form.ConsentSource,
		Status:        form.Status,
		OperatorID:    mailContactOperator(c),
	}
	if req.ID > 0 {
		if err := h.svc.UpdateContact(c.Request.Context(), req); err != nil {
			pageview.FailScene(c, mailErrScene, err)
			return
		}
	} else if _, err := h.svc.CreateContact(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *Handle) ContactDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[maildto.MailContactDeleteForm](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	hasIDs := len(form.IDs) > 0
	id := mailIDOfText(form.ID)
	hasID := strings.TrimSpace(form.ID) != ""
	if hasIDs == hasID {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	operator := mailContactOperator(c)

	if hasID {
		if id == 0 {
			pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
			return
		}
		deleted, err := h.svc.DeleteContacts(c.Request.Context(), &maildto.DeleteContactsReq{
			IDs: []uint64{id}, OperatorID: operator,
		})
		if err != nil {
			pageview.FailScene(c, mailErrScene, err)
			return
		}
		if deleted == 0 {
			pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrContactNotFound))
			return
		}
		pageview.Success(c)
		return
	}

	rawIDs, berr := bulkids.Normalize(mailContactIDs(form.IDs))
	if berr != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, berr)
		return
	}
	if len(rawIDs) == 0 {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	ids, invalid := mailBatchIDs(rawIDs)
	if len(invalid) > 0 {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, &mailInvalidBatchError{ids: invalid})
		return
	}
	// 一次 service 调用：整批同一事务，任一条失败整批回滚。
	if _, err := h.svc.DeleteContacts(c.Request.Context(), &maildto.DeleteContactsReq{
		IDs: ids, OperatorID: operator,
	}); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *Handle) ContactTag(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[maildto.MailContactTagForm](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	addTags, removeTags := splitTagInput(form.Add), splitTagInput(form.Remove)
	if len(addTags) == 0 && len(removeTags) == 0 {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrContactTagEmpty))
		return
	}
	rawIDs, berr := bulkids.Normalize(mailContactIDs(form.IDs))
	if berr != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, berr)
		return
	}
	if len(rawIDs) == 0 {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	ids, invalid := mailBatchIDs(rawIDs)
	if len(invalid) > 0 {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, &mailInvalidBatchError{ids: invalid})
		return
	}
	operator := mailContactOperator(c)
	// 一次 service 调用：加与减、整批联系人在同一事务里完成。
	if _, terr := h.svc.TagContacts(c.Request.Context(), &maildto.TagContactsReq{
		IDs:        ids,
		Add:        addTags,
		Remove:     removeTags,
		OperatorID: operator,
	}); terr != nil {
		pageview.FailScene(c, mailErrScene, terr)
		return
	}
	pageview.Success(c)
}
