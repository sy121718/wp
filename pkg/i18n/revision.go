package i18n

// revision.go — 文案资源版本号（docs/06-D §10.4 依赖追踪）。
//
// 构建期把该版本号写进 Artifact Manifest 的 i18n 依赖条目
// （pipeline.I18NDependency）：改文案 → revision 变化 → 依赖比对不等 → 触发重建。
//
// 数据源优先级：
//  1. sys_i18n_revision（迁移 056 的单行单调递增版本号，若写入口维护它则最准确）；
//  2. max(sys_i18n.update_time)（兜底：任何词条写入都会推进该值）；
//  3. 两者都不可用时返回空串，调用方按「无 revision」处理（不阻断构建）。
//
// 本函数只读查询，不修改缓存/加载逻辑；构建期每页一次查询，代价可接受。

import (
	"fmt"
	"time"

	"go_wp/pkg/database"
)

// Revision 返回文案资源版本号（查询失败返回空串）。
func Revision() string {
	db, err := database.GetDB()
	if err != nil || db == nil {
		return ""
	}
	var rev struct {
		Revision int64 `gorm:"column:revision"`
	}
	if err = db.Table("sys_i18n_revision").Select("revision").Where("id = ?", 1).Scan(&rev).Error; err == nil && rev.Revision > 0 {
		return fmt.Sprintf("i18n-rev-%d", rev.Revision)
	}
	var latest *time.Time
	if err = db.Table("sys_i18n").Select("max(update_time) AS latest").Scan(&latest).Error; err != nil || latest == nil {
		return ""
	}
	return "i18n-max-" + latest.UTC().Format(time.RFC3339Nano)
}
