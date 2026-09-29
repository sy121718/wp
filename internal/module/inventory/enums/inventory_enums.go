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

	// —— 仓库类型与第三方对接配置（迁移 240）——
	//
	// 仓库不再只有「默认虚拟仓」一种：类型（自营 / 第三方 / 虚拟）决定它在业务上的角色，
	// 第三方仓各家的对接方式不同，配置存在 config jsonb 里（非敏感项 + 加密后的凭据）。
	ErrWarehouseTypeInvalid = "ErrWarehouseTypeInvalid" // 类型不是 self / third_party / virtual
	// ErrWarehouseConfigInvalid 对接配置必须是 JSON 对象（数组 / 标量都无法按字段取值）。
	ErrWarehouseConfigInvalid = "ErrWarehouseConfigInvalid"
	// ErrWarehouseCredentialInvalid 凭据内容不合法（过长或含控制字符）。
	ErrWarehouseCredentialInvalid = "ErrWarehouseCredentialInvalid"
	// ErrWarehouseCredentialKeyMissing 敏感配置加密密钥未配置（app.secret 为空）——
	// 宁可直接报错，也不把凭据明文写进 config。
	ErrWarehouseCredentialKeyMissing = "ErrWarehouseCredentialKeyMissing"
	// ErrWarehouseTypeVirtualDefault 虚拟仓不能作为默认仓。
	//
	// 默认仓是「未指定仓库」时的兜底，兜底仓必须是能真正收发的实体仓；
	// 把兜底落到虚拟仓上，库存会被记到一个永远不出货的仓里。
	ErrWarehouseTypeVirtualDefault = "ErrWarehouseTypeVirtualDefault"

	// —— 库存调整入口（盘点 / 报损，迁移 243 配套）——
	//
	// 库存管理页撤掉「入库 / 出库 / 调整」三向内联表单后，只留「库存调整（盘点 / 报损）」
	// 这一个受权限约束的写入口：盘点（adjust，目标绝对量）与报损（out）都必须写清原因与备注。
	ErrStockRemarkRequired = "ErrStockRemarkRequired" // 调整 / 报损必须填写备注
	// ErrMovementTimeRangeInvalid 流水的时间筛选值不合法（或起始晚于结束）。
	ErrMovementTimeRangeInvalid = "ErrMovementTimeRangeInvalid"

	// —— 库存记录（issue #15 验收 2/3）——
	ErrStockNotFound        = "ErrStockNotFound"        // 库存记录不存在
	ErrStockVariantRequired = "ErrStockVariantRequired" // 生成库存记录必须给出变体
	ErrStockWarehouseNeeded = "ErrStockWarehouseNeeded" // 生成库存记录必须给出仓库（或可兜底的默认仓）
	// ErrStockCostInvalid 显式传入的成本价不合法（必须是 >= 0 的有限数）。
	//
	// 成本价**可空**：NULL = 尚未核算（迁移 244；本模块绝不用 0 冒充「未知成本」，
	// 0 是合法的显式成本）。因此这条只拦「显式给了值却给错」的调用 ——
	// 不传成本是合法状态（出库 / 盘点 / 报损默认不动成本），不报错。
	ErrStockCostInvalid = "ErrStockCostInvalid"

	// —— 库存变动与流水（issue #16 验收 1/2/3）——
	ErrStockLinesRequired    = "ErrStockLinesRequired"    // 变动清单不能为空
	ErrStockLinesTooMany     = "ErrStockLinesTooMany"     // 变动清单行数超过单批上限
	ErrStockDirectionInvalid = "ErrStockDirectionInvalid" // 方向不是 in / out / adjust
	ErrStockQuantityInvalid  = "ErrStockQuantityInvalid"  // 数量不合法（in/out 必须为正，adjust 不得为负）
	ErrStockProductRequired  = "ErrStockProductRequired"  // 变动必须能确定商品（行内 / 已有库存记录）
	ErrStockReasonRequired   = "ErrStockReasonRequired"   // 变动原因必填
	// ErrStockSKURequired 缺少**仓库侧 SKU 编码**（空串一律拒绝）。
	//
	// 为什么单列一条而不是复用「变体必填」：这两个是不同维度的事实 —— 变体回答「是哪件货」，
	// 仓库侧 SKU 回答「这条货在仓库里叫什么」。仓库里的编码**永远是裸码**（不带仓码前缀，
	// 前缀只属于商品侧），空串建不出可追溯的库存行：两条这样的行还会在
	// UNIQUE (warehouse_id, sku_code)（迁移 244）上撞成 23505 —— 运营看到的是一句
	// 没有上下文的数据库错误，而不是「你少给了编码」。文案因此要可行动：让调用方传裸码。
	ErrStockSKURequired = "ErrStockSKURequired"
	// ErrStockInsufficient 可用量不足，扣减整体拒绝（一个事务内不留半截）。
	// 判定只读真源 inventory_stocks（带行锁），绝不读 product_variants.stock_total 缓存。
	ErrStockInsufficient = "ErrStockInsufficient"
	// ErrStockCachePortMissing 商品侧库存缓存端口未注入（装配缺陷 / 纯库存单测路径）。
	ErrStockCachePortMissing = "ErrStockCachePortMissing"

	// —— 无限库存（跟踪开关，迁移 261）——
	//
	// 「无限」用显式开关表达（inventory_stocks.track_quantity = false），不用可空数量：
	// 数量的 0 有两义（跟踪且卖光 / 不跟踪无限），区分它们的唯一依据就是这一列。
	// 下面两条是行内编辑「跟踪开关 + 数量」的拒绝理由。
	//
	// ErrStockQuantityRequired 跟踪库存时必须显式给数量。0 是「卖光」这个**具体事实**，
	// 必须由用户自己打出来 —— 留空不等于 0（否则「忘了填」会被静默记成「没货」）。
	ErrStockQuantityRequired = "ErrStockQuantityRequired"
	// ErrStockUntrackedQuantity 不跟踪（无限）的行不允许带数量：数量与开关是同一件事的
	// 两种表达，同时存在就是自相矛盾的数据（DDL 侧 CHECK (track_quantity OR quantity = 0)
	// 兜底，这里提前给一条可读的业务错误）。
	ErrStockUntrackedQuantity = "ErrStockUntrackedQuantity"

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

	// —— 仓库 SKU 外部编码（迁移 251 / docs/14 §9.3）——
	//
	// 属性属于**商品**（属性组 + 值 → 笛卡尔积 → variant.option_values），仓库侧只回答
	// 「这条货在这个仓叫什么」：inventory_stocks.external_sku 就是那个「叫什么」。
	// 映射是 **N:1**（同一个商品的十几个口味在仓库侧可能共用同一条 SKU / 同一个价格），
	// 所以唯一性**不是** DDL 上的唯一索引，而是下面这几条弱校验 ——
	// 把 N:1 写成 1:1 的约束会把合法数据判成冲突（迁移 251 的注释记了这件事）。
	ErrSKUSourceInvalid = "ErrSKUSourceInvalid" // 新建商品的 SKU 来源取值不合法（只认自己创建 / 从仓库选）
	// ErrWarehouseSKURequired 「从仓库选」必须同时给出仓库与仓库 SKU：
	// 只有仓库不知道选哪条货，只有编码不知道在哪个仓（同一个编码可以在多个仓各有一条）。
	ErrWarehouseSKURequired = "ErrWarehouseSKURequired"
	// ErrWarehouseSKUNotFound 该仓库里没有这条仓库 SKU（含编码不在本仓 / 不属于该工程）。
	ErrWarehouseSKUNotFound = "ErrWarehouseSKUNotFound"
	// ErrWarehouseSKUBundleNotAllowed 捆绑商品不存在于仓库，不能「从仓库选」建主体 SKU。
	ErrWarehouseSKUBundleNotAllowed = "ErrWarehouseSKUBundleNotAllowed"
	// ErrWarehouseSKUCodeTaken 该仓已有同一条 sku_code 的库存行（我们自己的 SKU 仓内唯一，
	// 迁移 244 的 UNIQUE (warehouse_id, sku_code)）；提前拦截是为了给出可读的错误，
	// 而不是让运营对着一个 23505 猜是哪两行。
	ErrWarehouseSKUCodeTaken = "ErrWarehouseSKUCodeTaken"
	// ErrExternalSKUInvalid 外部编码不合法（超过长度上限或含控制字符）。
	ErrExternalSKUInvalid = "ErrExternalSKUInvalid"
	// ErrExternalSKUProductConflict N:1 弱校验的拒绝理由：同一仓内同一外码已挂在**另一个商品**上。
	// 多个变体共用同一个外码是合法的（这正是 N:1 的意义），跨商品才是映射写错了。
	ErrExternalSKUProductConflict = "ErrExternalSKUProductConflict"
)

// 仓库状态取值（写入即校验，不接受自由文本）。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// 仓库类型取值（迁移 240 的 check 约束与之逐字一致）。
//
//	self        —— 自营仓：自有实体仓库，可收货、可发货，是「默认仓」的合法类型
//	               （默认仓是「未指定仓库」时的兜底，兜底仓必须是能真正存发货的实体仓）；
//	third_party —— 第三方仓：由外部服务商运营，对接方式各异（config 存对接方 / 外部仓代码 /
//	               地址联系人 / 加密凭据 / 是否允许发货），实际收发货走对方系统；
//	virtual     —— 虚拟仓：只用来分组或记账占位（在途 / 质检 / 报废待处理），
//	               没有实体收发能力，不能作为默认仓。
const (
	WarehouseTypeSelf       = "self"
	WarehouseTypeThirdParty = "third_party"
	WarehouseTypeVirtual    = "virtual"
)

// 第三方仓配置的固定键名（config jsonb 里的键）。
//
// 键名是**对外协议**：模板回显、接口出参、将来别家的适配器都按这些字面量取值，
// 因此收在 enums 里而不是散在 service / handler 各写一遍字符串。
const (
	// WarehouseConfigKeyProvider 对接方名称（如「菜鸟仓配」「顺丰云仓」）。
	WarehouseConfigKeyProvider = "provider"
	// WarehouseConfigKeyExternalCode 对方系统里的仓库代码。
	WarehouseConfigKeyExternalCode = "externalCode"
	// WarehouseConfigKeyAddress 仓库地址（人读，不参与任何路由）。
	WarehouseConfigKeyAddress = "address"
	// WarehouseConfigKeyContact 联系人 / 联系方式。
	WarehouseConfigKeyContact = "contact"
	// WarehouseConfigKeyAllowsShipping 该仓是否允许发货（第三方仓常见「只收不发」）。
	WarehouseConfigKeyAllowsShipping = "allowsShipping"
	// WarehouseConfigKeyAPICipher API 凭据的**密文**（AES-256-GCM，pkg/crypto 的 Encrypt 输出）。
	//
	// 只存密文：明文既不落库也不出接口。解密只发生在需要调用对方系统的路径上，
	// 后台回显一律是掩码（见 inventoryservice.MaskCredential）。
	WarehouseConfigKeyAPICipher = "apiKeyCipher"
	// WarehouseConfigKeySecretRef 凭据的**引用名**（不落明文时的替代形态）。
	//
	// 与 APICipher 二选一：有对称加密且配置了 app.secret 时用前者；
	// 否则只记「这份凭据在别处叫什么名字」（如 KMS 别名 / 环境变量名），
	// 值由部署方在进程环境里配置，系统只存引用、不存值。
	WarehouseConfigKeySecretRef = "secretRef"
)

// CredentialMask 凭据的掩码占位（接口与页面回显恒为它，绝不返回明文或密文）。
const CredentialMask = "****"

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

// 库存页行内编辑「跟踪开关 + 数量」（迁移 261）使用的变动原因与来源类型。
//
// 写成常量而不是在 service 里裸写字符串：原因 code 是字典表的数据、来源类型是流水的
// 对外协议，两处都要能被检索到（改了字典却漏改调用点，表现是「找不到原因」的业务错误）。
const (
	// ReasonManualAdjust 内置变动原因「手工调整」（迁移 103 seed，方向 = adjust）。
	// 行内编辑数量经变动契约走它 —— 有变动必有流水，行内编辑不是例外。
	ReasonManualAdjust = "manual_adjust"
	// MovementSourceInventoryPage 来源类型：库存页行内编辑。
	MovementSourceInventoryPage = "inventory_page"
)

// 入库产生的库存流水来源引用（source_type 列，验收 3 的「来源引用齐全」）。
//
// 采购收货的 source_ref 是采购单号，生产入库的 source_ref 是入库单号 ——
// 流水可以反向定位到是哪张单据动的库存。
const (
	MovementSourcePurchaseOrder = "purchase_order"
	MovementSourceProduction    = "production"
)

// 后台页面标题（i18n key）。
//
// 与原 dashboard/enums 里的历史同名常量同值：i18n key 是字符串协议，模板与词条表都按
// 字面量取值，页面搬回本模块后在本模块留一份常量，比让模块反向依赖 web/dashboard 层干净。
const (
	// MsgInventorySourcesTitle 货源管理页标题（issue #17）。
	// 值就是 i18n key：shell.Prepare 会做 t(title, title)，词条命中则显示译文、未命中回退字面量。
	// 因此**新增这两个常量必须同批 seed 词条**（迁移 217），否则顶栏会把 key 原样显示出来。
	MsgInventorySourcesTitle = "MsgInventorySourcesTitle"
	// MsgInventoryPurchasesTitle 采购入库页标题（issue #18）。同上，词条见迁移 217。
	MsgInventoryPurchasesTitle = "MsgInventoryPurchasesTitle"
)

// 业务错误**补充说明**的词条（key + 具名参数 + 中文兜底）。
//
// 背景与 product 模块同一套（见 pkg/i18n/errdetail.go）：service 用
// fmt.Errorf("%s：%s", 业务 key, 补充说明) 把上下文跟在业务文案后，而读侧只翻前半截 key ——
// 后半截中文在英文界面上永远是中文。改用 ErrorDetail 之后，读侧
//（inventoryErrDetailText）取词并填 {name} 占位符，整句按当前语言渲染。
const (
	// DetailExternalSKUOwner 「该外部编码在本仓已属于商品 X」里的那个商品。
	//
	// 形态是「业务 key：明细」，明细自带商品名 —— 只说「冲突了」等于让人去猜哪一条占了它。
	//
	// 名字**不带 Err 前缀**：它不是可抛出的业务错误消息（不单独经 ErrorAuto 的形态判据），
	// 而是「补充说明」的词条 key，由 i18n.ErrorDetail 编码进业务错误的 tail。
	DetailExternalSKUOwner = "admin.inventory.err.externalSkuOwner"
)

// ErrDetailFallbacks 上面那组明细词条的中文兜底（i18n 未初始化 / 该 key 没有词条时用）。
//
// 占位符与词条一一对应（{name} / …）：读侧填不上某个占位符时判为坏词条、回落这里的中文，
// 而不是把 {name} 摆给运营看。
var ErrDetailFallbacks = map[string]string{
	DetailExternalSKUOwner: "该外部编码在本仓已属于商品 {name}",
}
