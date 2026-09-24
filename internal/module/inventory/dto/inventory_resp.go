// inventory_resp.go — inventory 模块出参。
package inventorydto

import "encoding/json"

// SourceResp 货源（含类型与关联方标志，两者共同构成报表区分维度）。
//
// SettlePrice 仅内部货源有值（自产商品的成本口径）；Config 是异构对接扩展信息。
type SourceResp struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"projectId"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	RelatedParty bool            `json:"relatedParty"`
	SettlePrice  *float64        `json:"settlePrice"`
	Status       string          `json:"status"`
	Config       json.RawMessage `json:"config"`
	Sort         int             `json:"sort"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

// SourceSummaryGroupResp 一个「类型 × 关联方」分组的计数。
type SourceSummaryGroupResp struct {
	Type         string `json:"type"`
	RelatedParty bool   `json:"relatedParty"`
	Count        int64  `json:"count"`
}

// SourceSummaryResp 货源关联方统计（验收 4「关联方标志可用于报表区分」的数据出口）。
//
// Internal / External 是类型口径，RelatedParty / Unrelated 是关联交易口径 ——
// 两组数字交叉即「内部且关联」「外部但关联」等组合，Groups 给出完整交叉表。
type SourceSummaryResp struct {
	ProjectID    string                    `json:"projectId"`
	Total        int64                     `json:"total"`
	Internal     int64                     `json:"internal"`
	External     int64                     `json:"external"`
	RelatedParty int64                     `json:"relatedParty"`
	Unrelated    int64                     `json:"unrelated"`
	SettlePriced int64                     `json:"settlePriced"`
	Groups       []*SourceSummaryGroupResp `json:"groups"`
}

// WarehouseThirdPartyResp 第三方仓对接配置的**出参形状**。
//
// 凭据安全：本结构里没有任何字段承载凭据本体 —— 明文不落库、密文不出接口。
// 客户端只能知道「配没配」（HasCredential）、看到掩码（CredentialMasked）与引用名
// （SecretRef，它本身不是秘密，秘密在部署侧）。
type WarehouseThirdPartyResp struct {
	Provider       string `json:"provider"`
	ExternalCode   string `json:"externalCode"`
	Address        string `json:"address"`
	Contact        string `json:"contact"`
	AllowsShipping bool   `json:"allowsShipping"`
	// HasCredential 是否已配置凭据（存了密文或引用名）。
	HasCredential bool `json:"hasCredential"`
	// CredentialMasked 恒为掩码占位（****），供表单回显「已配置」而不泄露任何内容。
	CredentialMasked string `json:"credentialMasked"`
	// SecretRef 凭据引用名：系统只知道值在别处叫什么，不知道值是什么。
	SecretRef string `json:"secretRef"`
	// Extras 是 config 里未被识别的扩展键（将来加字段时旧版本也能原样带回）。
	Extras map[string]any `json:"extras,omitempty"`
}

// WarehouseResp 仓库（含类型、默认仓标记与第三方对接配置）。
type WarehouseResp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	IsDefault bool   `json:"isDefault"`
	Sort      int    `json:"sort"`
	// ThirdParty 仅第三方仓非空；其余类型为 nil（不给前端一份看似可用的空配置）。
	ThirdParty *WarehouseThirdPartyResp `json:"thirdParty,omitempty"`
	CreatedAt  string                   `json:"createdAt"`
	UpdatedAt  string                   `json:"updatedAt"`
}

// StockResp 库存记录（带上仓库展示信息，后台不必二次查询）。
//
// Quantity 是**真源**可用量：直接读 inventory_stocks.quantity，
// 绝不读 product_variants.stock_total（那是列表展示用的冗余缓存）。
type StockResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`
	SKUCode       string `json:"skuCode"`
	// ExternalSKU 是这一 (仓库, 变体) 在该仓的外部 / 第三方编码（迁移 251）。
	// 空串 = 该仓用我们自己的 SKU；N:1：同一商品的多个变体可以是同一个外码。
	ExternalSKU string `json:"externalSku"`
	// TrackQuantity 是否跟踪数量（迁移 261）：false = **无限**（不跟踪）——
	// 扣减不校验可用量、也不扣减（订单照卖）；true = 按 Quantity 跟踪。
	//
	// 必须与 Quantity 一起给出：quantity = 0 有两义（跟踪且卖光 / 不跟踪无限），
	// 只读数量会让「无限」在后台被看成「没货」。
	TrackQuantity bool `json:"trackQuantity"`
	Quantity      int  `json:"quantity"`
	// CostPrice 是这一 (仓库, SKU) 的**当前成本价**（迁移 244）：NULL = 尚未核算，
	// 0 是合法的显式成本 —— 后台必须能把两者分开显示，所以这里是可空指针而不是 float64。
	//
	// 成本只记一个当前值、不做成本流水；核算归采购侧（可外部核算后导入）。
	CostPrice *float64 `json:"costPrice"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// WarehouseStockResp 某个 (仓库, SKU) 库存行的**分仓视图**节点（含跟踪开关）。
//
// 与 StockResp 的分工：StockResp 是后台库存记录列表的投影（带 id / 工程 / 变体 / 时间戳），
// 本形状只回答「这条货在这个仓是什么状态」—— SKU / 跟踪开关 / 数量 / 成本。
// 多带 id 与时间戳只会让分仓展示多背一份它不用的数据。
//
// TrackQuantity 与 Quantity **必须一起读**：quantity = 0 有两义
// （跟踪且卖光 / 不跟踪无限），只看数量会把无限看成没货。
type WarehouseStockResp struct {
	WarehouseID   string   `json:"warehouseId"`
	WarehouseCode string   `json:"warehouseCode"`
	WarehouseName string   `json:"warehouseName"`
	SKUCode       string   `json:"skuCode"`
	TrackQuantity bool     `json:"trackQuantity"`
	Quantity      int      `json:"quantity"`
	CostPrice     *float64 `json:"costPrice"`
}

// ProductWarehouseStock 一个商品在**某个仓**的库存行（分仓聚合契约的元素形状）。
//
// 这是 contract.WarehouseStocksByProducts 的返回元素，**字段名与类型是冻结的**
// （商品侧依赖它）：product_id / warehouse_id / warehouse_code / warehouse_name /
// sku_code / track_quantity / quantity / cost_price 八项，一个都不能少。
//
// 为什么是「一行一个仓」而不是「一行一个商品」：聚合口径各调用方不同 ——
// 商品列表要跨仓求和、下单要按归属仓取一条，服务端替它们选定一种分组就等于替它们
// 丢了另一种。track_quantity 必须随行给出：否则「无限」与「卖光」在下游都是 0。
type ProductWarehouseStock struct {
	ProductID     string   `json:"productId"`
	WarehouseID   string   `json:"warehouseId"`
	WarehouseCode string   `json:"warehouseCode"`
	WarehouseName string   `json:"warehouseName"`
	SKUCode       string   `json:"skuCode"`
	TrackQuantity bool     `json:"trackQuantity"`
	Quantity      int      `json:"quantity"`
	CostPrice     *float64 `json:"costPrice"`
}

// WarehouseSKUResp 仓库里的一条货（(仓库, 仓库 SKU) 的只读投影，迁移 251）。
//
// 它回答的是「这个仓里有哪些货可选、这条货在该仓叫什么、商品侧有没有对应的变体」——
// 新建商品的「从仓库选」按它列出候选，服务端再按 sku_code 复核（前端只是便捷入口）。
type WarehouseSKUResp struct {
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	IsDefault     bool   `json:"isDefault"`
	// SKUCode 是我们自己在该仓的 SKU 编码（迁移 244 的 (warehouse_id, sku_code) 唯一）。
	SKUCode string `json:"skuCode"`
	// ExternalSKU 是该行登记的外部 / 第三方编码；空串 = 该仓用我们自己的 SKU。
	ExternalSKU string `json:"externalSku"`
	// ProductID / VariantID 是该行已绑定的商品与变体（HasVariant 为假时为空串）。
	ProductID string `json:"productId"`
	VariantID string `json:"variantId"`
	// HasVariant 这条货在商品侧是否已经有变体（今天恒为真，见 model.WarehouseSKURow）。
	HasVariant bool `json:"hasVariant"`
}

// —— 库存变动与流水（issue #16）——

// MovementResp 库存流水一行（含仓库与原因的展示信息，后台不必二次查询）。
type MovementResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`
	SKUCode       string `json:"skuCode"`
	Direction     string `json:"direction"`
	// Quantity 是本次变动的绝对量（恒 > 0）；Delta 是带符号的实际变化量。
	Quantity       int    `json:"quantity"`
	Delta          int    `json:"delta"`
	QuantityBefore int    `json:"quantityBefore"`
	QuantityAfter  int    `json:"quantityAfter"`
	ReasonID       string `json:"reasonId"`
	ReasonCode     string `json:"reasonCode"`
	ReasonName     string `json:"reasonName"`
	// ParentVariantID 非空表示这条流水是「按物料清单展开」出的子项扣减。
	ParentVariantID string `json:"parentVariantId"`
	SourceType      string `json:"sourceType"`
	SourceRef       string `json:"sourceRef"`
	Remark          string `json:"remark"`
	OperatorID      string `json:"operatorId"`
	BatchID         string `json:"batchId"`
	// UnitCost 是这次变动时刻的**成本留痕**（元，迁移 256）：出库 = 扣减时该库存行的
	// 当前成本；入库 = 本次显式成本（采购 / 生产单价），无显式成本时记库存行当前成本。
	// 可空：nil = 当时该 (仓库, SKU) 尚未核算 —— 与「0 成本」严格区分。
	UnitCost  *float64 `json:"unitCost"`
	CreatedAt string   `json:"createdAt"`
}

// StockChangeResp 一次库存变动（或一次 BOM 展开扣减）的结果。
//
// CacheTotals / CacheSynced / CacheFailures 是商品侧库存缓存时代的字段（issue #16）：那层
// 缓存已由迁移 121 删除、同步动作随之移除，三个字段保留以免改动对外响应结构 —— 值恒为
// 空 map / true / 空切片，**不代表任何真实同步结果**。
type StockChangeResp struct {
	BatchID       string          `json:"batchId"`
	Direction     string          `json:"direction"`
	Movements     []*MovementResp `json:"movements"`
	CacheTotals   map[string]int  `json:"cacheTotals"`
	CacheSynced   bool            `json:"cacheSynced"`
	CacheFailures []string        `json:"cacheFailures"`
}

// —— 变动原因字典（issue #16 验收 4）——

// ReasonResp 变动原因（内置 + 自定义）。
//
// Name 是 **i18n key**（真文案在 sys_i18n，后台按请求语言取词渲染）：
// 与 enums 常量的约定一致 —— 值就是 key，缺词条时页面会原样显示 key，
// 所以本模块新增原因必须同批 seed 词条（迁移 242）。
type ReasonResp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Direction string `json:"direction"`
	IsBuiltin bool   `json:"isBuiltin"`
	Status    string `json:"status"`
	Sort      int    `json:"sort"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// —— 物料清单（issue #16 验收 5）——

// BOMItemResp 物料清单的一条子项。
type BOMItemResp struct {
	ID                 string `json:"id"`
	ComponentVariantID string `json:"componentVariantId"`
	ComponentSKUCode   string `json:"componentSkuCode"`
	Quantity           int    `json:"quantity"`
}

// BOMResp 某个父 SKU 的物料清单。
type BOMResp struct {
	ParentVariantID string         `json:"parentVariantId"`
	ParentSKUCode   string         `json:"parentSkuCode"`
	ProjectID       string         `json:"projectId"`
	Items           []*BOMItemResp `json:"items"`
	UpdatedAt       string         `json:"updatedAt"`
}

// —— 库存对账（issue #16 验收 7）——
//
// 同族的 CacheSyncItemResp / SyncStockCacheResp 已随迁移 121 删除商品侧缓存一并移除：
// 前者是当时缓存同步结果的响应结构，后者在删除时只剩一行注释、定义早已不存在。
type ReconcileItemResp struct {
	VariantID   string `json:"variantId"`
	SKUCode     string `json:"skuCode"`
	TrueTotal   int    `json:"trueTotal"`
	CachedTotal int    `json:"cachedTotal"`
	Differed    bool   `json:"differed"`
	Repaired    bool   `json:"repaired"`
	RepairError string `json:"repairError"`
}

// —— 归属仓成本解析（成本快照口径收口，docs/14 §9.3）——
//
// 这一对类型是「订单行成本 = 该变体在该行归属仓的当前成本」的输入 / 输出形状：
// 输入可以带**显式仓库**（后台代客下单指定发货仓时用它），留空表示按库存域既有的
// 归属仓解析规则（默认仓）解析 —— 与扣减时的解析是同一个入口（resolveWarehouse）。
//
// 为什么不复用 StockResp：这里回答的是「这一行现在按多少算成本」这一个问题，
// 结果里必须能表达**未核算**（CostPrice 为 nil）—— 用 0 冒充会让利润凭空多出一笔。

// VariantWarehouseCostRef 一行成本查询：哪个变体、在哪个仓（空 = 按归属仓解析）。
type VariantWarehouseCostRef struct {
	VariantID   string `json:"variantId"`
	WarehouseID string `json:"warehouseId"`
}

// VariantWarehouseCost 一行成本结果。
//
// WarehouseID 是**解析后**的归属仓（显式给了就是它，留空解析成默认仓）——
// 调用方据此知道成本到底取自哪个仓；该变体在解析出的仓里没有库存行时，
// WarehouseID 照填、CostPrice 为 nil（「没有这条货 = 没有成本」，不是错误）。
type VariantWarehouseCost struct {
	VariantID   string `json:"variantId"`
	WarehouseID string `json:"warehouseId"`
	// CostPrice 该 (仓库, SKU) 的当前成本价（元）；nil = 尚未核算**或**该仓没有这条库存行。
	CostPrice *float64 `json:"costPrice"`
}

// ReconcileStockCacheResp 对账结果（Total == Matched + Differed）。
