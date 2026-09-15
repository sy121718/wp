// Package masterdataenums 主数据变更记录模块的统一文案与枚举取值（issue #19）。
//
// 「主数据变更记录」记的是**字段级配置变更**：谁在什么时候把哪条主数据的哪个字段
// 从什么改成了什么。它与库存流水（inventory_stock_movements，记「库存数量怎么变的」）
// 是两件事，各记一处：
//
//	· 本表（master_data_changes）  —— 商品 / 变体 / 货源的结构化字段被改动的历史；
//	· 库存流水                     —— 数量增减（入库 / 出库 / 盘点）的历史。
//
// 因此改价格、改 SKU 编码、上下架、改货源资料一律只进本表；
// 收货入库改变库存数量只进库存流水（数量列不进本表，那是流水的职责）。
package masterdataenums

// 实体类型白名单（master_data_changes.entity_type）。
//
// 只收「关键主数据」：商品、商品变体、货源。未知类型在写入入口被拒绝 ——
// 变更记录的价值在于「按实体能查全」，放进无主语的行只会稀释这张表。
const (
	// EntityProduct 商品主体（product.products）。
	EntityProduct = "product"
	// EntityProductVariant 商品变体（product.product_variants，一行一个 SKU）。
	EntityProductVariant = "product_variant"
	// EntityInventorySource 货源（inventory.inventory_sources，供应商 / 关联公司 / 自家工厂）。
	EntityInventorySource = "inventory_source"
)

// Actions 变更动作白名单。
const (
	// ActionCreate 新增（字段行的 old 为空、new 为该字段创建时的值）。
	ActionCreate = "create"
	// ActionUpdate 修改（只写真正变化的字段，逐字段一行）。
	ActionUpdate = "update"
	// ActionDelete 删除（字段行的 new 为空、old 为删除前的值）。
	ActionDelete = "delete"
)

// 变更来源（origin 列）：这条记录是**哪条写入路径**产生的。
//
// 同一个字段可能被多条路径改动（价格既能在变体编辑里改，也能被定价工具批量改，
// 还能被采购入库的成本价回写改），来源列让「这个值是怎么来的」可追。
const (
	// OriginProduct 商品接口 / 后台商品表单的商品级写操作。
	OriginProduct = "product"
	// OriginVariant 变体接口 / 后台商品表单的变体级写操作。
	OriginVariant = "variant"
	// OriginVariantGenerate 变体组合生成（勾选属性值 → 笛卡尔积）。
	OriginVariantGenerate = "variant_generate"
	// OriginPricing 定价工具批量改价（issue #13）。
	OriginPricing = "pricing"
	// OriginReceipt 采购收货 / 生产入库的成本价回写（issue #18）。
	OriginReceipt = "receipt"
	// OriginSource 货源接口 / 后台货源表单的写操作（issue #17）。
	OriginSource = "source"
)

// 实体类型展示名。
const (
	LabelProduct         = "商品"
	LabelProductVariant  = "商品变体"
	LabelInventorySource = "货源"
)

// 动作展示名。
const (
	LabelActionCreate = "新增"
	LabelActionUpdate = "修改"
	LabelActionDelete = "删除"
)

// 错误消息（handle / service 不硬编码文案，统一取这里）。
//
// 值是 i18n key（形态 `模块.类别.语义`），文案落在 sys_i18n（迁移 198）：响应层的
// pkg/response.translate 按请求语言查表，未命中时**原样返回 key**（可见的降级，不是空白）。
//
// 为什么必须是 key 形态：错误出口已统一到 response.ErrorAuto，而它的判据 IsBusinessError
// 只认两种**形态确定**的模式 —— key 形态与 enums 常量名形态（`ErrXxx`）。本模块的值曾经是
// 中文原文（`"参数不合法"`），两种都不匹配，于是全部业务错误被判成内部错误、对外统一 500
// （审计 CQ-010 的收尾缺口：既不是 key 也不是常量名，判据无法用形态区分它与内部 sentinel）。
const (
	// ErrInvalidParam 参数不合法。
	ErrInvalidParam = "masterdata.err.invalidParam"
	// ErrEntityTypeInvalid 实体类型不在变更记录的白名单内。
	ErrEntityTypeInvalid = "masterdata.err.entityTypeInvalid"
	// ErrActionInvalid 变更动作不合法。
	ErrActionInvalid = "masterdata.err.actionInvalid"
	// ErrEntityIDRequired 实体 id 必填。
	ErrEntityIDRequired = "masterdata.err.entityIDRequired"
	// ErrProjectRequired 未指定工程且无法解析出唯一工程。
	ErrProjectRequired = "masterdata.err.projectRequired"
	// ErrTimeRangeInvalid 时间范围不合法（起始晚于结束）。
	ErrTimeRangeInvalid = "masterdata.err.timeRangeInvalid"
)

// 成功消息（同样是 i18n key，词条见迁移 198）。
const (
	// MsgListSuccess 查询成功。
	MsgListSuccess = "masterdata.msg.listSuccess"
)

// EntityTypeLabel 实体类型 → 展示名（未知类型原样回显，不吞掉数据）。
func EntityTypeLabel(entityType string) string {
	switch entityType {
	case EntityProduct:
		return LabelProduct
	case EntityProductVariant:
		return LabelProductVariant
	case EntityInventorySource:
		return LabelInventorySource
	default:
		return entityType
	}
}

// ActionLabel 动作 → 展示名。
func ActionLabel(action string) string {
	switch action {
	case ActionCreate:
		return LabelActionCreate
	case ActionUpdate:
		return LabelActionUpdate
	case ActionDelete:
		return LabelActionDelete
	default:
		return action
	}
}

// EntityTypes 实体类型白名单（接口与页面的下拉项直接用这个顺序）。
func EntityTypes() []string {
	return []string{EntityProduct, EntityProductVariant, EntityInventorySource}
}

// Actions 动作白名单（页面下拉项顺序：新增 / 修改 / 删除）。
func Actions() []string {
	return []string{ActionCreate, ActionUpdate, ActionDelete}
}

// IsValidEntityType 实体类型是否在白名单内。
func IsValidEntityType(entityType string) bool {
	switch entityType {
	case EntityProduct, EntityProductVariant, EntityInventorySource:
		return true
	default:
		return false
	}
}

// IsValidAction 动作是否在白名单内。
func IsValidAction(action string) bool {
	switch action {
	case ActionCreate, ActionUpdate, ActionDelete:
		return true
	default:
		return false
	}
}

// fieldLabels 字段展示名表：键是「实体类型.字段名」。
//
// 字段名本身是主数据模块自己定的白名单键（见各模块的快照函数），这里只负责展示文案；
// 未登记的字段原样回显字段名 —— 显示一个陌生的英文键，好过把一条记录藏起来。
var fieldLabels = map[string]string{
	EntityProduct + ".name":          "商品名",
	EntityProduct + ".slug":          "URL 段",
	EntityProduct + ".status":        "上下架状态",
	EntityProduct + ".default_price": "商品级默认售价",
	EntityProduct + ".brand_id":      "品牌",

	EntityProductVariant + ".sku_code":            "SKU 编码",
	EntityProductVariant + ".barcode":             "条码",
	EntityProductVariant + ".price":               "售价",
	EntityProductVariant + ".compare_price":       "划线价",
	EntityProductVariant + ".cost_price":          "成本价",
	EntityProductVariant + ".enabled":             "启用状态",
	EntityProductVariant + ".option_values":       "规格组合",
	EntityProductVariant + ".home_warehouse_id":   "默认发货仓",
	EntityProductVariant + ".home_warehouse_code": "默认发货仓短码",

	EntityInventorySource + ".code":          "货源编码",
	EntityInventorySource + ".name":          "货源名称",
	EntityInventorySource + ".type":          "货源类型",
	EntityInventorySource + ".related_party": "关联方",
	EntityInventorySource + ".settle_price":  "内部结算价",
	EntityInventorySource + ".status":        "货源状态",
	EntityInventorySource + ".config":        "对接配置",
	EntityInventorySource + ".sort":          "排序",
}

// FieldLabel 字段展示名（未登记时回退字段名本身）。
func FieldLabel(entityType, field string) string {
	if label, ok := fieldLabels[entityType+"."+field]; ok {
		return label
	}
	return field
}
