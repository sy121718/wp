// Package sysconfigmodel 实现 sysconfig 模块的 sys_config 表持久化（迁移 484）。
//
// sys_config 是**全局**系统配置（按 group_key 分组存 JSON），不属于任何工程：没有
// project_id 列，也不在 RLS 策略（迁移 215）的对象清单里。工程级覆盖在 projects.settings，
// 读取链为「工程值 > 全局默认（本表）> 启动兜底（代码常量）」。
//
// 本模块只碰本表。乐观锁（version 列）是整组读-改-写的唯一并发保护，写入必须走
// UpdateDataWithVersion —— 它的 WHERE 里带 version 条件，禁止在 service 里「先读出来
// 算完再写回」（AGENTS.md「写操作的事务与回滚」）。
package sysconfigmodel

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

const tableNameSysConfig = "sys_config"

// 分组状态（与迁移 484 的 status 列口径一致）。
const (
	// StatusDisabled 禁用：后台保留该组，但读取侧不生效。
	StatusDisabled = 0
	// StatusEnabled 启用。
	StatusEnabled = 1
)

// JSONMap 可序列化的 JSON 对象字段（与其它模块同名类型是有意重复：跨模块不得 import 对方 model）。
type JSONMap map[string]any

// Value 落库：整组配置按 JSON 对象存 JSONB 列。
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan 读库。
func (j *JSONMap) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("JSONMap Scan: 类型不是 []byte")
	}
	return json.Unmarshal(bytes, j)
}

// SysConfigEntity 对应 sys_config 表（一 row = 一个配置分组）。
//
// 不声明列型（除 jsonb 这类 gorm 无法推断的映射）：列型真相在迁移 484。
type SysConfigEntity struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	GroupKey   string    `gorm:"column:group_key"`
	GroupName  string    `gorm:"column:group_name"`
	ConfigData JSONMap   `gorm:"column:config_data;type:jsonb"`
	Remark     string    `gorm:"column:remark"`
	Status     int       `gorm:"column:status"`
	Version    int64     `gorm:"column:version"`
	CreateBy   int64     `gorm:"column:create_by"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateBy   int64     `gorm:"column:update_by"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

// TableName 表名。
func (SysConfigEntity) TableName() string { return tableNameSysConfig }

// Model sys_config 表数据访问（Repository）。
type Model struct {
	db *gorm.DB
}

// NewSysConfigModel 构造。
func NewSysConfigModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 sys_config 表的 GORM 实例。
//
// 裸句柄是 model 的内部实现细节，只允许被本文件的仓储方法消费（service 调用它即越界，
// 由 scripts/check-service-db-boundary.sh 拦截）。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SysConfigEntity{})
}

// FindByKey 按分组键取一行；不存在返回 (nil, nil)（「没有这一组」不是错误，
// 由调用方决定是报错还是回退默认值）。
func (m *Model) FindByKey(ctx context.Context, groupKey string) (*SysConfigEntity, error) {
	var e SysConfigEntity
	err := m.DB(ctx).Where("group_key = ?", groupKey).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListByStatus 按状态列出全部分组（status < 0 表示不过滤）；按分组键排序保证输出稳定。
func (m *Model) ListByStatus(ctx context.Context, status int) (rows []SysConfigEntity, err error) {
	q := m.DB(ctx)
	if status >= 0 {
		q = q.Where("status = ?", status)
	}
	err = q.Order("group_key ASC").Find(&rows).Error
	return rows, err
}

// UpdateDataWithVersion 按乐观锁整组更新一行：
//
//	UPDATE sys_config SET config_data = …, version = version + 1, …
//	 WHERE group_key = ? AND version = ?
//
// 返回受影响行数。**0 行有两种成因**（版本不符 / 这一组不存在），本层不区分、也不重试：
// 分开报是调用方的事（见 service.SetGroup 的归因）。守卫写进 WHERE 而不是先 SELECT
// 后比对，是因为「先读出来算完再写回」在并发下必然丢更新，而这里的 0 行就是丢更新
// 被拦住的那一刻。
//
// update_time 用数据库 now()：应用进程时钟与库时钟不一致时，写进去的时间会与
// 「行是什么时候变的」这件事对不上（update_by 同理由调用方传入，不在这里补会话）。
func (m *Model) UpdateDataWithVersion(ctx context.Context, groupKey string, version int64, data JSONMap, updateBy int64) (int64, error) {
	res := m.DB(ctx).
		Where("group_key = ? AND version = ?", groupKey, version).
		Updates(map[string]any{
			"config_data": data,
			"version":     gorm.Expr("version + 1"),
			"update_by":   updateBy,
			"update_time": gorm.Expr("now()"),
		})
	return res.RowsAffected, res.Error
}

// VersionOf 只读当前版本号（存在性 + 版本）。
//
// 唯一用途是**写失败后的冲突归因**：0 行到底是「版本不符」还是「这一组不存在」，
// 决定给操作者哪一句话。它不参与任何写决策 —— 拿它的结果去写回就是丢更新。
func (m *Model) VersionOf(ctx context.Context, groupKey string) (version int64, found bool, err error) {
	var row struct {
		Version int64 `gorm:"column:version"`
	}
	terr := m.DB(ctx).Select("version").Where("group_key = ?", groupKey).Take(&row).Error
	if errors.Is(terr, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if terr != nil {
		return 0, false, terr
	}
	return row.Version, true, nil
}
