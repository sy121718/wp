// inventory_req.go — inventory 模块入参（inbound 绑定用）。
package inventorydto

import "encoding/json"

// WarehouseThirdPartyReq 第三方仓的对接配置（迁移 240）。
//
// 非敏感项（对接方 / 外部仓代码 / 地址 / 联系人 / 是否允许发货）明文进 config；
// 凭据只以**密文**或**引用名**落库：APICredential 是写入方向的明文，
// service 加密后立刻替换成密文，明文既不落库也不回显（见 WarehouseThirdPartyResp）。
type WarehouseThirdPartyReq struct {
	Provider       string `json:"provider"`
	ExternalCode   string `json:"externalCode"`
	Address        string `json:"address"`
	Contact        string `json:"contact"`
	AllowsShipping bool   `json:"allowsShipping"`
	// APICredential 本次要写入的**明文**凭据；为空或等于掩码 **** 表示「不改凭据」
	// （未改动就不覆盖 —— 后台回显的是掩码，直接保存不该把真凭据清掉）。
	APICredential string `json:"apiCredential"`
	// ClearCredential 显式清除已配置的凭据（指针为 nil 表达不了「改成没有」）。
	ClearCredential bool `json:"clearCredential"`
	// SecretRef 凭据的引用名（无可用加密能力时的替代形态）：系统只记名字，值在部署侧。
	SecretRef string `json:"secretRef"`
}

// CreateWarehouseReq 新建仓库（验收 1）。
//
// Code 是仓库短码：工程内唯一，且是 SKU 编码的前缀（{仓短码}_{商品码}_{序号}）。
// IsDefault 为真表示同时把它设为该工程的默认仓（「未指定仓库」时的兜底）。
// Type 是仓库类型；第三方仓（third_party）的对接配置走 ThirdParty。
type CreateWarehouseReq struct {
	ProjectID string `json:"projectId"`
	Code      string `json:"code" binding:"required"`
	Name      string `json:"name" binding:"required"`
	// Type 空串按 self 归一（存量语义：不加类型就是自营仓）。
	Type      string          `json:"type"`
	IsDefault bool            `json:"isDefault"`
	Status    string          `json:"status"`
	Sort      int             `json:"sort"`
	Metadata  json.RawMessage `json:"metadata"`
	// ThirdParty 非 nil 且类型为 third_party 时写入 config；其余类型忽略它。
	ThirdParty *WarehouseThirdPartyReq `json:"thirdParty"`
}

// UpdateWarehouseReq 修改仓库（逐字段可选；nil = 本次不改）。
type UpdateWarehouseReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string  `json:"projectId"`
	Code      *string `json:"code"`
	Name      *string `json:"name"`
	// IsDefault 置真即「切换默认仓」（同工程唯一）；置假在已是默认仓时被拒绝 ——
	// 取消默认会让「未指定仓库」失去兜底，必须先指定另一个默认仓。
	IsDefault *bool   `json:"isDefault"`
	Status    *string `json:"status"`
	Sort      *int    `json:"sort"`
	// Type 仓库类型；nil = 本次不改。
	Type     *string         `json:"type"`
	Metadata json.RawMessage `json:"metadata"`
	// ThirdParty 非 nil 表示本次整体替换对接配置（凭据按「未改动则不覆盖」处理）。
	ThirdParty *WarehouseThirdPartyReq `json:"thirdParty"`
}

// GetWarehouseReq 按 ID 查询仓库。
type GetWarehouseReq struct {
	ID string `form:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `form:"projectId"`
}

// ListWarehouseReq 仓库列表（工程维度）。
type ListWarehouseReq struct {
	ProjectID string `form:"projectId"`
}

// DeleteWarehouseReq 删除仓库。
type DeleteWarehouseReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
}

// EnsureStockReq 确保某个 SKU 在某仓有一条库存记录（初始 0）。
//
// WarehouseID 为空时兜底到该工程的默认仓；VariantID 必填（库存以 SKU × 仓库 为维度）。
type EnsureStockReq struct {
	ProjectID   string `json:"projectId"`
	ProductID   string `json:"productId"`
	VariantID   string `json:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode"`
	WarehouseID string `json:"warehouseId"`
}

// UpdateStockTrackingReq 库存页行内编辑「跟踪开关 + 数量」（迁移 261）。
//
// TrackQuantity 为 false 即**无限**（不跟踪）：此时 Quantity 必须为空 / 0 ——
// 不跟踪的行不允许带数字（DDL 侧 CHECK (track_quantity OR quantity = 0) 兜底，
// 这里提前给一条可读的业务错误）。表单在无限态把数量框禁用并留空，
// **绝不预填 0**：0 是「卖光」这个具体事实，要写就得用户自己打出来。
//
// TrackQuantity 为 true 时数量必填：数量与开关一起写回，并且经变动契约
// （原因 = 手工调整）落一条流水 —— 数量的任何变化都要有痕迹。
type UpdateStockTrackingReq struct {
	ProjectID   string `json:"projectId"`
	WarehouseID string `json:"warehouseId" binding:"required"`
	VariantID   string `json:"variantId" binding:"required"`
	// TrackQuantity 跟踪开关：false = 无限（不跟踪）。
	TrackQuantity bool `json:"trackQuantity"`
	// Quantity 仅在 TrackQuantity 为真时生效；为假时必须为 0。
	Quantity int `json:"quantity"`
}

// GetStockReq 单条库存记录（按 id，或按 变体 × 仓库 定位）。
type GetStockReq struct {
	ID          string `form:"id"`
	VariantID   string `form:"variantId"`
	WarehouseID string `form:"warehouseId"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `form:"projectId"`
}

// ListStockBySKUReq 某 SKU 在各仓的库存（验收 3/4：库存以「SKU × 仓库」为维度）。
type ListStockBySKUReq struct {
	ProjectID string `form:"projectId"`
	SKUCode   string `form:"skuCode" binding:"required"`
}

// ListStockReq 库存记录列表（后台核对用，按需过滤 + 分页）。
type ListStockReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	ProductID   string `form:"productId"`
	VariantID   string `form:"variantId"`
	SKUCode     string `form:"skuCode"`
	// ExternalSKU 按该仓的外部编码过滤（迁移 251）：N:1 下同一个外码会命中同一商品的
	// 多个变体行 —— 「这个外码在本仓有多少条货」正是这么查的。
	ExternalSKU string `form:"externalSku"`
	Page        int    `form:"page"`
	Size        int    `form:"size"`
}

// —— 仓库 SKU 与外部编码（迁移 251，docs/14 §9.3）——

// ListWarehouseSKUReq 按工程 / 仓库列出可选的仓库 SKU（新建商品抽屉「从仓库选」的数据源）。
//
// WarehouseID 为空 = 列全部仓（后台核对场景）；Keyword 同时匹配我们自己的 sku_code
// 与外部编码（运营手上可能是其中任意一个）。
type ListWarehouseSKUReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	Keyword     string `form:"keyword"`
	Page        int    `form:"page"`
	Size        int    `form:"size"`
}

// GetWarehouseSKUReq 「从仓库选」的最小查询：给定仓库 + 我们自己那条仓库 SKU，返回该行。
type GetWarehouseSKUReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId" binding:"required"`
	SKUCode     string `form:"skuCode" binding:"required"`
}

// BindExternalSKUReq 绑定 / 更新某 (仓库, 变体) 库存行的外部编码。
//
// ExternalSKU 空串是**合法值**：清空 = 该仓改回用我们自己的 SKU（自营仓的常态）。
// 非空时的弱校验在 service：同一仓内同一外码必须指向同一个 product_id
// （同一商品的多个变体共用合法；跨商品报 ErrExternalSKUProductConflict）。
type BindExternalSKUReq struct {
	ProjectID   string `json:"projectId"`
	WarehouseID string `json:"warehouseId" binding:"required"`
	VariantID   string `json:"variantId" binding:"required"`
	ExternalSKU string `json:"externalSku"`
}

// —— 库存变动与流水（issue #16）——

// StockChangeLineReq 一条目标变动（SKU × 仓库 维度）。
//
// WarehouseID 为空时逐级兜底：本行 → 请求级 WarehouseID → 该工程的默认仓。
// ProductID / SKUCode 是落库快照（库存表按表隔离约定自带 sku_code 快照列）；
// 目标行已存在时沿用既有值，故只有「首次为新 SKU 建行」才必须给全。
type StockChangeLineReq struct {
	WarehouseID string `json:"warehouseId"`
	ProductID   string `json:"productId"`
	VariantID   string `json:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode"`
	// Quantity 语义随方向而定：in / out 是正数增减量；adjust 是目标绝对量（>= 0）。
	Quantity int `json:"quantity"`
	// CostPrice 可选：本次变动要写入的**当前成本价**（(仓库, SKU) 维度，覆盖式，迁移 244）。
	//
	// nil = 本次变动不碰成本 —— 出库、盘点、报损的默认行为就是不动成本（调成本不是
	// 它们的事）；给出值即按显式值写，且**与数量是否变化无关**（数量没变但要改成本
	// 是合法诉求，例如外部核算后导入）。
	//
	// 注意 nil 与「成本为空」是两回事：成本列可空表示尚未核算，显式给出的值必须
	// >= 0 且有限（0 合法：赠品 / 内部划拨）。
	CostPrice *float64 `json:"costPrice"`
}

// ChangeStockReq 按 SKU 增减库存（issue #16 验收 1/2/3/4）。
//
// 多行（多 SKU / 多仓）在**同一事务**里整体生效：任一行可用量不足即整体拒绝，
// 不留半截变动；加锁顺序由服务端按 (变体, 仓库) 标识升序固定，与入参顺序无关。
type ChangeStockReq struct {
	ProjectID   string               `json:"projectId"`
	WarehouseID string               `json:"warehouseId"`
	Direction   string               `json:"direction" binding:"required"`
	ReasonCode  string               `json:"reasonCode" binding:"required"`
	SourceType  string               `json:"sourceType"`
	SourceRef   string               `json:"sourceRef"`
	Remark      string               `json:"remark"`
	OperatorID  string               `json:"operatorId"`
	Lines       []StockChangeLineReq `json:"lines" binding:"required"`
}

// DeductStockReq 按 SKU 扣减库存（issue #16 验收 5：支持按物料清单展开多个子项 SKU）。
//
// ExpandBOM 为真时：入参每个 SKU 若维护了物料清单，就展开成它的子项（用量 × 请求量），
// 递归到**叶子**为止；有清单的 SKU 被展开而不是被扣，因此中间件半成品自身的真源不动。
// 没有清单的 SKU 就是叶子，按自身扣减。展开后的子项集合仍然整体排序加锁、整体生效
// （任一项不足即整批拒绝）。
type DeductStockReq struct {
	ProjectID   string               `json:"projectId"`
	WarehouseID string               `json:"warehouseId"`
	ReasonCode  string               `json:"reasonCode" binding:"required"`
	SourceType  string               `json:"sourceType"`
	SourceRef   string               `json:"sourceRef"`
	Remark      string               `json:"remark"`
	OperatorID  string               `json:"operatorId"`
	ExpandBOM   bool                 `json:"expandBom"`
	Lines       []StockChangeLineReq `json:"lines" binding:"required"`
}

// ListMovementReq 库存流水查询（按工程 / 仓 / 商品 / 变体 / SKU / 方向 / 原因 / 来源过滤 + 分页）。
type ListMovementReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	ProductID   string `form:"productId"`
	VariantID   string `form:"variantId"`
	SKUCode     string `form:"skuCode"`
	Direction   string `form:"direction"`
	ReasonCode  string `form:"reasonCode"`
	SourceType  string `form:"sourceType"`
	SourceRef   string `form:"sourceRef"`
	BatchID     string `form:"batchId"`
	// TimeFrom / TimeTo 流水时间区间（闭区间）：接受 2006-01-02 或 2006-01-02 15:04:05；
	// TimeTo 只给日期时按当日 23:59:59 收口（否则「截止今天」会把今天整天漏掉）。
	TimeFrom string `form:"timeFrom"`
	TimeTo   string `form:"timeTo"`
	Page     int    `form:"page"`
	Size     int    `form:"size"`
}

// —— 变动原因字典（issue #16 验收 4）——

// ListReasonReq 变动原因列表。
type ListReasonReq struct {
	ProjectID       string `form:"projectId"`
	Direction       string `form:"direction"`
	Keyword         string `form:"keyword"`
	IncludeDisabled bool   `form:"includeDisabled"`
}

// CreateReasonReq 新建自定义变动原因（内置原因由迁移 103 seed，全工程可见、只读）。
//
// Name 是**给人读的文案**（运营填的）：service 把它写成 sys_i18n 里的一条词条，
// 原因行本身只存自动派生的 i18n key —— 全站文案的唯一真源是文案词条，不是这张表。
type CreateReasonReq struct {
	ProjectID string `json:"projectId"`
	Code      string `json:"code" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Direction string `json:"direction" binding:"required"`
	Sort      int    `json:"sort"`
}

// UpdateReasonReq 修改自定义变动原因（逐字段可选）。
//
// 内置原因只读：改 Name 一律拒绝（它的 key 由系统按 code 派生），仅允许改启停与排序。
// 自定义原因改 Name 时同步更新 sys_i18n 里的那条词条。
type UpdateReasonReq struct {
	ID     string  `json:"id" binding:"required"`
	Name   *string `json:"name"`
	Status *string `json:"status"`
	Sort   *int    `json:"sort"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
}

// —— 物料清单（issue #16 验收 5）——

// BOMItemReq 物料清单的一条子项。
type BOMItemReq struct {
	ComponentVariantID string `json:"componentVariantId" binding:"required"`
	ComponentSKUCode   string `json:"componentSkuCode"`
	Quantity           int    `json:"quantity"`
}

// SetBOMReq 全量替换某个父 SKU 的物料清单（派生物，非追加）。
//
// Items 为空表示**清空**该父 SKU 的清单（之后扣减它就按自身扣）。
type SetBOMReq struct {
	ProjectID       string       `json:"projectId"`
	ParentVariantID string       `json:"parentVariantId" binding:"required"`
	ParentSKUCode   string       `json:"parentSkuCode"`
	Items           []BOMItemReq `json:"items"`
}

// GetBOMReq 查看某个父 SKU 的物料清单。
type GetBOMReq struct {
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID       string `form:"projectId"`
	ParentVariantID string `form:"parentVariantId" binding:"required"`
}

// —— 货源（issue #17 验收 1/2/4）——

// CreateSourceReq 新建货源（外部供应商 / 集团内关联公司 / 自家工厂）。
//
// Type 只认 external / internal；RelatedParty 为 nil 时按类型取默认 ——
// 内部货源恒为关联方（内部交易必须能被关联方报表捕获），外部默认非关联方。
// SettlePrice 是内部结算价，只允许出现在内部货源上。
// Config 是**异构对接扩展信息**（JSON 对象，不同来源字段形状各不相同）。
type CreateSourceReq struct {
	ProjectID    string          `json:"projectId"`
	Code         string          `json:"code" binding:"required"`
	Name         string          `json:"name" binding:"required"`
	Type         string          `json:"type"`
	RelatedParty *bool           `json:"relatedParty"`
	SettlePrice  *float64        `json:"settlePrice"`
	Status       string          `json:"status"`
	Config       json.RawMessage `json:"config"`
	Sort         int             `json:"sort"`
	Metadata     json.RawMessage `json:"metadata"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-"`
}

// UpdateSourceReq 修改货源（逐字段可选；nil = 本次不改）。
//
// ClearSettlePrice 显式清空结算价 —— 指针为 nil 表示「不改」，没有它就无法把值改回 NULL。
type UpdateSourceReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID        string          `json:"projectId"`
	Code             *string         `json:"code"`
	Name             *string         `json:"name"`
	Type             *string         `json:"type"`
	RelatedParty     *bool           `json:"relatedParty"`
	SettlePrice      *float64        `json:"settlePrice"`
	ClearSettlePrice bool            `json:"clearSettlePrice"`
	Status           *string         `json:"status"`
	Config           json.RawMessage `json:"config"`
	Sort             *int            `json:"sort"`
	Metadata         json.RawMessage `json:"metadata"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-"`
}

// GetSourceReq 按 ID 查询货源。
type GetSourceReq struct {
	ID string `form:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `form:"projectId"`
}

// DeleteSourceReq 删除货源。
type DeleteSourceReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-"`
}

// ListSourceReq 货源列表（报表区分维度直接落在查询上）。
//
// RelatedParty 是三态字符串："" 全部 / "true" 仅关联方 / "false" 仅非关联方 ——
// 用字符串而不是 *bool，是因为 GET 查询里「参数缺失」与「参数为空」必须能区分开。
type ListSourceReq struct {
	ProjectID       string `form:"projectId"`
	Type            string `form:"type"`
	RelatedParty    string `form:"relatedParty"`
	Status          string `form:"status"`
	Keyword         string `form:"keyword"`
	IncludeDisabled bool   `form:"includeDisabled"`
	Page            int    `form:"page"`
	Size            int    `form:"size"`
}

// SourceSummaryReq 货源关联方统计（按类型 × 关联方分组计数）。
type SourceSummaryReq struct {
	ProjectID string `form:"projectId"`
}

// —— 商品侧缓存同步与对账（issue #16 验收 6/7）——

// SyncStockCacheReq 【已废弃，121 删缓存列】历史 DTO，仅保留类型兼容。

// ReconcileStockCacheReq 缓存对账（可选对齐修复）。
