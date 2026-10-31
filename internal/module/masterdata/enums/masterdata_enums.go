package masterdataenums

import "go_wp/pkg/facing"

const (
	EntityProduct = "product"

	EntityProductVariant = "product_variant"

	EntityInventorySource = "inventory_source"
)

const (
	ActionCreate = "create"

	ActionUpdate = "update"

	ActionDelete = "delete"
)

const (
	OriginProduct = "product"

	OriginVariant = "variant"

	OriginVariantGenerate = "variant_generate"

	OriginPricing = "pricing"

	OriginReceipt = "receipt"

	OriginSource = "source"
)

const (
	LabelKeyProduct         = "admin.masterdata.entity.product"
	LabelKeyProductVariant  = "admin.masterdata.entity.product_variant"
	LabelKeyInventorySource = "admin.masterdata.entity.inventory_source"
)

const (
	LabelProduct         = "商品"
	LabelProductVariant  = "商品变体"
	LabelInventorySource = "货源"
)

const (
	LabelKeyActionCreate = "admin.masterdata.action.create"
	LabelKeyActionUpdate = "admin.masterdata.action.update"
	LabelKeyActionDelete = "admin.masterdata.action.delete"
)

const (
	LabelActionCreate = "新增"
	LabelActionUpdate = "修改"
	LabelActionDelete = "删除"
)

type LabelPair struct {
	Key      string
	Fallback string
}

const (
	ErrInvalidParam = "masterdata.err.invalidParam"

	ErrEntityTypeInvalid = "masterdata.err.entityTypeInvalid"

	ErrActionInvalid = "masterdata.err.actionInvalid"

	ErrEntityIDRequired = "masterdata.err.entityIDRequired"

	ErrProjectRequired = "masterdata.err.projectRequired"

	ErrTimeRangeInvalid = "masterdata.err.timeRangeInvalid"
)

const (
	MsgListSuccess = "masterdata.msg.listSuccess"
)

func EntityTypeLabel(entityType string) LabelPair {
	switch entityType {
	case EntityProduct:
		return LabelPair{LabelKeyProduct, LabelProduct}
	case EntityProductVariant:
		return LabelPair{LabelKeyProductVariant, LabelProductVariant}
	case EntityInventorySource:
		return LabelPair{LabelKeyInventorySource, LabelInventorySource}
	default:
		return LabelPair{"", entityType}
	}
}

func ActionLabel(action string) LabelPair {
	switch action {
	case ActionCreate:
		return LabelPair{LabelKeyActionCreate, LabelActionCreate}
	case ActionUpdate:
		return LabelPair{LabelKeyActionUpdate, LabelActionUpdate}
	case ActionDelete:
		return LabelPair{LabelKeyActionDelete, LabelActionDelete}
	default:
		return LabelPair{"", action}
	}
}

func EntityTypes() []string {
	return []string{EntityProduct, EntityProductVariant, EntityInventorySource}
}

func Actions() []string {
	return []string{ActionCreate, ActionUpdate, ActionDelete}
}

func IsValidEntityType(entityType string) bool {
	switch entityType {
	case EntityProduct, EntityProductVariant, EntityInventorySource:
		return true
	default:
		return false
	}
}

func IsValidAction(action string) bool {
	switch action {
	case ActionCreate, ActionUpdate, ActionDelete:
		return true
	default:
		return false
	}
}

var fieldLabelPairs = map[string]LabelPair{
	EntityProduct + ".name":          {"admin.masterdata.field.product.name", "商品名"},
	EntityProduct + ".slug":          {"admin.masterdata.field.product.slug", "URL 段"},
	EntityProduct + ".status":        {"admin.masterdata.field.product.status", "上下架状态"},
	EntityProduct + ".default_price": {"admin.masterdata.field.product.default_price", "商品级默认售价"},
	EntityProduct + ".brand_id":      {"admin.masterdata.field.product.brand_id", "品牌"},

	EntityProductVariant + ".sku_code":            {"admin.masterdata.field.product_variant.sku_code", "SKU 编码"},
	EntityProductVariant + ".barcode":             {"admin.masterdata.field.product_variant.barcode", "条码"},
	EntityProductVariant + ".price":               {"admin.masterdata.field.product_variant.price", "售价"},
	EntityProductVariant + ".compare_price":       {"admin.masterdata.field.product_variant.compare_price", "划线价"},
	EntityProductVariant + ".cost_price":          {"admin.masterdata.field.product_variant.cost_price", "成本价"},
	EntityProductVariant + ".enabled":             {"admin.masterdata.field.product_variant.enabled", "启用状态"},
	EntityProductVariant + ".option_values":       {"admin.masterdata.field.product_variant.option_values", "规格组合"},
	EntityProductVariant + ".home_warehouse_id":   {"admin.masterdata.field.product_variant.home_warehouse_id", "默认发货仓"},
	EntityProductVariant + ".home_warehouse_code": {"admin.masterdata.field.product_variant.home_warehouse_code", "默认发货仓短码"},

	EntityInventorySource + ".code":          {"admin.masterdata.field.inventory_source.code", "货源编码"},
	EntityInventorySource + ".name":          {"admin.masterdata.field.inventory_source.name", "货源名称"},
	EntityInventorySource + ".type":          {"admin.masterdata.field.inventory_source.type", "货源类型"},
	EntityInventorySource + ".related_party": {"admin.masterdata.field.inventory_source.related_party", "关联方"},
	EntityInventorySource + ".settle_price":  {"admin.masterdata.field.inventory_source.settle_price", "内部结算价"},
	EntityInventorySource + ".status":        {"admin.masterdata.field.inventory_source.status", "货源状态"},
	EntityInventorySource + ".config":        {"admin.masterdata.field.inventory_source.config", "对接配置"},
	EntityInventorySource + ".sort":          {"admin.masterdata.field.inventory_source.sort", "排序"},
}

const MsgMasterDataChangesTitle = "MsgMasterDataChangesTitle"

var PageTitleChanges = LabelPair{MsgMasterDataChangesTitle, "变更记录"}

func FieldLabel(entityType, field string) LabelPair {
	if pair, ok := fieldLabelPairs[entityType+"."+field]; ok {
		return pair
	}
	return LabelPair{"", field}
}

var errFallbacks = map[string]string{
	ErrInvalidParam:      "参数不合法",
	ErrEntityTypeInvalid: "实体类型不在变更记录的白名单内",
	ErrActionInvalid:     "变更动作不合法",
	ErrEntityIDRequired:  "实体 id 必填",
	ErrProjectRequired:   "未指定工程",
	ErrTimeRangeInvalid:  "时间范围不合法",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
