package sysconfigmodel

// sysconfig_tx.go — 多组保存的事务支持。
//
// 为什么要它：后台系统设置页一次提交会改**两个组**（i18n 与 trade）。两次整组保存
// 分处两条独立语句，任一步失败若只回滚自己，就会出现「默认语言改了、默认货币没改」
// 这种半截状态，而界面上两处是同一张表单的提交（AGENTS.md：涉及两处及以上持久化写入
// 必须落在同一事务里）。
//
// 形状与 page 模块的 TransactionScoped 一致：句柄由 model 往下传，service 决定事务边界。

import (
	"context"

	"gorm.io/gorm"
)

// Transaction 在单个事务里执行 fn。fn 拿到的是已绑定事务的句柄，供本 model 的
// …Tx 方法消费（service 不得直接拼查询）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// UpdateDataWithVersionTx 与 UpdateDataWithVersion 同语义，但跑在调用方给的事务里。
//
// 乐观锁守卫仍在 WHERE（`group_key = ? AND version = ?`）：多组一起保存时，任意一组
// 版本不符都会返回 0 行，由 service 决定整批回滚 —— 「部分组写成功」正是要避免的状态。
func (m *Model) UpdateDataWithVersionTx(tx *gorm.DB, groupKey string, version int64, data JSONMap, updateBy int64) (int64, error) {
	res := tx.Model(&SysConfigEntity{}).
		Where("group_key = ? AND version = ?", groupKey, version).
		Updates(map[string]any{
			"config_data": data,
			"version":     gorm.Expr("version + 1"),
			"update_by":   updateBy,
			"update_time": gorm.Expr("now()"),
		})
	return res.RowsAffected, res.Error
}

// VersionOfTx 在事务里读某组当前版本（写失败后的归因用，不参与写决策）。
func (m *Model) VersionOfTx(tx *gorm.DB, groupKey string) (version int64, found bool, err error) {
	var row struct {
		Version int64 `gorm:"column:version"`
	}
	terr := tx.Model(&SysConfigEntity{}).Select("version").Where("group_key = ?", groupKey).Take(&row).Error
	if terr == gorm.ErrRecordNotFound {
		return 0, false, nil
	}
	if terr != nil {
		return 0, false, terr
	}
	return row.Version, true, nil
}
