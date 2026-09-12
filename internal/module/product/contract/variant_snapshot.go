package productcontract

// variant_snapshot.go — 供订单域取下单快照的只读能力。
//
// 与 VariantCostPort / VariantAvailabilityPort 同属「按消费方需要收窄」的端口：
// 订单只需要「这几个规格现在叫什么、卖多少钱、成本多少、还启用着吗」这几条事实，
// 而 ProductService 有三十来个方法（且含全部写方法）。依赖面越大越容易在不经意间
// 用上不该用的能力；测试要造替身时，三十个方法的空实现也会把测试意图淹掉。

import "context"

// VariantSnapshot 变体的下单快照事实（只读）。
//
// 金额单位是**分**：商品域存的是元（numeric(12,2)），跨模块边界统一换算成分 ——
// 订单是记账的地方，浮点累加会让对账对不平。
type VariantSnapshot struct {
	VariantID    string
	ProductID    string
	ProjectID    string
	ProductName  string
	VariantLabel string
	SKU          string
	Price        int64
	CostPrice    int64
	Enabled      bool
}

// VariantSnapshotPort 供订单域按下单快照取商品事实。
type VariantSnapshotPort interface {
	// VariantSnapshots 按变体 id 批量取快照；入参顺序不影响结果，重复 id 自动去重。
	//
	// 查不到的 id **不会出现在返回里** —— 调用方按「请求了哪些 / 拿到哪些」做差集，
	// 缺的那些就是「规格不存在或已删除」。不做静默跳过：静默会让订单少一行却不报错。
	VariantSnapshots(ctx context.Context, variantIDs []string) (list []*VariantSnapshot, err error)
}
