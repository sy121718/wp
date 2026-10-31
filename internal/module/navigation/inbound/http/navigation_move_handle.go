package navigationhttp

import (
	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/response"
)

var navigationMoveKeys = map[string]bool{
	"action":    true,
	"id":        true,
	"projectId": true,
	"dir":       true,
}

func (h *Handle) moveNavigationEnvelope(c *gin.Context, env *navigationdto.UpdateEnvelope) {
	if err := env.ValidateMove(); err != nil {
		response.ParamError(c)
		return
	}
	req, err := env.MoveRequest()
	if err != nil {
		response.ParamError(c)
		return
	}
	raw := map[string]json.RawMessage{}
	for key, value := range map[string]string{"id": req.ID, "dir": req.Dir} {
		encoded, merr := json.Marshal(value)
		if merr != nil {
			response.ParamError(c)
			return
		}
		raw[key] = encoded
	}

	if req.ProjectID != "" {
		encoded, merr := json.Marshal(req.ProjectID)
		if merr != nil {
			response.ParamError(c)
			return
		}
		raw["projectId"] = encoded
	}
	h.moveNavigation(c, raw)
}

func (h *Handle) moveNavigation(c *gin.Context, raw map[string]json.RawMessage) {
	if !navigationDeleteShapeOK(raw, navigationMoveKeys) {
		response.ParamError(c)
		return
	}
	id, ok := navigationWriteRequiredString(raw, "id")
	if !ok {
		response.ParamError(c)
		return
	}

	dir, ok := navigationWriteRequiredString(raw, "dir")
	if !ok {
		response.ParamError(c)
		return
	}

	projectID, ok := navigationAddSourceOptionalString(raw, "projectId")
	if !ok {
		response.ParamError(c)
		return
	}
	res, err := h.svc.SwapMove(c.Request.Context(), &navigationdto.SwapMoveReq{
		ID:        id,
		ProjectID: projectID,
		Dir:       dir,
	})
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgUpdateSuccess, res)
}
