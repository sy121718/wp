package projectmodel

// locale_model.go — 站点语言清单持久化（多语言 P3，docs/06-D §14 D10）。
//
// project_locales 是「站点有哪几种语言」的唯一真源：顺序、默认标记、启用状态。
// 消费方为路由登记、sitemap 分组、产物 hreflang 与语言切换器（见迁移 064 注释）。

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const tableNameProjectLocales = "project_locales"

// withProjectScope 在事务内设置 RLS 会话变量 app.project_id 后执行 fn。
//
// project_locales 已启用行级安全（迁移 199）：策略谓词读取 app.project_id，
// 未设置或非法时行不可见（fail closed）。变量必须用 set_config(..., is_local => true)
// 在事务内设置：事务结束自动还原，GORM 连接池复用连接时不会把本工程的隔离
// 上下文泄漏给下一个请求。
//
// projectID 先做 uuid 语法校验：策略谓词里有 ::uuid 强转，非法值会把 PG 报错
// （invalid input syntax）暴露给调用方，提前校验让错误归属明确。
func (m *Model) withProjectScope(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	if _, err := uuid.Parse(projectID); err != nil {
		return err
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.project_id', ?, true)", projectID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

// LocaleEntity 对应 project_locales 表。
type LocaleEntity struct {
	ProjectID string    `gorm:"column:project_id;type:uuid;primaryKey"`
	Lang      string    `gorm:"column:lang;type:text;primaryKey"`
	SortOrder int       `gorm:"column:sort_order;not null"`
	IsDefault bool      `gorm:"column:is_default;not null"`
	Enabled   bool      `gorm:"column:enabled;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

func (LocaleEntity) TableName() string { return tableNameProjectLocales }

// LocaleDB 返回已绑定 project_locales 表的 GORM 实例。
func (m *Model) LocaleDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&LocaleEntity{})
}

// ListLocales 列出工程语言清单：默认语言在前，其余按 sort_order、语言升序（输出稳定）。
// RLS（迁移 199）按 app.project_id 过滤，读取必须包在 withProjectScope 的事务里。
func (m *Model) ListLocales(ctx context.Context, projectID string) (list []LocaleEntity, err error) {
	err = m.withProjectScope(ctx, projectID, func(tx *gorm.DB) error {
		return tx.Model(&LocaleEntity{}).
			Where("project_id = ?", projectID).
			Order("is_default DESC, sort_order ASC, lang ASC").Find(&list).Error
	})
	return list, err
}

// ReplaceLocales 全量替换工程语言清单（同一事务删除后写入，聚合内原子组合）。
// 调用方负责校验（至少一种语言、至多一个默认且默认必须启用）。
// ReplaceLocales 全量替换工程语言清单（同一事务删除后写入，聚合内原子组合）。
// 调用方负责校验（至少一种语言、至多一个默认且默认必须启用）。
// RLS 的 WITH CHECK 按 app.project_id 校验写入行，必须包在 withProjectScope 的事务里。
func (m *Model) ReplaceLocales(ctx context.Context, projectID string, rows []LocaleEntity) (err error) {
	return m.withProjectScope(ctx, projectID, func(tx *gorm.DB) error {
		if derr := tx.Where("project_id = ?", projectID).Delete(&LocaleEntity{}).Error; derr != nil {
			return derr
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}
