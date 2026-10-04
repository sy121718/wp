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
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/internal/web/shell"
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
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrInstanceEditNotAssembled))
		return
	}
	ctx := c.Request.Context()
	inst, err := h.instances.Get(ctx, &presentationdto.GetReq{ID: instanceID, ProjectID: c.Query("projectId")})
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrInstanceNotFound))
		return
	}
	// 生效文档：override 优先（toResp 随快照返回），否则实例尚无可编辑文档，
	// 由调用方（商品编辑页）先经 CreateInstance/Rebuild 产生快照再进来。
	document := inst.Document
	if len(document) == 0 {
		// 422 保留（这是「这个实例还不能编辑」的业务状态，不是服务端故障），
		// 文案走 key：硬编码中文在英文站点上不会翻译，且说不清下一步怎么做。
		c.String(http.StatusUnprocessableEntity, workbenchFacingText(c, workbenchenums.ErrInstanceNoDocument))
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
	renderModeLabel := workbenchShortText(c, workbenchenums.ModeFollowTemplate)
	if renderMode == presentationdto.RenderModeDocument {
		renderModeLabel = workbenchShortText(c, workbenchenums.ModeDocument)
	}
	metaJSON, _ := json.Marshal(gin.H{
		"target": workbenchTargetOf(EditTargetInstance), "pageId": inst.ID,
		"saveBase": "instance", "entityType": inst.EntityType, "entityId": inst.EntityID,
		"projectId": inst.ProjectID, "draftPath": "", "instanceMode": true,
		"renderMode": renderMode,
	})
	schemas, serr := builder.ComponentSchemas()
	if serr != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaBuildFailed))
		return
	}
	schemasJSON, _ := json.Marshal(schemas)
	// 与另外三种画布模式一致地走 shell.Prepare：它注入 t（画布模板的取词函数）、
	// csrf_token（workbench.js 的 POST fetch 读它）与 lang。此前这个分支直接传 gin.H，
	// 于是 layout.html 的 `{{ .["csrf_token"] }}` 恒为空串 —— JS 只能靠 sessionStorage 兜底。
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title": workbenchShortText(c, workbenchenums.TitleInstance), "pageId": inst.ID, "isBlock": false, "isTemplate": true,
		// 双轨状态条（layout.html 据 isset(.instanceMode) 渲染）
		"instanceMode": true, "renderMode": renderMode, "renderModeLabel": renderModeLabel,
		"document": string(documentJSON), "meta": string(metaJSON), "schemas": string(schemasJSON),
		"previewQS": "instance=" + inst.ID + "&entityType=" + inst.EntityType +
			"&entityId=" + inst.EntityID + "&editor=1&projectId=" + inst.ProjectID,
		// jsVer 是 layout.html 的**必需键**（脚本 / 样式 / Trix 的缓存版本）：另外三种模式
		//（页面 / 块 / 模板）都给，唯独这里漏了。漏掉的后果与缺陷 B1 同一类，但更早 ——
		// 模板第 17 行就取它，于是实例编辑器整页在 <head> 里中断，只回 445 字节。
		// 回归守卫：public/test/workbench/feature/workbench_layout_render_test.go 的「实例模式」。
		"jsVer": workbenchservice.StaticJSVersion(),
	}))
}

// InstanceSave POST /workbench/instance/save：保存实例覆盖文档并重建发布。
// 只改本实例（override_document + 重编译发布），不影响共享模板（docs/04-C）。
func (h *Handle) InstanceSave(c *gin.Context) {
	if h.instances == nil {
		// 形态与同文件其余分支一致（c.JSON + code/message）：本端点由前端 fetch 消费，
		// api.js 的 send() 直接 r.json()，正文是 text/plain 时解析抛异常 → then 链断掉 →
		// 到不了那句 alert，用户看不到任何提示。文案走归口译文：能力未装配属装配缺陷，
		// 给用户看的只能是受控提示，装配细节不进响应。
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": shell.PageInternalText(c)})
		return
	}
	var body struct {
		ID            string          `json:"id"`
		ProjectID     string          `json:"projectId"`
		DraftDoc      json.RawMessage `json:"draftDocument"`
		ConfirmDetach bool            `json:"confirmDetach"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ID) == "" || len(body.DraftDoc) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": workbenchShortText(c, workbenchenums.ErrSaveParamsIncomplete)})
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
				"code":    409,
				"message": workbenchShortText(c, workbenchenums.ErrDetachConfirmRequired),
			})
			return
		}
		// 其余失败：编译/校验原文带节点路径与模板片段，只进日志。
		// 能归因的（编译失败 / 工程作用域没定下来）给一条说清「没写入」与「怎么修」的文案，
		// 归不了因的给归口文案 —— 判据见 service.InstanceSaveFacingKey。
		logger.Scene("workbench").With("path", c.Request.URL.Path).Error(err, "实例文档保存失败")
		message := workbenchCompileFallbackText(c)
		if key, ok := workbenchservice.InstanceSaveFacingKey(err.Error()); ok {
			if text := workbenchFacingText(c, key); text != "" {
				message = text
			}
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": message})
		return
	}
	mode := presentationdto.RenderModeTemplate
	if res != nil && res.RenderMode != "" {
		mode = res.RenderMode
	}
	// data.renderMode 让前端保存成功后即时把状态条切成「独立文档」。
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "", "data": gin.H{"renderMode": mode}})
}
