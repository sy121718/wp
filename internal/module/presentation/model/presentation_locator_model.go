// presentation_locator_model.go — 「实体 → 已上线路径」的查询（BIZ-2 站内搜索）。
//
// 单独一个文件而不是塞进 presentation_model.go：本查询服务的是**访问面**的只读用途
// （搜索结果给链接），与后台实例管理那批查询的读者不同 —— 放一起会让下一个人
// 在管理面查询里顺手复用它，而那条路径需要的是「全部实例」，不是「已上线的」。
package presentationmodel

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// maxLocatorEntityIDs 单次查询的实体 id 上限（片段参数来自 URL，不能无限长）。
const maxLocatorEntityIDs = 50

// ListActiveURLPaths 按实体批量取**已上线**实例的线上路径（键为实体 id）。
//
// 「已上线」= active_artifact_id 指针非空：那是发布成功时写下的产物指针，
// 也是实例能对外提供内容的唯一标志（staged 指针只表示「构建过但没上线」）。
//
// 只返回有路径的行：调用方（搜索片段）按「没有这个实体」处理 —— 未发布 / 已删除的
// 实体不输出链接，宁可不给链接，也不给死链。
func (m *Model) ListActiveURLPaths(ctx context.Context, projectID, entityType, lang string, entityIDs []string) (out map[string]string, err error) {
	out = map[string]string{}
	projectID = strings.TrimSpace(projectID)
	entityType = strings.TrimSpace(entityType)
	lang = strings.TrimSpace(lang)
	if projectID == "" || entityType == "" || lang == "" || len(entityIDs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(entityIDs))
	seen := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if len(ids) >= maxLocatorEntityIDs {
			break
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	// 按请求语言取已激活访问路径（I18N-013）；无 publication 行时回退 url_path。
	var rows []struct {
		EntityID   string `gorm:"column:entity_id"`
		ActivePath string `gorm:"column:active_path"`
	}
	// RLS（迁移 215）：投影的表 presentation_instances 带 FORCE 策略，
	// 访问面的这条读路径同样要设 app.project_id —— 否则换角色后站内搜索恒为空链接。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNamePresentationInstances+" AS i").
			Select(`i.entity_id,
			COALESCE(pp.active_path, i.url_path) AS active_path`).
			Joins(`LEFT JOIN `+tableNamePresentationPublications+` AS pp
			ON pp.presentation_id = i.id AND pp.lang = ?`, lang).
			Where("i.project_id = ? AND i.entity_type = ? AND i.active_artifact_id IS NOT NULL AND i.entity_id IN ?",
				projectID, entityType, ids).
			Find(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if strings.TrimSpace(row.EntityID) == "" || strings.TrimSpace(row.ActivePath) == "" {
			continue
		}
		out[row.EntityID] = row.ActivePath
	}
	return out, nil
}
