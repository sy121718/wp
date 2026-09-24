// inventory_changes.go — 主数据变更记录接入（issue #19）。
//
// 本文件是库存模块与 masterdata 模块之间的**唯一适配面**：货源资料（编码 / 名称 /
// 类型 / 关联方 / 结算价 / 状态 / 对接配置）的字段级留痕在这里定义白名单与格式化，
// masterdata 只做 diff 与落库。
//
// 与库存流水的分工（验收 5）：货源资料是**配置**，改动进主数据变更记录；
// 数量增减进库存流水（inventory_stock_movements）。同一张货源的「停用」与
// 「入库 +10」分别落在两处，谁都不替代谁。
package inventoryservice

import (
	"context"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
)

// sourceChangeSnapshot 货源主数据的字段白名单快照。
//
// 只有结构化列进快照：类型 / 关联方 / 结算价 / 状态都是报表与采购单要按它们取数或
// 校验的字段，改一次必须留痕；对接配置（config）是自由形状的 JSON，
// 用压缩后的文本参与 diff（等价 JSON 不产生假记录）。
func sourceChangeSnapshot(e *inventorymodel.SourceEntity) masterdatacontract.FieldSnapshot {
	if e == nil {
		return nil
	}
	return masterdatacontract.NewSnapshot(
		"code", e.Code,
		"name", e.Name,
		"type", e.Type,
		"related_party", masterdatacontract.FormatBool(e.RelatedParty),
		"settle_price", masterdatacontract.FormatPricePtr(e.SettlePrice),
		"status", e.Status,
		"config", masterdatacontract.FormatJSON(e.Config),
		"sort", masterdatacontract.FormatInt(e.Sort),
	)
}

// sourceChangeInput 组装货源级变更输入。
func sourceChangeInput(e *inventorymodel.SourceEntity, action, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if e == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: e.ProjectID, EntityType: masterdataenums.EntityInventorySource,
		EntityID: e.ID, EntityLabel: e.Name,
		Action: action, Origin: masterdataenums.OriginSource, OperatorID: operator,
		Before: before, After: after,
	}
}

// recordChanges 记录主数据变更（端口未注入时空转：纯库存单测路径）。
//
// **新代码优先用 recordChangesTx**：业务行与留痕必须同事务（见下）。本方法留给
// 「这次写操作只有留痕一处写入」、或确实无法进事务的路径。
func (s *Service) recordChanges(ctx context.Context, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChanges(ctx, inputs)
}

// recordChangesTx 在**调用方事务内**记录主数据变更（端口未注入时空转，与上面同口径）。
//
// 与 recordChanges 只差一个参数：写的是调用方的 tx。这个差别是本质的 ——
// master_data_changes 是 append-only（迁移 111 的触发器拒绝 UPDATE / DELETE）：
// 业务行提交了而留痕没提交，等于**永久缺一条审计**，事后只能靠人工补一条「说明性」记录；
// 反过来则是「审计里有、业务没变」。所以只要有主实体写，留痕就必须在同一事务里。
// 先例：masterdata.RecordChangesTx（product 模块的三处写路径同此形状）。
func (s *Service) recordChangesTx(ctx context.Context, tx *gorm.DB, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChangesTx(ctx, tx, inputs)
}
