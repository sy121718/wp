// membership_entitlement_model.go — 权益表（membership_entitlements）的仓储。
//
// 这张表**没有 project_id**（作用域经 tier_id 传递），因此不在迁移 462 的 RLS 清单里，
// 本文件的方法也就不带工程作用域 —— 但它们的调用点必须已经先拿到「作用域内可见的 tier」：
// service 的规矩是先 GetTier(projectID, tierID) 命中，才允许动该 tier 的权益。
// 少了那一步，「拿别人的 tier_id 来改权益」会一路成功（表本身没有第二道防线）。
package membershipmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// EntitlementEntity membership_entitlements 的一行。
//
// ValueInt 是**按 kind 解释的整数**，不是通用数值：免运费取 0/1，折扣取 1..100。
// 取值域由迁移 462 的 ck_membership_entitlements_value 承载（应用层先判一次只为给出可行动的说法）。
type EntitlementEntity struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	TierID     int64     `gorm:"column:tier_id"`
	Kind       string    `gorm:"column:kind"`
	ValueInt   int64     `gorm:"column:value_int"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

// TableName 显式绑定表名。
func (EntitlementEntity) TableName() string { return "membership_entitlements" }

// ListByTierTx 在调用方事务内列出某等级的权益（按 kind 升序 —— 读起来稳定）。
func (m *Model) ListByTierTx(ctx context.Context, tx *gorm.DB, tierID int64) (list []*EntitlementEntity, err error) {
	err = tx.WithContext(ctx).Model(&EntitlementEntity{}).
		Where("tier_id = ?", tierID).
		Order("kind ASC").
		Find(&list).Error
	return list, err
}

// ListByTierIDsTx 在调用方事务内批量列出多个等级的权益（后台列表用：避免每行一次查询的 N+1）。
//
// 一次 IN 查询而不是循环：等级数量在合理建站形态下是十几个，
// 但列表页的 N+1 会随等级数线性增长，而这正是「页面越用越慢」的常见起点。
//
// 必须在**同一个事务**里查：写路径（CreateTier / SaveEntitlements）要在落库后立刻读回
// 「库里的最终值」组装响应 —— 走事务外的连接会读到写之前的快照，
// 于是响应里是旧权益而库里是新权益（保存成功但页面显示旧值，刷新一下才对）。
func (m *Model) ListByTierIDsTx(ctx context.Context, tx *gorm.DB, tierIDs []int64) (out map[int64][]*EntitlementEntity, err error) {
	out = map[int64][]*EntitlementEntity{}
	if len(tierIDs) == 0 {
		return out, nil
	}
	var rows []*EntitlementEntity
	if err = tx.WithContext(ctx).Model(&EntitlementEntity{}).
		Where("tier_id IN ?", tierIDs).
		Order("tier_id ASC, kind ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TierID] = append(out[r.TierID], r)
	}
	return out, nil
}

// ReplaceForTierTx 在调用方事务内**全量替换**某等级的权益：先删该 tier 的全部行，再插新行。
//
// 「全量」的语义是有意的：清单就是最终状态，没列出的 kind 会被删掉 ——
// 这样「取消折扣」不需要一个单独的删除接口，也不会留下「页面上没显示但数据库里还在生效」
// 的权益行（那会让订单侧继续按已被运营删掉的折扣算钱）。
//
// 删与插必须在同一事务里：分两次写而失败一半，会得到「折扣没了但免运费也没了」
// 或「旧折扣与新折扣同时在库里」——前者少算、后者多算，都是钱。
func (m *Model) ReplaceForTierTx(ctx context.Context, tx *gorm.DB, tierID int64, rows []*EntitlementEntity) error {
	if err := tx.WithContext(ctx).Model(&EntitlementEntity{}).
		Where("tier_id = ?", tierID).
		Delete(&EntitlementEntity{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now()
	for _, r := range rows {
		r.TierID = tierID
		r.CreateTime = now
		r.UpdateTime = now
	}
	return tx.WithContext(ctx).Model(&EntitlementEntity{}).Create(&rows).Error
}

// DeleteForTierTx 在事务内删除某等级的全部权益（等级被软删时不必调 ——
// 权益行留着无害，硬删反而让「误删等级后恢复」丢配置）。
func (m *Model) DeleteForTierTx(ctx context.Context, tx *gorm.DB, tierID int64) (int64, error) {
	res := tx.WithContext(ctx).Model(&EntitlementEntity{}).
		Where("tier_id = ?", tierID).
		Delete(&EntitlementEntity{})
	return res.RowsAffected, res.Error
}
