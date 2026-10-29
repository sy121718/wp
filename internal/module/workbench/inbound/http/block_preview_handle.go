package workbenchhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/response"
)

// BlockPreviewDraft 预览尚未保存的块文档，沿原授权与版本规则读取目标，不写块或发布产物。
func (h *Handle) BlockPreviewDraft(c *gin.Context) {
	req, err := binding.ParseForm[workbenchdto.BlockPreviewDraftReq](c)
	if err != nil {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrDraftDecodeFailed))
		return
	}
	block, err := h.svc.BlockDraftForPreview(c.Request.Context(), req)
	if err != nil {
		c.String(response.ErrorStatus(err), response.MessageOf(c, "workbench.block-preview", err))
		return
	}
	h.renderPreview(c, json.RawMessage(req.DraftDocument), block.ProjectID, "", true, workbenchservice.PreviewDocBlock)
}
