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
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

func (h *Handle) AutomationSave(c *gin.Context) {
	req, err := binding.ParseJSON[maildto.SaveAutomationReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseJSON[maildto.SaveAutomationLayoutReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseJSON[maildto.SetAutomationStatusReq](c)
	if err != nil {
		response.ParamError(c)
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
	query, err := binding.ParseQuery[maildto.MailIDQuery](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.DeleteAutomation(c.Request.Context(), parseID(query.ID)); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

func (h *Handle) AutomationStartRun(c *gin.Context) {
	req, err := binding.ParseJSON[maildto.AutomationRunReq](c)
	if err != nil {
		response.ParamError(c)
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

// jsonOutletTr 是 JSON 出口的取词口：投递/运行记录里的错误说明是数据接口字段，
// 取词走 pkg/response（与 response.MessageOf 同源），不在 handler 里自建 i18n 闭包。
func jsonOutletTr(c *gin.Context) mailTr {
	return func(key, fallback string) string {
		return response.MsgText(c, facing.Msg{Key: key, Fallback: fallback})
	}
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

	mailRunTexts(jsonOutletTr(c), res.Items)
	response.Success(c, res)
}

func (h *Handle) AutomationRunDetail(c *gin.Context) {
	res, err := h.svc.AutomationRunDetail(c.Request.Context(), parseID(c.Query("id")))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	mailRunDetailTexts(jsonOutletTr(c), res)
	response.Success(c, res)
}

func (h *Handle) AutomationTick(c *gin.Context) {
	query, err := binding.ParseQuery[maildto.MailTickQuery](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 200
	}
	n, err := h.svc.EnqueueDueRuns(c.Request.Context(), limit)
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
	req, err := binding.ParseForm[maildto.SaveAccountReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	var item *maildto.AccountItem
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
	query, err := binding.ParseQuery[maildto.MailIDQuery](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	id := parseID(query.ID)
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
	query, err := binding.ParseQuery[maildto.MailIDQuery](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	id := parseID(query.ID)
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
	req, err := binding.ParseForm[maildto.TestSendReq](c)
	if err != nil {
		response.ParamError(c)
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

	req, err := binding.ParseJSON[maildto.SaveTemplateReq](c)
	if err != nil {
		response.ParamError(c)
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
	query, err := binding.ParseQuery[maildto.MailTemplateKeyQuery](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.DeleteTemplate(c.Request.Context(), query.TemplateKey, query.Locale); err != nil {
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
	form, err := binding.ParseForm[maildto.ContactImportFormReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseForm[maildto.UpdateContactStatusReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseForm[maildto.SaveCampaignReq](c)
	if err != nil {
		response.ParamError(c)
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
	query, err := binding.ParseQuery[maildto.MailIDQuery](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.DeleteCampaign(c.Request.Context(), parseID(query.ID)); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

func (h *Handle) CampaignStart(c *gin.Context) {
	req, err := binding.ParseForm[maildto.StartCampaignReq](c)
	if err != nil {
		response.ParamError(c)
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
