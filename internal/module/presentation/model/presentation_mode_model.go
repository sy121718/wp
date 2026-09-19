package presentationmodel

// presentation_mode_model.go — 渲染模式（迁移 282，商品页双轨）与它带来的两个差异化查询。
//
// 两种模式（docs/04-C-instance-override.md）：
//   template：文档 = 绑定模板的文档，每次构建参与；模板更新可全局下发（默认，零回归）；
//   document：文档 = override_document（该商品自己的文档）；模板更新不影响它。
//
// 为什么模式要落列而不是靠「override_document 是否为空」推断：
//   · 「改了又改回去」「重新套用预设后」这两种状态用空/非空推断会漂移；
//   · 模板更新的 stale 传播必须能在 SQL 里分流（只重建 template 模式的实例）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const (
	// RenderModeTemplate 跟随绑定模板（默认）。
	RenderModeTemplate = "template"
	// RenderModeDocument 该商品独立文档（override_document）。
	RenderModeDocument = "document"
)

// IsValidRenderMode 取值是否合法（空值不算合法：由调用方先归一化）。
func IsValidRenderMode(mode string) bool {
	return mode == RenderModeTemplate || mode == RenderModeDocument
}

// NormalizeRenderMode 归一化渲染模式：空值/未知值按 template 处理。
//
// 旧行（282 之前）在迁移里已回填，这里的兜底是为了「测试用 AutoMigrate 建表」与
// 手工插入等不经过迁移的路径：那些行 render_mode 可能为空串，
// 按 template 处理等于保持既有语义（比按 document 处理安全：不会误用空文档覆盖模板）。
func NormalizeRenderMode(mode string) string {
	if strings.TrimSpace(mode) == RenderModeDocument {
		return RenderModeDocument
	}
	return RenderModeTemplate
}

// IsDocumentMode 是否独立文档模式（空值/未知值按 template 处理，见 NormalizeRenderMode）。
func IsDocumentMode(mode string) bool {
	return NormalizeRenderMode(mode) == RenderModeDocument
}

// UpdateInstanceModeTx 事务内切换渲染模式并落独立文档。
//
// 与本次重建的快照/产物/指针同一事务（调用方在 persistBuild 的链内传进来）：
// 若只更新文档而不在同一事务里改模式，会出现「文档已是商品自己的、模式还写着跟随模板」
// 的中间态，下一次模板更新就会按 template 模式把它重建回模板文档 —— 自定义凭空消失。
//
// document 必填（document 模式没有独立文档就无内容可编译）；回到 template 用
// ClearInstanceModeTx（语义不同：那是「重新套用预设」，要清文档）。
func (m *Model) UpdateInstanceModeTx(tx *gorm.DB, projectID, id, renderMode string,
	document json.RawMessage, at time.Time) error {
	if !IsValidRenderMode(renderMode) {
		return errors.New("invalid render mode")
	}
	if renderMode == RenderModeDocument && len(document) == 0 {
		return errors.New("document mode requires document")
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	updates := map[string]any{
		"render_mode": renderMode,
		"update_time": at,
	}
	if renderMode == RenderModeDocument {
		updates["override_document"] = document
	} else {
		// 回到 template 模式必须同时清空独立文档：留下「template 模式 + 非空文档」
		// 的行会让审计与 UI 都读到矛盾状态（看起来像还在自定义）。
		updates["override_document"] = nil
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).
		Updates(updates).Error
}

// ClearInstanceModeTx 事务内回到 template 模式并清除独立文档（「重新套用预设」）。
//
// 两列必须同事务改写：只清文档不改模式会留下「document 模式但文档为空」的行，
// 下次构建回落到模板文档 —— 表现上像套用成功了，模式列却仍写着独立（审计与 UI 都会骗人）。
func (m *Model) ClearInstanceModeTx(tx *gorm.DB, projectID, id string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).
		Updates(map[string]any{
			"render_mode":       RenderModeTemplate,
			"override_document": nil,
			"update_time":       at,
		}).Error
}

// MarkStaleTemplateModeByDependency 只标记 **template 模式** 的受影响实例待重建。
//
// 用于「绑定模板出新版本」这类模板驱动的失效：document 模式的实例有自己的文档，
// 模板换代与它无关（重建反而多余，且会把 stale 徽标打在一张不受影响的页面上）。
// 其余依赖源（导航 / 全局块 / 译文）仍走 MarkStaleByDependency —— 那些改动对两种
// 模式**都**有效，漏掉 document 模式会让「改了导航但商品页不更新」且无任何报错。
//
// 命中口径与 MarkStaleByDependency 逐条一致（活跃或暂存产物声明的依赖），
// 只多一个模式条件；COALESCE 兜住不经过迁移建表的测试数据（空值按 template 语义）。
func (m *Model) MarkStaleTemplateModeByDependency(ctx context.Context, projectID, kind, key string,
	at time.Time) (ids []string, err error) {
	if kind == "" || key == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(`
		WITH affected AS (
			SELECT DISTINCT d.presentation_id AS presentation_id
			FROM presentation_dependencies d
			JOIN presentation_instances p ON p.id = d.presentation_id
			WHERE d.dependency_kind = ?
			  AND d.dependency_key = ?
			  AND p.deleted_at IS NULL
			  AND COALESCE(p.render_mode, 'template') = 'template'
			  AND d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id)
		)
		UPDATE presentation_instances SET stale = true, update_time = ?
		WHERE deleted_at IS NULL AND project_id = ? AND id IN (SELECT presentation_id FROM affected)
		RETURNING id`, kind, key, at, projectID).Scan(&ids).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// CountInstancesByTemplate 按绑定模板统计实例数（两个模式各多少）。
//
// 给 UI 用：「编辑模板（影响 N 个商品）」的影响面必须写在按钮上 —— 数字取错比没有数字
// 更糟（用户会照着一个假的 N 判断风险）。只数未删除的实例。
func (m *Model) CountInstancesByTemplate(ctx context.Context, projectID, templateID string) (
	templateMode, documentMode int64, err error) {
	if strings.TrimSpace(templateID) == "" {
		return 0, 0, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Select("COUNT(*) FILTER (WHERE COALESCE(render_mode, 'template') = 'template') AS template_mode, "+
				"COUNT(*) FILTER (WHERE COALESCE(render_mode, 'template') = 'document') AS document_mode").
			Where("project_id = ? AND template_id = ? AND deleted_at IS NULL", projectID, templateID).
			Row().Scan(&templateMode, &documentMode)
	})
	return templateMode, documentMode, err
}

// ListSnapshots 读取实例的历史快照（新→旧），供「快照级文档回滚」选择目标版本。
//
// 按 create_time 倒序：快照是只增的历史，时间序即版本序（同秒并发建快照时用 id 兜底稳定排序）。
func (m *Model) ListSnapshots(ctx context.Context, instanceID string, limit int) (list []SnapshotEntity, err error) {
	if strings.TrimSpace(instanceID) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	err = m.db.WithContext(ctx).Model(&SnapshotEntity{}).
		Where("presentation_instance_id = ?", instanceID).
		Order("create_time DESC, id DESC").Limit(limit).Find(&list).Error
	return list, err
}
