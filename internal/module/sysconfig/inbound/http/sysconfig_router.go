package sysconfighttp

// sysconfig_router.go — sysconfig 模块的 **API** 装配（挂 authorizedAPI 三层链）。
//
// 后台页面（/admin/system）的注册在 sysconfig_page_router.go，由本文件的
// SetupSysConfigRoutes 在同一位置调用 —— 落点分开、装配顺序不变。

import (
	"go_wp/internal/middleware/builtin"
	"net/http"

	"github.com/gin-gonic/gin"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	"go_wp/internal/permission"
	"go_wp/pkg/binding"
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
	g.POST("/system/save", permission.SysConfigSystemSave, h.APISystemSave)

	// 后台页面：注册在 sysconfig_page_router.go（同一入口调用，装配顺序不变）。
	SetupSysConfigPages(adminPages, h)
}

// APIList GET /api/sysconfig/get：列出分组（JSON 出口，供其它消费方读取）。
func (h *AdminHandle) APIList(c *gin.Context) {
	groups, err := h.svc.ListGroups(c.Request.Context())
	if err != nil {
		// 出口只有一份判据（pkg/facing）：业务错误按 400 透出（含中文兜底），
		// 其余由 ErrorAuto 归口成 500 + 原文进日志。
		status := http.StatusInternalServerError
		if response.IsBusinessError(err) {
			status = http.StatusBadRequest
		}
		response.ErrorAuto(c, status, "sysconfig", err)
		return
	}
	response.Success(c, gin.H{"groups": groups})
}

// APISave POST /api/sysconfig/save：整组保存（JSON 出口）。
func (h *AdminHandle) APISave(c *gin.Context) {
	req, ok := binding.JSON[sysconfigdto.SetGroupReq](c)
	if !ok {
		return
	}
	req.UpdateBy = int64(builtin.CurrentUserID(c))
	res, err := h.svc.SetGroup(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusConflict, response.MessageOf(c, "sysconfig", err))
		return
	}
	response.SuccessWithMessage(c, sysconfigenums.MsgGroupSaved, res)
}
