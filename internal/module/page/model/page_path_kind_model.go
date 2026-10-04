package pagemodel

// page_path_kind_model.go — 按线上访问路径批量反查页面类型（只读）。
//
// 消费者是「文章页浏览量排行」这一类跨模块聚合：analytics 只认 path，
// 要判断某个路径是不是文章页，只能问 page 模块（表隔离：analytics 读不到 pages 表）。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// PagePathKindRow 一条「路径 → 页面类型」的投影行。
type PagePathKindRow struct {
	Path string `gorm:"column:active_path"`
	Kind string `gorm:"column:kind"`
}

// KindsOfPaths 在**本工程内**按**已发布的访问路径**批量反查页面类型。
//
// 用 active_path 而不是 draft_path：analytics 记的是线上真实访问过的路径，
// 没发布的草稿路径不会有人访问到 —— 它在结果里查不到是正确行为，不是漏数据。
//
// 已删除的页面同样不出现。这带来的取舍是「历史访问量里的已删页面会被排除」，
// 这是刻意的：把已下线内容的访问量算进「文章页浏览量」，这个数字就永远降不下来，
// 而运营看到一个包含已下线内容的数字只会困惑。要统计历史全量得走另做一条
// 「含已删页面」的查询，不能靠放宽这里的谓词（那会顺手把跨工程与其他 kind 也放进来）。
//
// paths 为空返回空 map 且**不查库**：这不是错误 —— 调用方（路径排行）本来就可能是空的。
// 入参里的重复项与空串会被剔除：它们匹配不到任何页面，只会让 IN 列表多几个没意义的值。
//
// projectID 必填：pages 带 FORCE 策略，缺作用域在换非超级角色后是静默空集，
// 表现为「一个路径都认不出来」而没有任何报错。
func (m *Model) KindsOfPaths(ctx context.Context, projectID string, paths []string) (kinds map[string]string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	cleaned := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		cleaned = append(cleaned, p)
	}
	// 空结果也返回**非 nil** 的 map：调用方直接下标取值即可，不必先判 nil
	//（nil map 也能读，但 map 为空与「查询没跑」在调试时是两回事）。
	kinds = make(map[string]string, len(cleaned))
	if len(cleaned) == 0 {
		return kinds, nil
	}
	var rows []PagePathKindRow
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("active_path", "kind").
			Where("project_id = ? AND deleted_at IS NULL AND active_path IN ?", projectID, cleaned).
			Find(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if strings.TrimSpace(r.Path) == "" {
			continue
		}
		kinds[r.Path] = r.Kind
	}
	return kinds, nil
}
