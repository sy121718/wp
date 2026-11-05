package sysconfighttp

import (
	"go_wp/internal/middleware/builtin"
	"net/http"

	"github.com/gin-gonic/gin"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	"go_wp/pkg/binding"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

func SetupSysConfigRoutes(authorizedAPI *mw.RouteGroup, adminPages *gin.RouterGroup, svc sysconfigcontract.Service) {
	h := NewAdminHandle(svc)

	g := authorizedAPI.Group("/sysconfig")
	g.GET("/get", sysconfigenums.SysConfigGet, h.APIList)
	g.POST("/save", sysconfigenums.SysConfigSave, h.APISave)
	g.POST("/system/save", sysconfigenums.SysConfigSystemSave, pageview.HTMLFragment, h.APISystemSave)

	SetupSysConfigPages(adminPages, h)
}

func (h *AdminHandle) APIList(c *gin.Context) {
	groups, err := h.svc.ListGroups(c.Request.Context())
	if err != nil {

		status := http.StatusInternalServerError
		if response.IsBusinessError(err) {
			status = http.StatusBadRequest
		}
		response.ErrorAuto(c, status, "sysconfig", err)
		return
	}
	response.Success(c, gin.H{"groups": groups})
}

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
