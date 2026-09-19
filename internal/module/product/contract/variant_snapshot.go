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
	// CostPrice 该变体在**归属仓**的当前成本（分）。
	//
	// **nil = 尚未核算**，绝不是 0：库存行的 cost_price 可空（迁移 244），0 是
	// 「赠品 / 内部划拨」这类合法的显式成本 —— 用 0 冒充未知会让订单利润凭空多出一笔。
	// 早先用 int64 + 负值哨兵表达未知，2026-09-19 收口成指针：哨兵把「契约能不能表达
	// 未知」这件事藏进了实现侧的两处注释里，换个消费方就会漏判。
	CostPrice *int64
	Enabled   bool
}

// VariantSnapshotPort 供订单域按下单快照取商品事实。
type VariantSnapshotPort interface {
	// VariantSnapshots 按变体 id 批量取快照；入参顺序不影响结果，重复 id 自动去重。
	//
	// projectID 是**必填**的工程作用域（审计 DB-009），不是可选过滤条件：products 在
	// 迁移 215 名单里，策略谓词读会话变量 app.project_id —— 没有作用域时换非超级角色后
	// 查询**静默返回 0 行**（fail closed 不报错），表现为「订单快照为空 / 加购拿不到变体 /
	// 价格核对对不出结论」而没有任何错误日志。空串或非 uuid 由 model 层拒绝
	// （rls.ErrInvalidProjectID）：调用点漏传工程时当场报错，好过在生产上排查静默空结果。
	//
	// 三个消费方都拿得到工程：order 下单（projectID 形参）、cart 加购与结算（同上）、
	// productLivePrice 片段（工程由商品组件烘进片段 URL）。
	//
	// 查不到的 id **不会出现在返回里** —— 调用方按「请求了哪些 / 拿到哪些」做差集，
	// 缺的那些就是「规格不存在、已删除，或不属于本工程」。不做静默跳过：静默会让订单少一行却不报错。
	//
	// **不属于本工程的变体同样不出现在返回里**（而不是返回一条商品名为空的快照）：
	// 变体表没有工程列、也不在 RLS 名单里，跨工程的变体 id 是读得出来的；靠消费方比较
	// ProjectID 来拦越权的话，工程为空的那条快照会让比较失效 —— 隔离必须落在端口这一层。
	VariantSnapshots(ctx context.Context, variantIDs []string, projectID string) (list []*VariantSnapshot, err error)
}
