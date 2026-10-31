package productmodel

import (
	"fmt"

	json "go_wp/pkg/jsonx"
)

type WarehouseSKURef struct {
	StockID string `json:"stockId"`

	WarehouseID string `json:"warehouseId"`

	WarehouseCode string `json:"warehouseCode"`
	SKUCode       string `json:"skuCode"`
}

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

func EncodeWarehouseSKUs(refs []WarehouseSKURef) json.RawMessage {
	if len(refs) == 0 {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(refs)
	if err != nil {

		return json.RawMessage("[]")
	}
	return b
}

func WarehouseSKURefsOn(refs []WarehouseSKURef, warehouseID string) (out []WarehouseSKURef) {
	for _, r := range refs {
		if r.WarehouseID == warehouseID {
			out = append(out, r)
		}
	}
	return out
}

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

func WarehouseSKURefsProjectScopeError(stockID string) error {
	return fmt.Errorf("仓库货行 %s 不属于本工程", stockID)
}
