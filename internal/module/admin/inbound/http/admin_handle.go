package adminhttp

import (
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/auth"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handle struct {
	as admincontract.AdminService
}

func NewHandle(as admincontract.AdminService) *Handle {
	return &Handle{
		as: as,
	}
}

func (h *Handle) List(c *gin.Context) {
	var req admindto.ListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+":"+err.Error())
		return
	}

	res, err := h.as.List(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.Success(c, res)
}

// Login 管理员登录
func (h *Handle) Login(c *gin.Context) {
	var req admindto.LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+"："+err.Error())
		return
	}

	res, err := h.as.Login(c.Request.Context(), &req, c.ClientIP())
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}

	c.Header("X-New-Token", res.AccessToken)
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// Logout 注销当前登录会话。
func (h *Handle) Logout(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	uid, ok := userID.(int64)
	if !ok {
		r.ErrorWithMessage(c, 500, adminenums.MsgWrongUserType)
		return
	}
	if err := h.as.Logout(c.Request.Context(), uint64(uid)); err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	c.Set(auth.ContextSessionRevokedKey, true)
	r.SuccessWithMessage(c, adminenums.MsgLogoutSuccess, nil)
}

// Profile 获取当前登录用户信息。
func (h *Handle) Profile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}

	uid, ok := userID.(int64)
	if !ok {
		r.ErrorWithMessage(c, 500, adminenums.MsgWrongUserType)
		return
	}

	res, err := h.as.Profile(c.Request.Context(), uint64(uid))
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.Success(c, res)
}

// 新增管理员控制器
// h 代表指针类型的控制器服务，c是必备的上下文go没有全局共享
func (h *Handle) Create(c *gin.Context) {
	//变量 接收dto规则
	var req admindto.CreateReq
	//绑定规则，进行校验
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+"："+err.Error())
		return
	}
	//h是控制器服务，as是adminService服务，Create是具体方法，然后传递具体参数
	res, err := h.as.Create(c.Request.Context(), &req)
	// 捕获错误
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	//最终返回给用户信息
	r.Success(c, res)

}

func (h *Handle) Edit(c *gin.Context) {

	var req admindto.EditReq
	if err := c.ShouldBindJSON(&req); err != nil {

		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+":"+err.Error())
		return
	}
	res, err := h.as.Edit(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

func (h *Handle) Detail(c *gin.Context) {
	var req admindto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+"："+err.Error())
		return
	}

	res, err := h.as.Detail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.Success(c, res)
}

func (h *Handle) Delete(c *gin.Context) {
	var req admindto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+"："+err.Error())
		return
	}

	userID, exists := c.Get("user_id")
	uid, ok := userID.(int64)
	if !exists || !ok || uid <= 0 {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	req.OperatorID = uint64(uid)

	res, err := h.as.Delete(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.Success(c, res)
}

// --- 角色绑定 ---

func (h *Handle) RoleList(c *gin.Context) {
	var req admindto.RoleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.as.RoleList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

func (h *Handle) RoleSave(c *gin.Context) {
	var req admindto.RoleSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.as.RoleSave(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// --- 直接额外权限 ---

func (h *Handle) MenuList(c *gin.Context) {
	var req admindto.MenuListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.as.MenuList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

func (h *Handle) MenuSave(c *gin.Context) {
	var req admindto.MenuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.as.MenuSave(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// --- 动态路由权限投影 ---

func (h *Handle) Routes(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	uid, ok := userID.(int64)
	if !ok {
		r.ErrorWithMessage(c, 500, adminenums.MsgWrongUserType)
		return
	}
	res, err := h.as.Routes(c.Request.Context(), uint64(uid))
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}
