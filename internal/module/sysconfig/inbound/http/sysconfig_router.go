package sysconfighttp

// sysconfig_router.go — sysconfig 模块路由（API + 后台页面）。
//
// 页面写操作复用 API 权限点（AGENTS：加常量 + 在路由注册处声明，不写 seed 迁移）：
// 页面前缀 /admin/system 与权限点路径 /api/sysconfig/* 不一致，必须用
// CasbinMiddlewareForPath 显式指定 obj —— 直接按页面路径 enforce 会全员 403
// （权限点表里没有页面路径）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	"go_wp/internal/permission"
	"go_wp/internal/web/shell"
	"go_wp/pkg/response"
)

// SetupSysConfigRoutes 注册 sysconfig 的 API 与后台页面路由。
//
// svc 由装配层传入（同一个实例已挂在 i18n 的 ValueLoader 上，保存后的主动刷新走它）。
func SetupSysConfigRoutes(authorizedAPI *permission.RouteGroup, adminPages *gin.RouterGroup, svc sysconfigcontract.Service) {
	h := NewAdminHandle(svc)

	g := authorizedAPI.Group("/sysconfig")
	g.GET("/get", permission.SysConfigGet, h.APIList)
	g.POST("/save", permission.SysConfigSave, h.APISave)

	// 后台页面：Session + CSRF（adminPages 组）已具备。
	adminPages.GET("/system", builtin.CasbinMiddlewareForPathAs("/api/sysconfig/get", http.MethodGet), h.SystemPage)
	adminPages.POST("/system/save", builtin.CasbinMiddlewareForPath("/api/sysconfig/save"), h.SystemSave)
}

// APIList GET /api/sysconfig/get：列出分组（JSON 出口，供其它消费方读取）。
func (h *AdminHandle) APIList(c *gin.Context) {
	groups, err := h.svc.ListGroups(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, sysconfigenums.ErrInternal)
		return
	}
	response.Success(c, gin.H{"groups": groups})
}

// APISave POST /api/sysconfig/save：整组保存（JSON 出口）。
func (h *AdminHandle) APISave(c *gin.Context) {
	var req sysconfigdto.SetGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, sysconfigenums.ErrInvalidParam)
		return
	}
	req.UpdateBy = int64(shell.CurrentUserID(c))
	res, err := h.svc.SetGroup(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusConflict, sysconfigErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, sysconfigenums.MsgGroupSaved, res)
}
