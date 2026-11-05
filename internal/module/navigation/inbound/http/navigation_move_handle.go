package navigationhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	pageview "go_wp/pkg/view"
	"go_wp/pkg/response"
)

func (h *Handle) moveNavigationEnvelope(c *gin.Context, env *navigationdto.UpdateEnvelope) {
	if err := env.ValidateMove(); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	req, err := env.MoveRequest()
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	// move 已在信封层校验过形状（id / dir 必填）；这里只把已校验的事实交给 service。
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Dir) == "" {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	if _, err := h.svc.SwapMove(c.Request.Context(), &navigationdto.SwapMoveReq{
		ID: req.ID, ProjectID: req.ProjectID, Dir: req.Dir,
	}); err != nil {
		pageview.FailStatus(c, navigationErrorStatus(err), navigationErrScene, err)
		return
	}
	c.HTML(http.StatusOK, "admin/navigation/navigation_write_result.html", pageview.Objects(
		pageview.Object("Message", response.MsgText(c, navDoneNotice)),
	))
}
