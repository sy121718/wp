package buildhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type Handle struct {
	svc buildcontract.BuildService
}

func NewHandle(svc buildcontract.BuildService) *Handle { return &Handle{svc: svc} }

func (h *Handle) Queue(c *gin.Context) {
	res, err := h.svc.Stats(c.Request.Context(), strings.TrimSpace(c.Query("project")))
	if err != nil {
		response.ErrorInternal(c, "build", err)
		return
	}
	response.SuccessWithMessage(c, buildenums.MsgQueueFound, res)
}

func (h *Handle) List(c *gin.Context) {
	req, ok := binding.Query[builddto.ListReq](c)
	if !ok {
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorInternal(c, "build", err)
		return
	}
	response.Success(c, list)
}

type retryReq struct {
	ID string `json:"id"`

	ProjectID string `json:"projectId"`
}

func (h *Handle) Retry(c *gin.Context) {
	var req retryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		response.ParamError(c, buildenums.ErrInvalidParam)
		return
	}

	if err := h.svc.Retry(c.Request.Context(), strings.TrimSpace(req.ProjectID), req.ID); err != nil {
		if err.Error() == buildenums.ErrJobNotFound || err.Error() == buildenums.ErrInvalidParam {
			response.ErrorAuto(c, http.StatusBadRequest, "build", err)
			return
		}
		response.ErrorInternal(c, "build", err)
		return
	}
	response.SuccessWithMessage(c, buildenums.MsgJobRetried, nil)
}
