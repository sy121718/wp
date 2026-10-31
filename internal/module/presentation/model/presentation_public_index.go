package presentationmodel

import (
	"context"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
	"strings"
)

// ListPublicEntityPaths 只读真实单语言激活记录，不回退实例的候选 URL。
//
// 为什么没有 ORDER BY（PRD-007）：返回值是 map，两个消费者（商品公开目录、站内搜索）
// 都只需要「实体 → 路径」的对应关系，谁都不依赖行顺序 —— 商品目录自己把 id 排序后
// 当数组参数发（`= ANY((?::text[])::uuid[])`），搜索按命中集合取用。而这里每请求搬的是
// **本工程已上线实体全集**，在库侧对全集排序（ORDER BY i.id）是没有消费者的成本：
// 十万级商品时它是每个目录请求都要付的一次排序，换不来任何可见差异。去掉它不改变结果。
//
// 分母要说清楚：本查询仍返回全集（无 LIMIT）。真正的「上线条件进入分页路径」需要
// 跨模块分页口径（DEC-06 待定），不在这一层假装完成；这里只去掉可去的那部分成本。
func (m *Model) ListPublicEntityPaths(ctx context.Context, projectID, entityType, lang string) (map[string]string, error) {
	out := map[string]string{}
	if projectID == "" || entityType == "" || lang == "" {
		return out, nil
	}
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var rows []struct {
			EntityID   string
			ActivePath string
		}
		if err := tx.Table(tableNamePresentationInstances+" AS i").
			Select("i.entity_id, pp.active_path").
			Joins("JOIN "+tableNamePresentationPublications+" AS pp ON pp.presentation_id = i.id").
			Where("i.project_id = ? AND i.entity_type = ? AND pp.lang = ? AND pp.artifact_id IS NOT NULL AND pp.active_path <> ''", projectID, entityType, lang).
			Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.EntityID != "" && strings.TrimSpace(row.ActivePath) != "" {
				out[row.EntityID] = row.ActivePath
			}
		}
		return nil
	})
	return out, err
}
