// ai_model.go — AI 供应商表的访问单元（Repository）。
//
// 本文件只做 ai_provider 一张表的 CRUD：条件 / 分页 / 排序以参数传入，方法内不写死
// 业务条件、不多表关联；业务规则（密钥加解密、内置默认清单、乐观锁归因）一律留在 service。
//
// 为什么只有一张表：模型目录放在 ai_provider.config_data.models（整组替换的编辑单元），
// 目录行没有跨表引用、也不需要独立生命周期 —— 见迁移 511 的文件头说明。
package aimodel

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// JSONMap config_data 的读写形态。
//
// 与 sysconfig 的 JSONMap 是同形**两份独立实现**（模块间不跨依赖）：跨模块共用需要
// 一个公共包，那超出本次改动范围，留待后续按需沉淀。
type JSONMap map[string]any

// Value 写库：map → jsonb。
func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan 读库：jsonb → map。
func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = nil
		return nil
	}
	raw, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("JSONMap Scan: 类型不是 []byte")
	}
	if len(raw) == 0 {
		*m = JSONMap{}
		return nil
	}
	var out JSONMap
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

// AIProviderEntity ai_provider 一行。
//
// 不声明列型（除 jsonb）：表结构来自迁移 511，实体只描述映射 —— 列类型写死在 tag 里
// 会让「改了迁移忘了改实体」变成静默漂移。
type AIProviderEntity struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	ProviderKey  string    `gorm:"column:provider_key"`
	DisplayName  string    `gorm:"column:display_name"`
	BaseURL      string    `gorm:"column:base_url"`
	Protocol     string    `gorm:"column:protocol"`
	APIKeyCipher string    `gorm:"column:api_key_cipher"`
	Status       int       `gorm:"column:status"`
	Sort         int       `gorm:"column:sort"`
	ConfigData   JSONMap   `gorm:"column:config_data;type:jsonb"`
	Version      int64     `gorm:"column:version"`
	CreateBy     int64     `gorm:"column:create_by"`
	CreateTime   time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateBy     int64     `gorm:"column:update_by"`
	UpdateTime   time.Time `gorm:"column:update_time;autoUpdateTime"`
}

// TableName 固定表名（不随结构体名变化）。
func (AIProviderEntity) TableName() string { return "ai_provider" }

// Model ai_provider 的访问单元。
type Model struct {
	db *gorm.DB
}

// NewAIModel 构造访问单元。
func NewAIModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回绑定了 ai_provider 表的会话。
//
// 只允许本文件（以及同属本访问单元的文件）消费；service 调它就是越界，
// 由 scripts/check-service-db-boundary.sh 拦截。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AIProviderEntity{})
}

// FindByID 按主键查一行；不存在返回 (nil, nil)（让调用方区分「没查到」与「查失败」）。
func (m *Model) FindByID(ctx context.Context, id int64) (*AIProviderEntity, error) {
	var e AIProviderEntity
	err := m.DB(ctx).Where("id = ?", id).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// FindByKey 按 provider_key 查一行；不存在返回 (nil, nil)。
func (m *Model) FindByKey(ctx context.Context, providerKey string) (*AIProviderEntity, error) {
	var e AIProviderEntity
	err := m.DB(ctx).Where("provider_key = ?", providerKey).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListAll 列出全部供应商（按 sort 升序、同值按 id 升序）。
//
// 不分页：供应商是「后台设置里的几行」，不是业务列表 —— 分页只会让「模型目录」
// 这种跨供应商的视图变复杂。真要分页时再按 PageParams 传参进来。
func (m *Model) ListAll(ctx context.Context) ([]AIProviderEntity, error) {
	rows := make([]AIProviderEntity, 0, 8)
	if err := m.DB(ctx).Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Create 插入一行（ID / 时间列由数据库回填）。
func (m *Model) Create(ctx context.Context, e *AIProviderEntity) error {
	return m.DB(ctx).Create(e).Error
}

// UpdateFields 带乐观锁的字段更新，返回受影响行数。
//
// version 条件写在 WHERE 里：0 行 = 「不存在」或「版本已被别人推进」，不在此处区分
// （归因要再查一次，属于业务判断，见 service.updateWithVersion）。
// version / update_by / update_time 由本方法统一维护，调用方只传业务字段。
func (m *Model) UpdateFields(ctx context.Context, id, version int64, fields map[string]any, updateBy int64) (int64, error) {
	updates := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		updates[k] = v
	}
	updates["version"] = gorm.Expr("version + 1")
	updates["update_by"] = updateBy
	updates["update_time"] = gorm.Expr("now()")

	res := m.DB(ctx).Where("id = ? AND version = ?", id, version).Updates(updates)
	return res.RowsAffected, res.Error
}

// DeleteByID 按主键删除，返回受影响行数。
func (m *Model) DeleteByID(ctx context.Context, id int64) (int64, error) {
	res := m.DB(ctx).Where("id = ?", id).Delete(&AIProviderEntity{})
	return res.RowsAffected, res.Error
}

// VersionOf 只读版本号，用于更新冲突的归因（不存在返回 found=false）。
func (m *Model) VersionOf(ctx context.Context, id int64) (version int64, found bool, err error) {
	var e AIProviderEntity
	terr := m.DB(ctx).Select("id", "version").Where("id = ?", id).Take(&e).Error
	if errors.Is(terr, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if terr != nil {
		return 0, false, terr
	}
	return e.Version, true, nil
}
