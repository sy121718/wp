// product_detail_template_panel.go — 商品详情页模板面板的数据组装（详情页与编辑页共用）。
//
// 为什么抽出来：详情页改为**只读**之后，这个面板被两页消费 ——
//
//	· 详情页：只读展示（模式徽标 / 当前模板 / 预览入口 / 未发布的引导语）；
//	· 编辑页：承担写动作（进入自定义 / 编辑模板 / 重新套用预设 / 回滚文档）。
//
// 两处各组装一遍必然分叉（典型形态：一边算了「影响 N 个商品」、另一边没算，
// 或者一边给了 Snapshots、另一边回滚下拉是空的），而分叉不会编译失败。
package producthttp

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
)

// detailTemplatePanel 组装详情页模板面板的数据（docs/04-C-instance-override.md §5）。
//
// 三态：未绑定（引导首次发布）/ 已绑定（预览 + 进入自定义）/ 能力未装配（降级提示）。
// 未装配模板能力（templates / instances 未注入）或没给商品 id 时，只回 {"Avail": false}，
// 模板据 isset 给降级提示，页面照常渲染。
func (h *productPageHandle) detailTemplatePanel(ctx context.Context, selected, productID string) gin.H {
	tplPanel := gin.H{"Avail": false}
	if h.templates == nil || h.instances == nil || strings.TrimSpace(productID) == "" {
		return tplPanel
	}
	tplPanel["Avail"] = true
	if inst, ierr := h.instances.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: productEntityType, EntityID: productID, ProjectID: selected,
	}); ierr == nil && inst != nil {
		tplPanel["InstanceID"] = inst.ID
		tplPanel["TemplateID"] = inst.TemplateID
		tplPanel["Published"] = inst.Status == "published" || inst.Status == "active"
		tplPanel["URLPath"] = inst.URLPath
		tplPanel["PreviewQS"] = "template=" + inst.TemplateID + "&entityType=product&entityId=" +
			productID + "&projectId=" + selected
		// 双轨（迁移 282）：模式徽标 + 两个模式的入口分流。
		// document：可进入自定义、可重新套用预设、可按历史快照回滚；
		// template：布局的正确修改位置是模板 —— 按钮写成「编辑模板（影响 N 个商品）」，
		// 影响面用真实计数（含该模板下 template 模式的实例数），不写就让用户凭猜。
		isDoc := inst.RenderMode == presentationdto.RenderModeDocument
		tplPanel["RenderMode"] = presentationdto.RenderModeTemplate
		if isDoc {
			tplPanel["RenderMode"] = presentationdto.RenderModeDocument
		}
		tplPanel["IsDocumentMode"] = isDoc
		// 「预设有新版本」：document 模式不会自动跟随模板，只能靠快照记录的
		// 模板版本与模板最新版比对来提示（判定依据由 toResp 给出）。
		if isDoc {
			if tpl, rerr := h.templates.ResolveTemplate(ctx, productEntityType); rerr == nil && tpl != nil &&
				inst.SourceTemplateVersionID != "" && tpl.VersionID != inst.SourceTemplateVersionID {
				tplPanel["PresetUpdated"] = true
			}
			if h.modePort != nil {
				if snaps, serr := h.modePort.ListSnapshots(ctx, &presentationdto.ListSnapshotsReq{
					InstanceID: inst.ID, ProjectID: selected, Limit: 8,
				}); serr == nil {
					tplPanel["Snapshots"] = snaps
				}
			}
		}
		if h.modePort != nil {
			if counts, cerr := h.modePort.CountByTemplate(ctx, &presentationdto.CountByTemplateReq{
				TemplateID: inst.TemplateID, ProjectID: selected,
			}); cerr == nil && counts != nil {
				tplPanel["AffectedCount"] = counts.TemplateMode
			}
		}
	}
	if rows, terr := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: productEntityType}); terr == nil {
		tplPanel["Templates"] = rows
	}
	return tplPanel
}
