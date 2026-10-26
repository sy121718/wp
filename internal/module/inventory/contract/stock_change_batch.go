// stock_change_batch.go — 带批次号的库存变动（事务透传版）。
//
// 为什么单列一个窄口而不是直接扩 InventoryService：只有一个跨模块调用方需要它
// （purchase 的收货要把本次变动的批次号写进入库单同一行），而 InventoryService 上
// 已经有二十来个方法 —— 往那里加方法等于让每个消费者都多背一条它用不到的能力。
package inventorycontract

import (
	"context"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
)

// BatchStockWriter 在**调用方事务内**做一次库存变动，并返回本次批次号。
//
// 与 InventoryService.ChangeStockTx 的差别只有返回值：那个方法按契约只回 error，
// 调用方要拿批次号就得在提交后回读流水 —— 而「单据已提交、流水查不到」是一个
// 真实的窗口（回读失败就永久缺这个引用）。这里把批次号顺着事务一起交出来。
type BatchStockWriter interface {
	// ChangeStockTxWithBatch 与 ChangeStockTx 逐字同一条路径（同一套校验 / 加锁序 /
	// 流水构造 / 成本写入），只多回一个批次号。不自己开事务，错误原样返回。
	ChangeStockTxWithBatch(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (batchID string, err error)
}
