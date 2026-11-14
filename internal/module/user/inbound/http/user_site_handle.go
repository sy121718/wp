package userhttp

import (
	"github.com/gin-gonic/gin"
	userdto "go_wp/internal/module/user/dto"
	userservice "go_wp/internal/module/user/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

func (h *Handle) SiteSession(c *gin.Context) {
	response.Success(c, h.svc.SiteSession(siteUserID(c)))
}

func (h *Handle) SiteAccount(c *gin.Context) {
	data, err := h.svc.AccountOf(c.Request.Context(), siteUserID(c))
	if err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, data)
}

func (h *Handle) SiteDevices(c *gin.Context) {
	data, err := h.svc.SiteDevices(c.Request.Context(), siteUserID(c), currentToken(c))
	if err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, data)
}

func (h *Handle) SiteRegister(c *gin.Context) {
	// 站点表单是标准 form 提交（user_forms_widget.jet）；显式表单入口只读 body。
	req, err := binding.ParseForm[userdto.RegisterReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.RegisterIP, req.Locale = clientIP(c), locale(c)
	data, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, data)
}

func (h *Handle) SiteLogin(c *gin.Context) {
	req, err := binding.ParseForm[userdto.LoginReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	data, err := h.svc.Login(c.Request.Context(), req, userservice.SessionMeta{IP: clientIP(c), UserAgent: c.Request.UserAgent()})
	if err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	if err := h.saveSiteLogin(c, data.Token, req.RememberMe); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, h.svc.SiteSession(data.UserID))
}

func (h *Handle) SiteLogout(c *gin.Context) {
	if err := h.svc.Logout(c.Request.Context(), currentToken(c)); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	if err := clearUserSession(c); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, h.svc.SiteSession(0))
}

func (h *Handle) SiteActivate(c *gin.Context) {
	req, err := binding.ParseForm[userdto.ActivateEmailReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.Locale = locale(c)
	data, err := h.svc.ActivateEmail(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, data)
}

func (h *Handle) SiteResendActivation(c *gin.Context) {
	req, err := binding.ParseForm[userdto.ResendActivationReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.Locale = locale(c)
	if err := h.svc.ResendActivation(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) SiteForgot(c *gin.Context) {
	req, err := binding.ParseForm[userdto.PasswordResetReqRequest](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.Locale = locale(c)
	if err := h.svc.RequestPasswordReset(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) SiteReset(c *gin.Context) {
	req, err := binding.ParseForm[userdto.ResetPasswordReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) SiteUpdateProfile(c *gin.Context) {
	req, err := binding.ParseForm[userdto.UpdateProfileReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.UserID = siteUserID(c)
	if err := h.svc.UpdateProfile(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) SiteUpdatePreference(c *gin.Context) {
	req, err := binding.ParseForm[userdto.UpdatePreferenceReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.UserID = siteUserID(c)
	if err := h.svc.UpdatePreference(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) SiteChangePassword(c *gin.Context) {
	req, err := binding.ParseForm[userdto.ChangePasswordReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.UserID = siteUserID(c)
	if err := h.changeSitePasswordSession(c, req); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, h.svc.SiteSession(0))
}

func (h *Handle) SiteRevokeDevice(c *gin.Context) {
	req, err := binding.ParseForm[userdto.SiteRevokeReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.RevokeSiteDevice(c.Request.Context(), siteUserID(c), currentToken(c), req.ID); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) SiteRevokeOtherDevices(c *gin.Context) {
	if _, err := h.svc.RevokeOtherSessions(c.Request.Context(), siteUserID(c), currentToken(c)); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, nil)
}
