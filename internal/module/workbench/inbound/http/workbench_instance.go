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

	"go_wp/internal/builder"

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
	metaJSON, _ := json.Marshal(gin.H{
		"target": workbenchTargetOf(EditTargetInstance), "pageId": inst.ID,
		"saveBase": "instance", "entityType": inst.EntityType, "entityId": inst.EntityID,
		"projectId": inst.ProjectID, "draftPath": "", "instanceMode": true,
	})
	schemas, serr := builder.ComponentSchemas()
	if serr != nil {
		c.String(http.StatusInternalServerError, "组件 schema 生成失败")
		return
	}
	schemasJSON, _ := json.Marshal(schemas)
	c.HTML(http.StatusOK, "workbench/layout", gin.H{
		"title": "自定义商品页", "pageId": inst.ID, "isBlock": false, "isTemplate": true,
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
		ID        string          `json:"id"`
		ProjectID string          `json:"projectId"`
		DraftDoc  json.RawMessage `json:"draftDocument"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ID) == "" || len(body.DraftDoc) == 0 {
		c.String(http.StatusBadRequest, "保存参数不完整")
		return
	}
	req := &presentationdto.SaveOverrideReq{
		InstanceID: strings.TrimSpace(body.ID),
		ProjectID:  body.ProjectID,
		Document:   body.DraftDoc,
	}
	if _, err := h.instances.SaveOverrideDocument(c.Request.Context(), req); err != nil {
		c.String(http.StatusUnprocessableEntity, "保存失败")
		return
	}
	c.Status(http.StatusOK)
}
