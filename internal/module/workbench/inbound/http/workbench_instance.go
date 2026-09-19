package workbenchhttp

// workbench_instance.go — 实例编辑模式（?instance=ID，docs/04-C-instance-override.md）：
// 画布编辑的是该实例自己的覆盖文档（迁移 281），保存走 presentation.SaveOverrideDocument，
// 不影响共享模板。

import (
	"encoding/json"
	"net/http"
	"strings"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"

	"go_wp/internal/builder"
	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// SetInstanceOverrideDeps 注入实例编辑模式端口（装配期调用；未注入时页面明确提示）。
func (h *Handle) SetInstanceOverrideDeps(instances presentationcontract.PresentationService) {
	h.instances = instances
}

// workbenchInstance 实例编辑模式外壳。
func (h *Handle) workbenchInstance(c *gin.Context, instanceID string) {
	if h.instances == nil {
		c.String(http.StatusServiceUnavailable, "实例编辑能力未装配")
		return
	}
	ctx := c.Request.Context()
	inst, err := h.instances.Get(ctx, &presentationdto.GetReq{ID: instanceID, ProjectID: c.Query("projectId")})
	if err != nil {
		c.String(http.StatusNotFound, "实例不存在")
		return
	}
	// 生效文档：override 优先（toResp 随快照返回），否则实例尚无可编辑文档，
	// 由调用方（商品编辑页）先经 CreateInstance/Rebuild 产生快照再进来。
	document := inst.Document
	if len(document) == 0 {
		c.String(http.StatusUnprocessableEntity, "实例缺少可编辑文档（未发布且无覆盖）")
		return
	}
	documentJSON, _ := json.Marshal(document)
	// 渲染模式（迁移 282，双轨）：状态条据此显示「跟随模板中 / 独立文档」——
	// 编辑者必须随时知道自己在改的是共享模板还是这一个商品，否则会以为
	// 改模板会影响全站（或反之）。
	renderMode := strings.TrimSpace(inst.RenderMode)
	if renderMode != presentationdto.RenderModeDocument {
		renderMode = presentationdto.RenderModeTemplate
	}
	renderModeLabel := "跟随模板中（模板更新会同步到这里）"
	if renderMode == presentationdto.RenderModeDocument {
		renderModeLabel = "独立文档（只影响这个商品）"
	}
	metaJSON, _ := json.Marshal(gin.H{
		"target": workbenchTargetOf(EditTargetInstance), "pageId": inst.ID,
		"saveBase": "instance", "entityType": inst.EntityType, "entityId": inst.EntityID,
		"projectId": inst.ProjectID, "draftPath": "", "instanceMode": true,
		"renderMode": renderMode,
	})
	schemas, serr := builder.ComponentSchemas()
	if serr != nil {
		c.String(http.StatusInternalServerError, "组件 schema 生成失败")
		return
	}
	schemasJSON, _ := json.Marshal(schemas)
	c.HTML(http.StatusOK, "workbench/layout", gin.H{
		"title": "自定义商品页", "pageId": inst.ID, "isBlock": false, "isTemplate": true,
		// 双轨状态条（layout.html 据 isset(.instanceMode) 渲染）
		"instanceMode": true, "renderMode": renderMode, "renderModeLabel": renderModeLabel,
		"document": string(documentJSON), "meta": string(metaJSON), "schemas": string(schemasJSON),
		"previewQS": "instance=" + inst.ID + "&entityType=" + inst.EntityType +
			"&entityId=" + inst.EntityID + "&editor=1&projectId=" + inst.ProjectID,
	})
}

// InstanceSave POST /workbench/instance/save：保存实例覆盖文档并重建发布。
// 只改本实例（override_document + 重编译发布），不影响共享模板（docs/04-C）。
func (h *Handle) InstanceSave(c *gin.Context) {
	if h.instances == nil {
		c.String(http.StatusServiceUnavailable, "实例编辑能力未装配")
		return
	}
	var body struct {
		ID            string          `json:"id"`
		ProjectID     string          `json:"projectId"`
		DraftDoc      json.RawMessage `json:"draftDocument"`
		ConfirmDetach bool            `json:"confirmDetach"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ID) == "" || len(body.DraftDoc) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "保存参数不完整"})
		return
	}
	req := &presentationdto.SaveOverrideReq{
		InstanceID: strings.TrimSpace(body.ID),
		ProjectID:  body.ProjectID,
		Document:   body.DraftDoc,
		// 前端在收到 409（会放弃模板同步）并确认后重试时带上它。
		ConfirmDetach: body.ConfirmDetach,
	}
	res, err := h.instances.SaveOverrideDocument(c.Request.Context(), req)
	if err != nil {
		// 需要确认才能转入独立文档：回 409 + JSON，由前端弹确认后重试。
		// 顺序是「先请求、再确认」，因为判据（文档结构是否真的变了）只有服务端算得准。
		if strings.TrimSpace(err.Error()) == presentationenums.ErrDetachConfirmRequired {
			c.JSON(http.StatusConflict, gin.H{
				"code": 409,
				"message": "这次改动会让本商品转为独立文档：之后模板更新不再同步到这里；" +
					"想回到跟随时，在商品详情页点「重新套用预设」即可。继续保存？",
			})
			return
		}
		// 其余失败一律归口文案：编译/校验原文带节点路径与模板片段，只进日志。
		logger.Scene("workbench").With("path", c.Request.URL.Path).Error(err, "实例文档保存失败")
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": workbenchenums.MsgCompileFailed})
		return
	}
	mode := presentationdto.RenderModeTemplate
	if res != nil && res.RenderMode != "" {
		mode = res.RenderMode
	}
	// data.renderMode 让前端保存成功后即时把状态条切成「独立文档」。
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "", "data": gin.H{"renderMode": mode}})
}
