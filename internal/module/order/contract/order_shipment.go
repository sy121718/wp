package ordercontract

// order_shipment.go — 发货与承运商的契约（迁移 627 / 625，S2）。

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
)

// ShipmentService 发货用例与承运商管理。
//
// 与订单主体分开一个接口：发货是一个独立的**单据动作**（有单号、有承运商、有明细），
// 而不只是「改订单状态」的另一种写法 —— 两者混在一起会让读者以为
// ChangeStatus(shipped) 与 ShipOrder 等价（前者不扣库存、不建单据）。
type ShipmentService interface {
	// ShipOrder 发货：建发货单 + 扣库存（同时释放预占）+ 置 shipped，全在一个事务里。
	//
	// request_id 幂等：同一 (工程, request_id) 只会产生一张发货单，重复提交直接返回那张。
	ShipOrder(ctx context.Context, req *orderdto.ShipOrderReq) (res *orderdto.ShipmentResp, err error)
	// ListShipments 某订单的发货记录（客户与后台看同一份）。
	ListShipments(ctx context.Context, req *orderdto.ListShipmentReq) (list []*orderdto.ShipmentResp, err error)
	// ListPendingShip 待发货列表的一页：只列 paid 且还有未发数量的订单。
	//
	// 关键词 / 分页都在这里（列表页要能按订单号或收件人收窄，真实数据量下整页铺开不可用）。
	// 返回 total 与 rows 分开：分页条要的是「筛完之后总共有多少单」，不是本页几行。
	ListPendingShip(ctx context.Context, req *orderdto.PendingShipReq) (res *orderdto.PendingShipPageResp, err error)
	// BuildShipForm 发货表单数据（订单头 + 每行的「订单数量 / 已发 / 剩余 / 默认仓」+ 承运商下拉）。
	BuildShipForm(ctx context.Context, req *orderdto.ListShipmentReq) (form *orderdto.ShipForm, err error)

	// ListCarriers 承运商列表（默认只列启用中的）。
	ListCarriers(ctx context.Context, req *orderdto.ListCarrierReq) (list []*orderdto.CarrierResp, err error)
	// CreateCarrier 新建承运商（代码工程内唯一，大小写不敏感）。
	CreateCarrier(ctx context.Context, req *orderdto.CarrierCreateReq) (res *orderdto.CarrierResp, err error)
	// UpdateCarrier 修改承运商（停用走 Status，不物理删：历史发货单还指着它）。
	UpdateCarrier(ctx context.Context, req *orderdto.CarrierUpdateReq) (res *orderdto.CarrierResp, err error)
}

// ShipProbeLine 待判定的一行：这张订单行还差多少件、从哪个仓发。
type ShipProbeLine struct {
	OrderItemID uint64
	VariantID   string
	// WarehouseID 空 = 这一行还没固仓，按该工程默认仓判定。
	WarehouseID string
	// Quantity 本次要发的件数（订单数量 − 已发）。
	Quantity int
}

// ShipAvailability 一行的库存事实（发货前投影，不是扣减的依据）。
type ShipAvailability struct {
	// NoWarehouse 该行没固仓、工程里也没有可用默认仓 —— 发货必然报 ErrShipWarehouseRequired。
	NoWarehouse bool
	// Infinite 该行引用的货行全是不跟踪的（无限库存）：数量不构成约束。
	//
	// 与写路径逐字对齐：不跟踪行出库既不校验也不扣减，所以「没有跟踪行」= 可发，
	// 不能把它算成 0 件（那会把无限库存的商品整片误判成不可发）。
	Infinite bool
	// Unmatched 库存侧认不出这件货：变体没有任何货行引用、也拿不到它的 SKU 编码 ——
	// 发货时库存侧连「该扣哪一行」都答不出来（ErrStockSKURequired）。
	Unmatched bool
	// Available 跟踪行的 quantity 之和（**不减 reserved**）。
	//
	// 为什么不是 quantity − reserved：这张订单自己的预占也在 reserved 里，
	// 而发货扣减同时释放它（DeductStockOnShipTx 一句 SQL 写两列）。减掉 reserved
	// 会把「货已经替这张单留好了」的单判成发不出去 —— 那正是最该能发的一批。
	Available int
}

// ShipStockProbe 发货前「这批货现在能不能发」的只读判定（迁移 652，B6-a）。
//
// 为什么要有它：dy_erp 的仓库发货台在「行没匹配上本地 SKU」时禁用发货按钮，
// 而不是让人点了才失败。go_wp 对应的失败形态有两条 —— 变体缺失（下单数据不完整）、
// 库存不足（跟踪行不够扣）—— 都发生在提交事务里，页面无法预知。
//
// 判据必须与 ShipOrder 的真实校验同源：这里是**只读投影**，真正的胜负仍由发货事务的
// 行锁决定（投影说可发、并发卖掉了，提交照样失败）。所以它只用来禁用按钮，不承担正确性。
type ShipStockProbe interface {
	ShipAvailabilityByLine(ctx context.Context, projectID string, lines []ShipProbeLine) (out map[uint64]ShipAvailability, err error)
}
