package userhttp

// user_handle_account.go — 账号中心（资料 / 偏好 / 改密码 / 登录设备）。
//
// 这一组全部挂在 requireUser 之下：未登录一律 302 到登录页。
// userID **只从会话里取**，页面上的表单里没有、也不接受任何用户 id 字段 ——
// 从请求参数取 id 的账号接口是「谁都能改别人的资料」，而且它看起来完全正常。

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
)

// accountNextTarget 取「保存类操作完成后的站内回跳目标」；没有或不可信则返回空串。
//
// 为什么需要它：账号表单现在是**片段**，可以出现在作者自己排的账号页上。
// 提交后停在内置账号页，会让作者页面上的账号中心半途跳到另一个页面 ——
// 那正好抵消了「把账号功能放到自己页面上」的意义。
//
// 为什么必须校验而不是照抄表单值：直接把 next 塞进 Location 就是**开放重定向**，
// 站点会变成任意目的地的跳板，而页面看起来一切正常，没人会去查。
// 规则取最保守的一档（站内相对路径），够用且不留解释空间：
//   - 必须以单个 / 开头（//evil.com 是协议相对 URL，会被浏览器当外部站点）；
//   - 不得含反斜杠（多数浏览器把它归一成正斜杠，于是「斜杠 + 反斜杠 + 主机名」会被解析成站外地址）；
//   - 不得含控制字符（换行会污染响应头）；
//   - 长度封顶，避免把超长串带进 Location。
func accountNextTarget(c *gin.Context) string {
	next := strings.TrimSpace(c.PostForm("next"))
	if next == "" || len(next) > 512 {
		return ""
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return ""
	}
	if strings.ContainsAny(next, "\\\r\n") {
		return ""
	}
	return next
}

// redirectAfterSave 保存成功后的落点：带了合法的站内 next 就回跳，否则留在内置账号页。
//
// 只在**成功**路径上回跳：失败必须把错误显示出来，而错误现在只有内置账号页会渲染。
// 把失败也送走，用户会得到一个「点了保存、页面刷新了、什么都没变」的界面。
func redirectAfterSave(c *gin.Context) bool {
	target := accountNextTarget(c)
	if target == "" {
		return false
	}
	c.Redirect(http.StatusFound, target)
	return true
}

// ShowAccount 账号中心。
func (h *Handle) ShowAccount(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		// 理论上 requireUser 已经挡掉，这里是纵深防御：
		// 少一次判空就少一处「改装配顺序时静默变成 500」的地方。
		c.Redirect(http.StatusFound, "/user/login?next=%2Fuser%2Faccount")
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// renderAccount 渲染账号中心（actionError 非空时在页面上显示一条错误）。
func (h *Handle) renderAccount(c *gin.Context, status int, actionError string) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login?next=%2Fuser%2Faccount")
		return
	}
	account, err := h.svc.GetAccount(c.Request.Context(), sess.UserID)
	if err != nil {
		h.renderMessage(c, http.StatusInternalServerError, false, "打不开账号中心", userMessage(err))
		return
	}
	sessions, serr := h.svc.ListSessions(c.Request.Context(), sess.UserID, currentToken(c))
	if serr != nil {
		// 设备列表拉不到不该让整页打不开：资料与偏好仍然可用。
		sessions = nil
	}
	h.render(c, status, "user/account", gin.H{
		"account":     account,
		"sessions":    sessions,
		"actionError": actionError,
	})
}

// DoUpdateProfile 保存资料。
func (h *Handle) DoUpdateProfile(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	err := h.svc.UpdateProfile(c.Request.Context(), &userdto.UpdateProfileReq{
		UserID:    sess.UserID,
		Nickname:  formValue(c, "nickname"),
		FirstName: formValue(c, "firstName"),
		LastName:  formValue(c, "lastName"),
		Gender:    formInt(c, "gender", 0),
		Birthday:  formValue(c, "birthday"),
		Bio:       formValue(c, "bio"),
		Website:   formValue(c, "website"),
		Locale:    formValue(c, "locale"),
		Timezone:  formValue(c, "timezone"),
		Country:   formValue(c, "country"),
		Province:  formValue(c, "province"),
		City:      formValue(c, "city"),
		Address:   formValue(c, "address"),
		Postcode:  formValue(c, "postcode"),
		Phone:     formValue(c, "phone"),
		Company:   formValue(c, "company"),
	})
	if err != nil {
		h.renderAccount(c, http.StatusBadRequest, userMessage(err))
		return
	}
	if redirectAfterSave(c) {
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// DoUpdatePreference 保存偏好。
func (h *Handle) DoUpdatePreference(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	err := h.svc.UpdatePreference(c.Request.Context(), &userdto.UpdatePreferenceReq{
		UserID:            sess.UserID,
		Theme:             formValue(c, "theme"),
		Locale:            formValue(c, "locale"),
		Timezone:          formValue(c, "timezone"),
		PageSize:          formInt(c, "pageSize", 20),
		EmailNotify:       formBool(c, "emailNotify"),
		SmsNotify:         formBool(c, "smsNotify"),
		ProfileVisibility: formValue(c, "profileVisibility"),
		ShowOnline:        formBool(c, "showOnline"),
	})
	if err != nil {
		h.renderAccount(c, http.StatusBadRequest, userMessage(err))
		return
	}
	if redirectAfterSave(c) {
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// DoChangePassword 修改密码。
//
// 改密成功后 service 会撤销该用户的**全部**会话（含当前这一个），
// 所以这里必须清 cookie 并把人送回登录页 —— 留着 cookie 会让人看到
// 「还在账号中心、但每个操作都跳登录页」的诡异状态。
func (h *Handle) DoChangePassword(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	err := h.svc.ChangePassword(c.Request.Context(), &userdto.ChangePasswordReq{
		UserID:      sess.UserID,
		OldPassword: c.PostForm("oldPassword"),
		NewPassword: c.PostForm("newPassword"),
	})
	if err != nil {
		h.renderAccount(c, http.StatusBadRequest, userMessage(err))
		return
	}
	_ = clearUserSession(c)
	h.renderMessage(c, http.StatusOK, true, "密码已修改",
		"为安全起见，所有设备（包括当前这台）都已退出登录，请用新密码重新登录。")
}

// DoRevokeSession 踢掉某台设备。
func (h *Handle) DoRevokeSession(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	rowID, perr := strconv.ParseUint(formValue(c, "id"), 10, 64)
	if perr != nil || rowID == 0 {
		h.renderAccount(c, http.StatusBadRequest, userenums.ErrSessionNotFound)
		return
	}
	if err := h.svc.RevokeSession(c.Request.Context(), sess.UserID, rowID); err != nil {
		h.renderAccount(c, http.StatusBadRequest, userMessage(err))
		return
	}
	if redirectAfterSave(c) {
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// DoRevokeOtherSessions 退出其它所有设备。
func (h *Handle) DoRevokeOtherSessions(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	if _, err := h.svc.RevokeOtherSessions(c.Request.Context(), sess.UserID, currentToken(c)); err != nil {
		h.renderAccount(c, http.StatusBadRequest, userMessage(err))
		return
	}
	if redirectAfterSave(c) {
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}
