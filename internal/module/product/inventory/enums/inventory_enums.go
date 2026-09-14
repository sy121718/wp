// Package inventoryenums inventory 模块响应消息（issue #15 / #16）。
//
// 与 product 模块同形：未接 i18n 前 ErrXxx / MsgXxx 是消息键常量，
// handler 与 service 一律引用本包，不硬编码文案。
package inventoryenums

// 响应消息。
const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"

	// —— 库存变动与缓存（issue #16）——
	MsgChangeSuccess    = "MsgChangeSuccess"    // 库存变动成功
	MsgDeductSuccess    = "MsgDeductSuccess"    // 库存扣减成功
	MsgSyncSuccess      = "MsgSyncSuccess"      // 缓存同步成功
	MsgReconcileSuccess = "MsgReconcileSuccess" // 缓存对账完成

	// —— 采购单与入库（issue #18）——
	MsgReceiptSuccess = "MsgReceiptSuccess" // 入库登记成功
)

// 错误消息。
const (
	ErrInvalidParam = "ErrInvalidParam" // 参数错误

	// —— 仓库（issue #15 验收 1）——
	ErrWarehouseNotFound        = "ErrWarehouseNotFound"        // 仓库不存在
	ErrWarehouseNameRequired    = "ErrWarehouseNameRequired"    // 仓库名称必填
	ErrWarehouseCodeRequired    = "ErrWarehouseCodeRequired"    // 仓库短码必填
	ErrWarehouseCodeInvalid     = "ErrWarehouseCodeInvalid"     // 短码不是字母数字（SKU 编码前缀只允许 A-Z 0-9）
	ErrWarehouseCodeTaken       = "ErrWarehouseCodeTaken"       // 同工程下短码已占用
	ErrWarehouseStatusInvalid   = "ErrWarehouseStatusInvalid"   // 状态不是 active / disabled
	ErrWarehouseIsDefault       = "ErrWarehouseIsDefault"       // 默认仓不能删除，也不能取消默认 / 停用
	ErrWarehouseHasStock        = "ErrWarehouseHasStock"        // 仓内仍有非零库存，不能删除
	ErrWarehouseDisabled        = "ErrWarehouseDisabled"        // 已停用的仓库不能作为归属仓
	ErrWarehouseProjectMismatch = "ErrWarehouseProjectMismatch" // 仓库不属于该工程
	// ErrWarehouseDefaultMissing 「未指定仓库」时工程内没有默认仓可兜底 ——
	// 兜底铁律不是「随便挑一个仓」，缺默认仓即数据缺陷，必须显式暴露。
	ErrWarehouseDefaultMissing = "ErrWarehouseDefaultMissing"

	// —— 库存记录（issue #15 验收 2/3）——
	ErrStockNotFound        = "ErrStockNotFound"        // 库存记录不存在
	ErrStockVariantRequired = "ErrStockVariantRequired" // 生成库存记录必须给出变体
	ErrStockWarehouseNeeded = "ErrStockWarehouseNeeded" // 生成库存记录必须给出仓库（或可兜底的默认仓）

	// —— 库存变动与流水（issue #16 验收 1/2/3）——
	ErrStockLinesRequired    = "ErrStockLinesRequired"    // 变动清单不能为空
	ErrStockLinesTooMany     = "ErrStockLinesTooMany"     // 变动清单行数超过单批上限
	ErrStockDirectionInvalid = "ErrStockDirectionInvalid" // 方向不是 in / out / adjust
	ErrStockQuantityInvalid  = "ErrStockQuantityInvalid"  // 数量不合法（in/out 必须为正，adjust 不得为负）
	ErrStockProductRequired  = "ErrStockProductRequired"  // 变动必须能确定商品（行内 / 已有库存记录）
	ErrStockReasonRequired   = "ErrStockReasonRequired"   // 变动原因必填
	// ErrStockInsufficient 可用量不足，扣减整体拒绝（一个事务内不留半截）。
	// 判定只读真源 inventory_stocks（带行锁），绝不读 product_variants.stock_total 缓存。
	ErrStockInsufficient = "ErrStockInsufficient"
	// ErrStockCachePortMissing 商品侧库存缓存端口未注入（装配缺陷 / 纯库存单测路径）。
	ErrStockCachePortMissing = "ErrStockCachePortMissing"

	// —— 变动原因字典（issue #16 验收 4）——
	ErrReasonNotFound          = "ErrReasonNotFound"          // 原因不在字典里（不接受自由文本）
	ErrReasonDirectionMismatch = "ErrReasonDirectionMismatch" // 原因方向与本次变动方向不符
	ErrReasonCodeRequired      = "ErrReasonCodeRequired"      // 原因 code 必填
	ErrReasonCodeInvalid       = "ErrReasonCodeInvalid"       // 原因 code 只允许小写字母 / 数字 / 下划线
	ErrReasonCodeTaken         = "ErrReasonCodeTaken"         // 同工程（或内置）下 code 已占用
	ErrReasonNameRequired      = "ErrReasonNameRequired"      // 原因名称必填
	ErrReasonDirectionInvalid  = "ErrReasonDirectionInvalid"  // 原因方向必须是 in / out / adjust
	ErrReasonBuiltin           = "ErrReasonBuiltin"           // 内置原因不可改（自定义原因才可维护）
	ErrReasonStatusInvalid     = "ErrReasonStatusInvalid"     // 原因状态不是 active / disabled

	// —— 货源（issue #17 验收 1/2/4）——
	ErrSourceNotFound      = "ErrSourceNotFound"      // 货源不存在
	ErrSourceNameRequired  = "ErrSourceNameRequired"  // 货源名称必填
	ErrSourceCodeRequired  = "ErrSourceCodeRequired"  // 货源编码必填
	ErrSourceCodeInvalid   = "ErrSourceCodeInvalid"   // 货源编码只允许大写字母 / 数字 / 下划线，且不超长
	ErrSourceCodeTaken     = "ErrSourceCodeTaken"     // 同工程下货源编码已占用
	ErrSourceTypeInvalid   = "ErrSourceTypeInvalid"   // 类型只认 external / internal（决定是否按内部交易口径出报表）
	ErrSourceStatusInvalid = "ErrSourceStatusInvalid" // 状态不是 active / disabled
	// ErrSourceInternalNotRelated 内部货源必须标记关联方：内部交易必须能被关联方报表捕获。
	ErrSourceInternalNotRelated = "ErrSourceInternalNotRelated"
	// ErrSourceSettleNotInternal 结算价只属于内部货源（自产商品的成本口径，外部供应商走采购价）。
	ErrSourceSettleNotInternal = "ErrSourceSettleNotInternal"
	ErrSourceSettleInvalid     = "ErrSourceSettleInvalid" // 结算价不能为负
	ErrSourceConfigInvalid     = "ErrSourceConfigInvalid" // 对接配置必须是 JSON 对象
	ErrSourceFilterInvalid     = "ErrSourceFilterInvalid" // 报表筛选参数不合法（关联方标志只认 true / false）

	// —— 采购单（issue #18 验收 1/2）——
	ErrPurchaseOrderNotFound   = "ErrPurchaseOrderNotFound"   // 采购单不存在
	ErrPurchaseCodeRequired    = "ErrPurchaseCodeRequired"    // 采购单号必填
	ErrPurchaseCodeInvalid     = "ErrPurchaseCodeInvalid"     // 采购单号只允许大写字母 / 数字 / 下划线 / 连字符，且不超长
	ErrPurchaseCodeTaken       = "ErrPurchaseCodeTaken"       // 同工程下采购单号已占用
	ErrPurchaseSourceRequired  = "ErrPurchaseSourceRequired"  // 采购单必须指定货源（来源 = #17 的货源）
	ErrPurchaseSourceDisabled  = "ErrPurchaseSourceDisabled"  // 已停用的货源不能下采购单（停用 = 不再选用）
	ErrPurchaseLinesRequired   = "ErrPurchaseLinesRequired"   // 采购单至少要有一行
	ErrPurchaseLinesTooMany    = "ErrPurchaseLinesTooMany"    // 采购行数超过上限
	ErrPurchaseLineRequired    = "ErrPurchaseLineRequired"    // 采购行必须给出变体
	ErrPurchaseLineDuplicate   = "ErrPurchaseLineDuplicate"   // 同一采购单里同一个 SKU 重复出现
	ErrPurchaseQuantityInvalid = "ErrPurchaseQuantityInvalid" // 采购数量必须为正整数
	ErrPurchasePriceInvalid    = "ErrPurchasePriceInvalid"    // 采购单价必须为正数
	ErrPurchaseStatusInvalid   = "ErrPurchaseStatusInvalid"   // 状态筛选值不是 pending / partial / received
	ErrPurchaseLinesLocked     = "ErrPurchaseLinesLocked"     // 已有入库数量的采购单不能再改行（改了状态推导就不成立）
	ErrPurchaseLineNotFound    = "ErrPurchaseLineNotFound"    // 采购行不存在（或不属于该采购单）

	// —— 入库（issue #18 验收 3/4/5）——
	ErrReceiptLinesRequired   = "ErrReceiptLinesRequired"   // 入库清单不能为空
	ErrReceiptLineDuplicate   = "ErrReceiptLineDuplicate"   // 同一入库请求里同一采购行只能出现一次
	ErrReceiptQuantityInvalid = "ErrReceiptQuantityInvalid" // 入库数量必须为正整数
	// ErrReceiptOverReceive 入库数量超过「采购数量 - 已入库数量」：超收在数据层即不可能，
	// 服务层在同一事务内用带守卫的原子递增拒绝（不留半截、不靠读-改-写）。
	ErrReceiptOverReceive    = "ErrReceiptOverReceive"
	ErrReceiptOrderDone      = "ErrReceiptOrderDone"      // 采购单已全部入库，无需再收
	ErrReceiptRequestInvalid = "ErrReceiptRequestInvalid" // 幂等键不合法（超长 / 非法字符）
	// ErrProductionSourceNotInternal 生产入库只认内部货源（自家工厂）：外部供应商走采购单，
	// 没有「无采购单的生产入库」这一说。
	ErrProductionSourceNotInternal = "ErrProductionSourceNotInternal"
	ErrProductionVariantRequired   = "ErrProductionVariantRequired" // 生产入库必须给出 SKU 变体
	ErrProductionCostInvalid       = "ErrProductionCostInvalid"     // 生产入库的成本价必须手工填写且非负
	// ErrVariantCostPortMissing 商品侧成本价端口未注入（装配缺陷 / 纯库存单测路径）。
	ErrVariantCostPortMissing = "ErrVariantCostPortMissing"

	// —— 物料清单（issue #16 验收 5）——
	ErrBOMQuantityInvalid    = "ErrBOMQuantityInvalid"    // 子项用量必须为正整数
	ErrBOMSelfReference      = "ErrBOMSelfReference"      // 父项不能把自己列为子项
	ErrBOMDuplicateComponent = "ErrBOMDuplicateComponent" // 同一子项重复出现
	ErrBOMComponentRequired  = "ErrBOMComponentRequired"  // 子项必须给出变体
	ErrBOMCycle              = "ErrBOMCycle"              // 物料清单会形成环（A→B→A）
	ErrBOMDepthExceeded      = "ErrBOMDepthExceeded"      // 展开层数超过上限
)

// 仓库状态取值（写入即校验，不接受自由文本）。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// 库存变动方向取值（写入即校验，不接受自由文本）。
//
//	in    —— 入库：quantity 是净增量；
//	out   —— 出库：quantity 是净减量，可用量不足即整体拒绝；
//	adjust—— 调整：quantity 是**目标绝对量**（盘点口径），实际增减由当前值算出。
const (
	DirectionIn     = "in"
	DirectionOut    = "out"
	DirectionAdjust = "adjust"
)

// 缓存同步台账状态取值（历史：对应表 inventory_stock_cache_syncs，迁移 121 已删除；
// 常量仍被 legacy DTO/响应字段引用，勿用于新逻辑）。
const (
	CacheSyncOK     = "ok"
	CacheSyncFailed = "failed"
)

// 货源类型取值（写入即校验，不接受自由文本）。
//
//	external —— 外部供应商（第三方，采购走采购单）；
//	internal —— 集团内（关联公司 / 自家工厂，可设内部结算价，自家工厂走生产入库）。
//
// 类型不是装饰性标签：它是「这笔交易是否按内部交易口径出报表」的判据，
// 内部货源恒为关联方（见 Service.CreateSource）。
const (
	SourceTypeExternal = "external"
	SourceTypeInternal = "internal"
)

// 货源状态取值（写入即校验，不接受自由文本）。
const (
	SourceStatusActive   = "active"
	SourceStatusDisabled = "disabled"
)

// 采购单状态取值（issue #18 验收 2）。
//
// 三种状态**全部由「已入库数量 与 采购数量」推导**（没有任何人工置位入口）：
//
//	pending  —— 所有行的已入库数量都是 0（未入库）；
//	partial  —— 有入库但至少一行还没收满（部分入库）；
//	received —— 每一行的已入库数量都等于采购数量（已入库）。
//
// 推导在登记入库的同一事务内完成并写回，因此状态永远与行数据自洽。
const (
	PurchaseStatusPending  = "pending"
	PurchaseStatusPartial  = "partial"
	PurchaseStatusReceived = "received"
)

// 入库单类型（验收 3/5）。
//
//	purchase   —— 采购收货：必有采购单，来源是 #17 的货源，成本价取采购单价；
//	production —— 自家工厂生产入库：无采购单，成本价手工填写。
const (
	ReceiptKindPurchase   = "purchase"
	ReceiptKindProduction = "production"
)

// 入库单状态（入库是「先记账再动库存」）。
//
//	pending —— 记账已落库、库存变动尚未完成（中间态；进程中断时会停在这里，后台可见）；
//	posted  —— 库存变动已提交并记下批次号（正常终态）。
//
// 变动失败不留 failed 单据：补偿路径把单据整体删除，状态列只有这两态。
const (
	ReceiptStatusPending = "pending"
	ReceiptStatusPosted  = "posted"
)

// 入库产生的库存流水来源引用（source_type 列，验收 3 的「来源引用齐全」）。
//
// 采购收货的 source_ref 是采购单号，生产入库的 source_ref 是入库单号 ——
// 流水可以反向定位到是哪张单据动的库存。
const (
	MovementSourcePurchaseOrder = "purchase_order"
	MovementSourceProduction    = "production"
)
