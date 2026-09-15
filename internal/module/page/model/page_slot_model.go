package pagemodel

// page_slot_model.go — 系统页面槽位（page_site_slots 表）的持久化。
//
// 槽位回答的是「引擎该去哪找某个页面」：购物车里的「去结算」、下单后的「查看订单」、
// 登录页与注册页的互跳。绑的是**页面 id**（uuid，不可变），不是 URL ——
// 改 URL 是页面的常规操作（draft_path / active_path 都是可变列），绑 id 之后链接自动跟着走。
//
// 表隔离：page_id 只存 uuid 值，不加外键（跨模块表互不关联查询）；
// 「这个页面存不存在、属不属于本工程」由本模块 service 校验。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

const tableNamePageSiteSlots = "page_site_slots"

// SiteSlotEntity 对应 page_site_slots 表：一个工程里「某个角色由哪个页面担任」。
type SiteSlotEntity struct {
	ID         int64     `gorm:"column:id;type:bigint;primaryKey"`
	ProjectID  string    `gorm:"column:project_id;type:uuid;not null"`
	Slot       string    `gorm:"column:slot;type:text;not null"`
	PageID     string    `gorm:"column:page_id;type:uuid;not null"`
	CreateTime time.Time `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time `gorm:"column:updated_at;not null"`
}

func (SiteSlotEntity) TableName() string { return tableNamePageSiteSlots }

// SiteSlotDB 返回已绑定 page_site_slots 表的 GORM 实例。
func (m *Model) SiteSlotDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SiteSlotEntity{})
}

// ListSiteSlots 按工程列出全部槽位绑定（未绑定的槽位不出现在结果里）。
func (m *Model) ListSiteSlots(ctx context.Context, projectID string) (list []SiteSlotEntity, err error) {
	if projectID == "" {
		return nil, nil
	}
	err = m.SiteSlotDB(ctx).Where("project_id = ?", projectID).Find(&list).Error
	return list, err
}

// UpsertSiteSlot 绑定槽位（同工程同槽位已有绑定时原地换页）。
//
// 写成「查后写」而不是数据库 upsert：唯一索引是 (project_id, slot)，冲突时既要改 page_id
// 又要保留 create_time，ON CONFLICT DO UPDATE 只省一次往返；而这个入口的调用频率是
// 「人工点保存」，不是热路径 —— 可读性更值钱。
func (m *Model) UpsertSiteSlot(ctx context.Context, e *SiteSlotEntity) (err error) {
	var existing SiteSlotEntity
	err = m.SiteSlotDB(ctx).Where("project_id = ? AND slot = ?", e.ProjectID, e.Slot).First(&existing).Error
	switch {
	case err == nil:
		return m.SiteSlotDB(ctx).Where("id = ?", existing.ID).Updates(map[string]any{
			"page_id":    e.PageID,
			"updated_at": e.UpdatedAt,
		}).Error
	case errors.Is(err, gorm.ErrRecordNotFound):
		return m.SiteSlotDB(ctx).Create(e).Error
	default:
		return err
	}
}

// DeleteSiteSlot 解绑槽位；返回受影响行数（0 = 本来就没绑）。
func (m *Model) DeleteSiteSlot(ctx context.Context, projectID, slot string) (n int64, err error) {
	res := m.SiteSlotDB(ctx).Where("project_id = ? AND slot = ?", projectID, slot).Delete(&SiteSlotEntity{})
	return res.RowsAffected, res.Error
}

// ListSiteSlotsByPage 查某个页面被哪些槽位引用（删除页面时的引用提示）。
func (m *Model) ListSiteSlotsByPage(ctx context.Context, pageID string) (list []SiteSlotEntity, err error) {
	if pageID == "" {
		return nil, nil
	}
	err = m.SiteSlotDB(ctx).Where("page_id = ?", pageID).Find(&list).Error
	return list, err
}

// FindPagesByIDs 按 id 批量取**未删除**的页面元数据（排除大字段草稿）。
//
// 返回结果里没有的 id = 页面不存在、已删或已软删 —— 调用方据此判「绑定悬空」，
// 不能把「查不到」当成「页面没标题」这类软失败。
func (m *Model) FindPagesByIDs(ctx context.Context, ids []string) (list []PageEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.DB(ctx).Omit("draft_document").
		Where("id IN ? AND deleted_at IS NULL", ids).Find(&list).Error
	return list, err
}

// MarkStaleForProject 把该工程全部未删除页面标记为待重建。
//
// 用于「页面文档之外的、被构建期读进去的配置」发生变更：槽位绑定、槽位指向页面的 URL。
// 精确影响集合要构建期才知道（文档里没有「我引用了哪些槽位」的声明），
// 所以按工程全量标记 —— 与界面文案变更（MarkStaleForI18n）同一取舍；
// 这些入口的频率都是「人工点保存」，不是热路径。
// stale=true 幂等，重复标记无副作用。
func (m *Model) MarkStaleForProject(ctx context.Context, projectID string, at time.Time) (err error) {
	if projectID == "" {
		return nil
	}
	return m.DB(ctx).Where("project_id = ? AND deleted_at IS NULL AND stale = ?", projectID, false).
		Updates(map[string]any{"stale": true, "updated_at": at}).Error
}
