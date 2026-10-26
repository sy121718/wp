package productmodel

// variant_warehouse_sku.go — 变体引用的仓库货行（迁移 637）。
//
// 关联方向：**变体记住它引用了哪几条货行**（product_variants.warehouse_skus，JSON 数组）。
// 取代迁移 624 的「认领」（inventory_stocks.variant_id 一对一），理由见 637 的文件头：
// 一条货行要能同时供多个变体卖，一对一表达不了。
//
// 元素只存标识，不存名称快照（名称会漂移，存快照等于留一份会各自演化的账）：
//
//	{"stockId":"…","warehouseId":"…","warehouseCode":"…","skuCode":"…"}
//
// 为什么 stockId 之外还留着 warehouseCode / skuCode：货行被删而引用还在时（悬空引用），
// 这两个只读线索是唯一能让守卫与页面说清「引用的是哪一条」的东西。它们**不参与判定**：
// 判定只认 stockId，编码可以改，行 id 不会。

import (
	"fmt"

	json "go_wp/pkg/jsonx"
)

// WarehouseSKURef 变体引用的**一条**仓库货行（JSON 元素的 Go 形状）。
type WarehouseSKURef struct {
	// StockID inventory_stocks.id —— 定位键（编码可以改，行 id 不会）。
	StockID string `json:"stockId"`
	// WarehouseID 该货行所在仓。让它能不过查就按仓分组（页面按仓列明细）。
	WarehouseID string `json:"warehouseId"`
	// WarehouseCode / SKUCode 是悬空引用的可读线索（见文件头），不参与判定。
	WarehouseCode string `json:"warehouseCode"`
	SKUCode       string `json:"skuCode"`
}

// DecodeWarehouseSKUs 解析变体上的引用数组；空 / 非法 JSON 一律当空数组。
//
// 为什么不报错：这是**读路径**上的投影。一条坏 JSON 只该让这个变体看起来「没有引用」，
// 不该让商品列表整页 500 —— DDL 的形状约束（迁移 637 的 CHECK）已经挡住了写坏的可能，
// 这里再兜一层是为了让历史数据（约束上线前写入的）不至于把页面拖垮。
func DecodeWarehouseSKUs(raw json.RawMessage) (refs []WarehouseSKURef) {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil
	}
	out := refs[:0]
	for _, r := range refs {
		if r.StockID == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// EncodeWarehouseSKUs 把引用数组写回 JSON 列的形状；nil 落空数组（不是 SQL NULL）。
//
// 落 `[]` 而不是 null：DDL 的 NOT NULL + 形状 CHECK 要求数组，而 null 会让
// `warehouse_skus @> ...` 这类包含查询静默不命中（null 不是数组）。
func EncodeWarehouseSKUs(refs []WarehouseSKURef) json.RawMessage {
	if len(refs) == 0 {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(refs)
	if err != nil {
		// 元素全是字符串，Marshal 不会失败；真失败时退回空数组而不是把 nil 写进 NOT NULL 列。
		return json.RawMessage("[]")
	}
	return b
}

// WarehouseSKURefsOn 从若干变体的引用里挑出「落在某个仓」的那些（同仓多条按传入顺序）。
//
// 页面要按仓分组展示「这个变体在这个仓引用了哪些货」，分组口径必须只有一处定义
// （商品侧各处自己 filter 会让「仓内条数」与「明细行数」对不上）。
func WarehouseSKURefsOn(refs []WarehouseSKURef, warehouseID string) (out []WarehouseSKURef) {
	for _, r := range refs {
		if r.WarehouseID == warehouseID {
			out = append(out, r)
		}
	}
	return out
}

// UpsertWarehouseSKURef 追加或就地更新一条引用（按 stockId 判重）。
//
// 「引用同一行两次」是坏形状：库存汇总会把同一行的数量算两遍。就地更新而不是忽略，
// 是因为仓短码 / 裸码可能在引用建立之后被改过 —— 那条线索要与货行保持同步。
func UpsertWarehouseSKURef(refs []WarehouseSKURef, ref WarehouseSKURef) []WarehouseSKURef {
	if ref.StockID == "" {
		return refs
	}
	for i := range refs {
		if refs[i].StockID == ref.StockID {
			refs[i] = ref
			return refs
		}
	}
	return append(refs, ref)
}

// RemoveWarehouseSKURef 去掉一条引用（解除引用时用）；返回新切片与是否真的删掉了。
func RemoveWarehouseSKURef(refs []WarehouseSKURef, stockID string) (out []WarehouseSKURef, removed bool) {
	out = make([]WarehouseSKURef, 0, len(refs))
	for _, r := range refs {
		if r.StockID == stockID {
			removed = true
			continue
		}
		out = append(out, r)
	}
	return out, removed
}

// WarehouseSKURefsProjectScopeError 引用跨工程时的错误文案（供 service 组装可定位明细）。
func WarehouseSKURefsProjectScopeError(stockID string) error {
	return fmt.Errorf("仓库货行 %s 不属于本工程", stockID)
}
