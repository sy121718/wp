package userhttp

// user_handle_auth.go — 注册 / 验证 / 登录 / 登出 / 密码重置的页面与提交入口。
//
// 全部走**原生表单**（无 JS 依赖）：访客侧是公开页面，必须在脚本不可用时也能完成注册与登录。
// 每个表单显式带 csrf_token 隐藏域（原生表单不会自动带 HTMX 的 hx-headers）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	userservice "go_wp/internal/module/user/service"
)

// 访客结果页（user/message）的标题与正文（key + 中文兜底）。
//
// 这些文本是**直接渲染**进页面的（renderMessage 把成品文案塞进 gin.H 的 title/message），
// 硬写中文等于英文界面恒中文。带变量的正文用 %s 占位 —— 词条协议只允许字符串占位符，
// 因此变量先经 fmt.Sprintf 字符串化（用户名 / 邮箱本来就是字符串，无需转换）。
var (
	userMsgActivationInvalidTitle = userLabel{"user.message.activation_invalid.title", "验证链接无效"}
	userMsgActivationFailedTitle  = userLabel{"user.message.activation_failed.title", "验证未通过"}
	// userMsgActivatedTitle 复用访客消息词条 user.msg.activateSuccess（值同为「邮箱验证成功」）：
	// 同一句话在消息通道与结果页各留一条词条，翻译改动时必然分叉。
	userMsgActivatedTitle = userLabel{userenums.MsgActivateSuccess, "邮箱验证成功"}
	userMsgActivatedBody  = userLabel{"user.message.activated.body", "账号 {username} 已激活，现在可以登录了。"}

	userMsgResendFailedTitle = userLabel{"user.message.resend_failed.title", "重发失败"}
	userMsgResendDoneTitle   = userLabel{"user.message.resend_done.title", "验证邮件已重发"}
	userMsgResendDoneBody    = userLabel{"user.message.resend_done.body",
		"如果 {email} 是一个待验证的账号，新的验证邮件已经发出，请查收。"}

	userMsgLogoutFailedTitle = userLabel{"user.message.logout_failed.title", "登出失败"}
	userMsgResetMailTitle    = userLabel{"user.message.reset_mail.title", "重置邮件已提交"}
	userMsgLinkInvalidTitle  = userLabel{"user.message.link_invalid.title", "链接无效"}

	userMsgPasswordResetTitle = userLabel{"user.message.password_reset.title", "密码已重置"}
	userMsgPasswordResetBody  = userLabel{"user.message.password_reset.body", "请使用新密码登录。"}

	userMsgAccountOpenFailedTitle = userLabel{"user.message.account_open_failed.title", "打不开账号中心"}
	userMsgPasswordChangedTitle   = userLabel{"user.message.password_changed.title", "密码已修改"}
	userMsgPasswordChangedBody    = userLabel{"user.message.password_changed.body",
		"为安全起见，所有设备（包括当前这台）都已退出登录，请用新密码重新登录。"}
)

// ShowRegister 注册页。
func (h *Handle) ShowRegister(c *gin.Context) {
	if currentSession(c) != nil {
		// 已登录还去注册页，说明多半是点了旧书签：直接送回账号中心，
		// 不显示一个「注册新账号」的表单让人以为自己没登录。
		c.Redirect(http.StatusFound, "/user/account")
		return
	}
	h.render(c, http.StatusOK, "user/register", gin.H{
		"form": emptyRegisterForm(),
	})
}

// DoRegister 提交注册。
func (h *Handle) DoRegister(c *gin.Context) {
	form := gin.H{
		"username": formValue(c, "username"),
		"email":    formValue(c, "email"),
		"nickname": formValue(c, "nickname"),
	}
	req := &userdto.RegisterReq{
		Username:   form["username"].(string),
		Email:      form["email"].(string),
		Password:   c.PostForm("password"),
		Nickname:   form["nickname"].(string),
		RegisterIP: clientIP(c),
		Locale:     locale(c),
	}
	res, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		// 失败时**回填已填内容**（不含密码）：让人重新把所有字段打一遍是最容易劝退的一步。
		h.render(c, http.StatusBadRequest, "user/register", gin.H{
			"error": userPageMessage(c, err),
			"form":  form,
		})
		return
	}
	h.render(c, http.StatusOK, "user/register_done", gin.H{
		"username": res.Username,
		"email":    res.Email,
		// mailQueued=false 时页面要提示「可以点重发」——邮件没发出去时
		// 只显示「验证邮件已发送」会让用户一直等一封不会到的信。
		"mailQueued": res.MailQueued,
	})
}

// Activate 邮箱验证（GET，链接来自邮件）。
func (h *Handle) Activate(c *gin.Context) {
	key := formValue(c, "key")
	if key == "" {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgActivationInvalidTitle), userKeyText(c, userenums.ErrActivationInvalid))
		return
	}
	res, err := h.svc.ActivateEmail(c.Request.Context(), &userdto.ActivateEmailReq{
		Key:    key,
		Locale: locale(c),
	})
	if err != nil {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgActivationFailedTitle), userPageMessage(c, err))
		return
	}
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgActivatedTitle),
		userTextFilled(c, userMsgActivatedBody, map[string]string{"username": res.Username}))
}

// DoResendActivation 重发验证邮件。
func (h *Handle) DoResendActivation(c *gin.Context) {
	email := formValue(c, "email")
	if email == "" {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgResendFailedTitle), userKeyText(c, userenums.ErrEmailRequired))
		return
	}
	if err := h.svc.ResendActivation(c.Request.Context(), &userdto.ResendActivationReq{
		Email:  email,
		Locale: locale(c),
	}); err != nil {
		// 与密码重置不同，这里**如实报错**：重发接口要求填的邮箱本身就能通过注册接口
		// 探测出是否被占用，在这里沉默不会多保护任何信息，只会让「邮箱打错了」的人
		// 盯着「已发送」干等。
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgResendFailedTitle), userPageMessage(c, err))
		return
	}
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgResendDoneTitle),
		userTextFilled(c, userMsgResendDoneBody, map[string]string{"email": email}))
}

// ShowLogin 登录页。
func (h *Handle) ShowLogin(c *gin.Context) {
	if currentSession(c) != nil {
		c.Redirect(http.StatusFound, "/user/account")
		return
	}
	h.render(c, http.StatusOK, "user/login", gin.H{
		"next":    c.Query("next"),
		"account": "",
	})
}

// DoLogin 提交登录。
func (h *Handle) DoLogin(c *gin.Context) {
	account := formValue(c, "account")
	next := safeNext(c.PostForm("next"), "/user/account")

	res, err := h.svc.Login(c.Request.Context(), &userdto.LoginReq{
		Account:    account,
		Password:   c.PostForm("password"),
		RememberMe: formBool(c, "remember"),
	}, userservice.SessionMeta{
		IP:        clientIP(c),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.render(c, http.StatusUnauthorized, "user/login", gin.H{
			"error":   userPageMessage(c, err),
			"account": account,
			"next":    next,
		})
		return
	}

	if werr := writeUserToken(c, res.Token, formBool(c, "remember")); werr != nil {
		// cookie 写不进去 → 会话虽然是真建了的，但这个浏览器拿不到令牌。
		// 提示重新登录比「看着像登录成功、下一页又回到登录页」好排查。
		h.render(c, http.StatusInternalServerError, "user/login", gin.H{
			"error":   userenums.ErrInternal,
			"account": account,
			"next":    next,
		})
		return
	}

	// 登录成功必须**轮换 CSRF token**：匿名阶段会话里的 token 可能已被预置
	//（子域 Set-Cookie 注入），沿用旧值会让攻击者预知登录后的 token。
	if _, rerr := builtin.RotateCSRFTokenWith(c, userCSRFStore{}); rerr != nil {
		// 轮换失败不阻断登录：用户已经登录成功，此处只是 CSRF token 没换新。
		// 下一次渲染表单时 EnsureCSRFTokenWith 会补一个。
		_ = rerr
	}

	c.Redirect(http.StatusFound, next)
}

// Logout 登出（POST：GET 登出会被浏览器预取或图片标签意外触发）。
func (h *Handle) Logout(c *gin.Context) {
	if err := h.svc.Logout(c.Request.Context(), currentToken(c)); err != nil {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgLogoutFailedTitle), userPageMessage(c, err))
		return
	}
	// 先撤销服务端会话再清 cookie：反过来的话，若撤销失败，用户看到的是
	// 「已经登出」（cookie 没了），但服务端会话仍然有效。
	if err := clearUserSession(c); err != nil {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgLogoutFailedTitle), userKeyText(c, userenums.ErrInternal))
		return
	}
	c.Redirect(http.StatusFound, "/user/login")
}

// ShowForgot 申请重置密码页。
func (h *Handle) ShowForgot(c *gin.Context) {
	h.render(c, http.StatusOK, "user/forgot", gin.H{
		"email": c.Query("email"),
	})
}

// DoForgot 提交重置申请。
//
// **无论邮箱是否存在都显示同一句成功文案**：这个接口是匿名的、只需一个字段，
// 如实报错就等于提供了一个「查这个邮箱注册过没有」的接口。
func (h *Handle) DoForgot(c *gin.Context) {
	email := formValue(c, "email")
	if email == "" {
		h.render(c, http.StatusBadRequest, "user/forgot", gin.H{
			"error": userenums.ErrEmailRequired,
			"email": email,
		})
		return
	}
	if err := h.svc.RequestPasswordReset(c.Request.Context(), &userdto.PasswordResetReqRequest{
		Email:  email,
		Locale: locale(c),
	}); err != nil {
		h.render(c, http.StatusBadRequest, "user/forgot", gin.H{
			"error": userPageMessage(c, err),
			"email": email,
		})
		return
	}
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgResetMailTitle), userKeyText(c, userenums.MsgResetMailSent))
}

// ShowReset 重置密码页（链接来自邮件，带 email + key）。
func (h *Handle) ShowReset(c *gin.Context) {
	email := formValue(c, "email")
	key := formValue(c, "key")
	if email == "" || key == "" {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgLinkInvalidTitle), userKeyText(c, userenums.ErrActivationInvalid))
		return
	}
	h.render(c, http.StatusOK, "user/reset", gin.H{
		"email": email,
		"key":   key,
	})
}

// DoReset 提交新密码。
func (h *Handle) DoReset(c *gin.Context) {
	email := formValue(c, "email")
	key := formValue(c, "key")
	form := gin.H{"email": email, "key": key}

	err := h.svc.ResetPassword(c.Request.Context(), &userdto.ResetPasswordReq{
		Email:       email,
		Key:         key,
		NewPassword: c.PostForm("password"),
	})
	if err != nil {
		h.render(c, http.StatusBadRequest, "user/reset", gin.H{
			"error": userPageMessage(c, err),
			"email": email,
			"key":   key,
		})
		return
	}
	_ = form
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgPasswordResetTitle), userTextOf(c, userMsgPasswordResetBody))
}

// renderMessage 渲染通用结果页（激活 / 重发 / 重置这类一次性结果）。
//
// 不为每种结果写一个模板：它们结构完全相同（一个标题 + 一段话 + 一个去处），
// 拆成五个模板只会让「改一次文案要改五个文件」。
func (h *Handle) renderMessage(c *gin.Context, status int, ok bool, title, message string) {
	h.render(c, status, "user/message", gin.H{
		"ok":      ok,
		"title":   title,
		"message": message,
	})
}

// emptyRegisterForm 注册表单的初始值（模板用 chain 索引取值，键必须齐全）。
func emptyRegisterForm() gin.H {
	return gin.H{"username": "", "email": "", "nickname": ""}
}
