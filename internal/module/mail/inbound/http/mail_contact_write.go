package mailhttp

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/internal/middleware/builtin"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

func mailContactOperator(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

type mailContactBulkItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

type mailContactTagItem struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated"`
	Message string `json:"message,omitempty"`
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
	var form maildto.MailContactSaveForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
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
	mailWriteOK(c)
}

func (h *Handle) ContactDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailContactDeleteForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
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
		mailWriteOK(c)
		return
	}

	ids, berr := bulkids.Normalize(mailContactIDs(form.IDs))
	if berr != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, berr)
		return
	}
	res := mailContactBulkResult{Items: make([]mailContactBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := mailContactBulkItem{ID: rawID}
		contactID := mailIDOfText(rawID)
		switch {
		case contactID == 0:
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
	mailWriteResult(c, "admin/mail/mail_contact_result.html", pageview.Objects(
		pageview.Object("None", len(ids) == 0),
		pageview.Object("Done", res.Deleted),
		pageview.Object("Skipped", res.Skipped),
		pageview.Object("Rows", mailContactRowsOf(res.Items)),
	))
}

func (h *Handle) ContactTag(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailContactTagForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	addTags, removeTags := splitTagInput(form.Add), splitTagInput(form.Remove)
	if len(addTags) == 0 && len(removeTags) == 0 {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrContactTagEmpty))
		return
	}
	ids, berr := bulkids.Normalize(mailContactIDs(form.IDs))
	if berr != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, berr)
		return
	}
	operator := mailContactOperator(c)
	res := mailContactTagResult{Items: make([]mailContactTagItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		contactID := mailIDOfText(rawID)
		if contactID == 0 {
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
	mailWriteResult(c, "admin/mail/mail_contact_tag_result.html", pageview.Objects(
		pageview.Object("None", len(ids) == 0),
		pageview.Object("Done", res.Updated),
		pageview.Object("Skipped", res.Skipped),
		pageview.Object("Rows", mailTagRowsOf(res.Items)),
	))
}
