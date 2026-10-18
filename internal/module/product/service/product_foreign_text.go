package productservice

// product_foreign_text.go — 别模块业务 key 的展示兜底（**只剩对端还没有兜底表的那一批**）。
//
// 本模块 service 一律返回 productenums.Err(key) —— 中文兜底跟着错误声明处走，出口经
// pkg/facing.FactOf 直接拿到 Fallback，不查任何表。inventory / presentation 的 key 还没有
// 自己的 ErrFallbacks，FactOf 只能给出「兜底 = key」，出口便会把裸 key 摆到页面上；
// 这张表就是补那一格。
//
// 为什么归 service 而不是控制器：这些 key 是**本模块 service 调用对端能力时**产生的
// （建 SKU 调 inventory、挂模板调 presentation），谁产生这种错误谁就该说清它对运营是什么意思。
// 控制器只负责取词与出口，不该自带一份跨模块的错误语义表。
//
// 清理条件：对端补上兜底表（enums 里再加一张 map，Err(key) 里取）之后本文件整体删除。
// 它不该长大：新增一个 key 先问「这个错误是谁声明的」—— 本模块的兜底跟着声明处走，
// 别模块的催对端补表或把它包成本模块的错误，别在这里续写第二份映射。

import (
	inventoryenums "go_wp/internal/module/inventory/enums"
	presentationenums "go_wp/internal/module/presentation/enums"
	"go_wp/pkg/facing"
)

// ForeignErrFallbacks 别模块 key → 展示兜底（只装「展示原文」，不含判定）。
var ForeignErrFallbacks = map[string]string{
	inventoryenums.ErrSKUSourceInvalid:             "SKU 来源取值不合法：只支持「自己创建」与「从仓库选」",
	inventoryenums.ErrWarehouseSKURequired:         "从仓库选 SKU 时必须先选仓库并指明该仓的那条仓库 SKU",
	inventoryenums.ErrWarehouseSKUNotFound:         "该仓库里没有这条仓库 SKU：请确认仓库选对了，或先在该仓建好这条货的库存记录",
	inventoryenums.ErrWarehouseSKUBundleNotAllowed: "捆绑商品不存在于仓库，不能用「从仓库选」建主体 SKU，请改用自己的编码",
	inventoryenums.ErrWarehouseSKUCodeTaken:        "该仓已有同一条 SKU 编码的库存行（我们自己的 SKU 在仓内唯一），请改用另一个仓库或另一个仓库 SKU",
	inventoryenums.ErrExternalSKUInvalid:           "外部编码不合法：长度需在 128 个字符以内且不含控制字符",
	inventoryenums.ErrStockQuantityInvalid:         "数量必须是非负整数",
	inventoryenums.ErrExternalSKUProductConflict:   "该外部编码在本仓已挂在另一个商品上：同一个外部编码在同一仓库内只能属于同一个商品（同一商品的多个口味可以共用一个外部编码）",
	inventoryenums.ErrWarehouseNotFound:            "仓库不存在",
	inventoryenums.ErrWarehouseDisabled:            "已停用的仓库不能作为归属仓",
	inventoryenums.ErrWarehouseProjectMismatch:     "仓库不属于该工程",
	inventoryenums.ErrWarehouseDefaultMissing:      "该工程没有默认仓可兜底，请先建一个仓库并设为默认仓",
	presentationenums.ErrDetachConfirmRequired:     "该操作会放弃模板同步（商品页转为独立文档），需要先确认",
	presentationenums.ErrRollbackTargetMiss:        "回滚目标不存在：该版本不属于这个商品，或产物文件已缺失",
	presentationenums.ErrRollbackFailed:            "回滚失败：线上版本保持不变",
	presentationenums.ErrSnapshotMismatch:          "该版本不属于这个商品，不能用来回滚",
}

// ForeignErrText 别模块 key → 展示兜底（未登记 / 空 key 一律空串）。
func ForeignErrText(key string) string {
	if key == "" {
		return ""
	}
	return ForeignErrFallbacks[key]
}

// ErrText 事实 → 展示用的中文兜底：本模块声明处的兜底优先，对端没有兜底表时用补丁表（见上）。
func ErrText(fact facing.Fact) string {
	if fb := ForeignErrText(fact.Key); fb != "" {
		return fb
	}
	return fact.Fallback
}
