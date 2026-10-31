package mailhttp

import (
	"go_wp/internal/middleware/builtin"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

func (h *Handle) AutomationSave(c *gin.Context) {
	req, ok := binding.JSON[maildto.SaveAutomationReq](c)
	if !ok {
		return
	}
	req.OperatorID = builtin.CurrentUserID(c)
	item, err := h.svc.SaveAutomation(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, item)
}

func (h *Handle) AutomationLayout(c *gin.Context) {
	req, ok := binding.JSON[maildto.SaveAutomationLayoutReq](c)
	if !ok {
		return
	}
	if err := h.svc.SaveAutomationLayout(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, nil)
}

func (h *Handle) AutomationList(c *gin.Context) {
	res, err := h.svc.ListAutomations(c.Request.Context(), &maildto.AutomationListReq{
		Status:   c.Query("status"),
		Page:     parseInt(c.Query("page"), 1),
		PageSize: parseInt(c.Query("pageSize"), 20),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) AutomationGet(c *gin.Context) {
	item, err := h.svc.GetAutomation(c.Request.Context(), parseID(c.Query("id")))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, item)
}

func (h *Handle) AutomationStatus(c *gin.Context) {
	req, ok := binding.JSON[maildto.SetAutomationStatusReq](c)
	if !ok {
		return
	}
	req.OperatorID = builtin.CurrentUserID(c)
	if err := h.svc.SetAutomationStatus(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, nil)
}

func (h *Handle) AutomationDelete(c *gin.Context) {
	if err := h.svc.DeleteAutomation(c.Request.Context(), parseID(c.Query("id"))); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

func (h *Handle) AutomationStartRun(c *gin.Context) {
	req, ok := binding.JSON[maildto.AutomationRunReq](c)
	if !ok {
		return
	}
	req.OperatorID = builtin.CurrentUserID(c)
	started, err := h.svc.StartRun(c.Request.Context(), req.AutomationID, req.ContactID, "manual")
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	if !started {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrAutomationRunExists)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgAutomationStarted, nil)
}

func (h *Handle) AutomationRunList(c *gin.Context) {
	res, err := h.svc.ListAutomationRuns(c.Request.Context(), &maildto.AutomationRunListReq{
		AutomationID: parseID(c.Query("automationId")),
		Status:       c.Query("status"),
		Page:         parseInt(c.Query("page"), 1),
		PageSize:     parseInt(c.Query("pageSize"), 20),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}

	mailRunTexts(i18n.TranslateFunc(response.RequestLanguage(c)), res.Items)
	response.Success(c, res)
}

func (h *Handle) AutomationRunDetail(c *gin.Context) {
	res, err := h.svc.AutomationRunDetail(c.Request.Context(), parseID(c.Query("id")))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	mailRunDetailTexts(i18n.TranslateFunc(response.RequestLanguage(c)), res)
	response.Success(c, res)
}

func (h *Handle) AutomationTick(c *gin.Context) {
	n, err := h.svc.EnqueueDueRuns(c.Request.Context(), parseInt(c.Query("limit"), 200))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, gin.H{"queued": n})
}

type Handle struct {
	svc mailcontract.MailService
}

func NewHandle(svc mailcontract.MailService) *Handle { return &Handle{svc: svc} }

func (h *Handle) AccountList(c *gin.Context) {
	list, err := h.svc.ListAccounts(c.Request.Context(), c.Query("purpose"))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, list)
}

func (h *Handle) AccountSave(c *gin.Context) {
	req, ok := binding.Form[maildto.SaveAccountReq](c)
	if !ok {
		return
	}
	var (
		item *maildto.AccountItem
		err  error
	)
	if req.ID > 0 {
		item, err = h.svc.UpdateAccount(c.Request.Context(), req)
	} else {
		item, err = h.svc.CreateAccount(c.Request.Context(), req)
	}
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, item)
}

func (h *Handle) AccountDelete(c *gin.Context) {
	id := parseID(c.Query("id"))
	if id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteAccount(c.Request.Context(), id); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

func (h *Handle) AccountSetDefault(c *gin.Context) {
	id := parseID(c.Query("id"))
	if id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	if err := h.svc.SetDefaultAccount(c.Request.Context(), id); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, nil)
}

func (h *Handle) AccountTestSend(c *gin.Context) {
	req, ok := binding.Form[maildto.TestSendReq](c)
	if !ok {
		return
	}
	req.Lang = response.RequestLanguage(c)
	res, err := h.svc.TestSend(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgTestSent, res)
}

func (h *Handle) TemplateList(c *gin.Context) {
	list, err := h.svc.ListTemplates(c.Request.Context(), c.Query("templateKey"))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, list)
}

func (h *Handle) TemplateSave(c *gin.Context) {

	req, ok := binding.JSON[maildto.SaveTemplateReq](c)
	if !ok {
		return
	}
	item, err := h.svc.UpsertTemplate(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, item)
}

func (h *Handle) TemplateDelete(c *gin.Context) {
	if err := h.svc.DeleteTemplate(c.Request.Context(), c.Query("templateKey"), c.Query("locale")); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

func (h *Handle) ContactList(c *gin.Context) {
	req := &maildto.ContactFilterReq{
		Keyword:  c.Query("keyword"),
		Status:   c.Query("status"),
		Page:     parseInt(c.Query("page"), 1),
		PageSize: parseInt(c.Query("pageSize"), 20),
	}
	if tags := strings.TrimSpace(c.Query("tags")); tags != "" {
		req.Tags = splitCSV(tags)
	}
	res, err := h.svc.ListContacts(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) ContactImport(c *gin.Context) {
	form, ok := binding.Form[maildto.ContactImportFormReq](c)
	if !ok {
		return
	}
	content := form.Content
	if content == "" {

		if raw, err := c.GetRawData(); err == nil {
			content = string(raw)
		}
	}
	req := &maildto.ImportContactsReq{
		Content:         []byte(content),
		ConsentDeclared: form.ConsentDeclared == "true",
		ConsentSource:   form.ConsentSource,
		UpdateExisting:  form.UpdateExisting == "true",
	}
	if tags := strings.TrimSpace(form.Tags); tags != "" {
		req.DefaultTags = splitCSV(tags)
	}
	res, err := h.svc.ImportContacts(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgImportSuccess, res)
}

func (h *Handle) ContactStatus(c *gin.Context) {
	req, ok := binding.Form[maildto.UpdateContactStatusReq](c)
	if !ok {
		return
	}
	if err := h.svc.UpdateContactStatus(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, nil)
}

func (h *Handle) CampaignList(c *gin.Context) {
	res, err := h.svc.ListCampaigns(c.Request.Context(), &maildto.CampaignListReq{
		Status:   c.Query("status"),
		Page:     parseInt(c.Query("page"), 1),
		PageSize: parseInt(c.Query("pageSize"), 20),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) CampaignGet(c *gin.Context) {
	res, err := h.svc.GetCampaign(c.Request.Context(), parseID(c.Query("id")))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) CampaignSave(c *gin.Context) {
	req, ok := binding.Form[maildto.SaveCampaignReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SaveCampaign(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, res)
}

func (h *Handle) CampaignDelete(c *gin.Context) {
	if err := h.svc.DeleteCampaign(c.Request.Context(), parseID(c.Query("id"))); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

func (h *Handle) CampaignStart(c *gin.Context) {
	req, ok := binding.Form[maildto.StartCampaignReq](c)
	if !ok {
		return
	}
	res, err := h.svc.StartCampaign(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgCampaignStarted, res)
}

func parseID(raw string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	return v
}

func parseInt(raw string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
