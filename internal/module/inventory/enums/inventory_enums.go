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

// 缓存同步台账状态取值。
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
