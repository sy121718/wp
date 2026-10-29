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
	req, ok := binding.JSON[userdto.RegisterReq](c)
	if !ok {
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
	req, ok := binding.JSON[userdto.LoginReq](c)
	if !ok {
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

func (h *Handle) SiteForgot(c *gin.Context) {
	req, ok := binding.JSON[userdto.PasswordResetReqRequest](c)
	if !ok {
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
	req, ok := binding.JSON[userdto.ResetPasswordReq](c)
	if !ok {
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "user", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) SiteUpdateProfile(c *gin.Context) {
	req, ok := binding.JSON[userdto.UpdateProfileReq](c)
	if !ok {
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
	req, ok := binding.JSON[userdto.UpdatePreferenceReq](c)
	if !ok {
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
	req, ok := binding.JSON[userdto.ChangePasswordReq](c)
	if !ok {
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
	req, ok := binding.JSON[userdto.SiteRevokeReq](c)
	if !ok {
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
