// mail_handle.go — 邮箱模块 HTTP 承接层：只做参数绑定 / 调 service / 输出响应。
package mailhttp

import (
	"strconv"
	"strings"

	"net/http"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/response"
)

// Handle 邮箱模块 HTTP 处理器。
type Handle struct {
	svc mailcontract.MailService
}

// NewHandle 构造。
func NewHandle(svc mailcontract.MailService) *Handle { return &Handle{svc: svc} }

// AccountList 账号列表。
func (h *Handle) AccountList(c *gin.Context) {
	list, err := h.svc.ListAccounts(c.Request.Context(), c.Query("purpose"))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, list)
}

// AccountSave 新建 / 更新账号（id 为 0 即新建）。
func (h *Handle) AccountSave(c *gin.Context) {
	var req maildto.SaveAccountReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	var (
		item *maildto.AccountItem
		err  error
	)
	if req.ID > 0 {
		item, err = h.svc.UpdateAccount(c.Request.Context(), &req)
	} else {
		item, err = h.svc.CreateAccount(c.Request.Context(), &req)
	}
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, item)
}

// AccountDelete 删除账号。
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

// AccountSetDefault 设为该用途的默认账号。
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

// AccountTestSend 用指定账号发测试邮件。
func (h *Handle) AccountTestSend(c *gin.Context) {
	var req maildto.TestSendReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	req.Lang = response.RequestLanguage(c)
	res, err := h.svc.TestSend(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgTestSent, res)
}

// TemplateList 模板列表。
func (h *Handle) TemplateList(c *gin.Context) {
	list, err := h.svc.ListTemplates(c.Request.Context(), c.Query("templateKey"))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, list)
}

// TemplateSave 新建 / 覆盖模板。
func (h *Handle) TemplateSave(c *gin.Context) {
	var req maildto.SaveTemplateReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	item, err := h.svc.UpsertTemplate(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, item)
}

// TemplateDelete 删除模板。
func (h *Handle) TemplateDelete(c *gin.Context) {
	if err := h.svc.DeleteTemplate(c.Request.Context(), c.Query("templateKey"), c.Query("locale")); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

// ContactList 联系人列表。
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

// ContactImport 导入联系人（原始内容放正文，或表单字段 content）。
func (h *Handle) ContactImport(c *gin.Context) {
	content := c.PostForm("content")
	if content == "" {
		// 也支持直接 POST 原始文本（import 用 curl / 脚本灌数据时更方便）。
		if raw, err := c.GetRawData(); err == nil {
			content = string(raw)
		}
	}
	req := &maildto.ImportContactsReq{
		Content:         []byte(content),
		ConsentDeclared: c.PostForm("consentDeclared") == "true",
		ConsentSource:   c.PostForm("consentSource"),
		UpdateExisting:  c.PostForm("updateExisting") == "true",
	}
	if tags := strings.TrimSpace(c.PostForm("tags")); tags != "" {
		req.DefaultTags = splitCSV(tags)
	}
	res, err := h.svc.ImportContacts(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgImportSuccess, res)
}

// ContactStatus 改联系人状态（后台手工订阅 / 退订）。
func (h *Handle) ContactStatus(c *gin.Context) {
	var req maildto.UpdateContactStatusReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	if err := h.svc.UpdateContactStatus(c.Request.Context(), &req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, nil)
}

// CampaignList 活动列表。
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

// CampaignGet 活动详情。
func (h *Handle) CampaignGet(c *gin.Context) {
	res, err := h.svc.GetCampaign(c.Request.Context(), parseID(c.Query("id")))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.Success(c, res)
}

// CampaignSave 新建 / 更新活动。
func (h *Handle) CampaignSave(c *gin.Context) {
	var req maildto.SaveCampaignReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SaveCampaign(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, res)
}

// CampaignDelete 删除活动。
func (h *Handle) CampaignDelete(c *gin.Context) {
	if err := h.svc.DeleteCampaign(c.Request.Context(), parseID(c.Query("id"))); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "mail", err)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

// CampaignStart 启动群发（HTTP 秒回，收件人展开在后台分批进行）。
func (h *Handle) CampaignStart(c *gin.Context) {
	var req maildto.StartCampaignReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.StartCampaign(c.Request.Context(), &req)
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
