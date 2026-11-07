package masterdatahttp

import (
	"testing"

	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
)

// 页面的展示名不再由后端预取：只要枚举值与词条 key + 兜底，
// 译文由模板按请求语言取。这里锁住「key 与兜底都交齐、未登记项原样回显」。

func TestChangeRowLabelsCarryKeyAndFallback(t *testing.T) {
	row := &masterdatadto.ChangeResp{
		EntityType: masterdataenums.EntityProductVariant,
		Action:     masterdataenums.ActionUpdate,
		Field:      "sku_code",
	}

	got := changeRow(row)
	if got.EntityTypeKey != masterdataenums.LabelKeyProductVariant || got.EntityTypeFallback != "商品变体" {
		t.Fatalf("实体类型应同时带 key 与兜底：%q / %q", got.EntityTypeKey, got.EntityTypeFallback)
	}
	if got.ActionKey != masterdataenums.LabelKeyActionUpdate || got.ActionFallback != "修改" {
		t.Fatalf("动作应同时带 key 与兜底：%q / %q", got.ActionKey, got.ActionFallback)
	}
	if got.FieldKey != "admin.masterdata.field.product_variant.sku_code" || got.FieldFallback != "SKU 编码" {
		t.Fatalf("字段应同时带 key 与兜底：%q / %q", got.FieldKey, got.FieldFallback)
	}
}

func TestChangeRowUnknownFieldKeepsRawName(t *testing.T) {
	view := changeRow(&masterdatadto.ChangeResp{
		EntityType: masterdataenums.EntityProduct,
		Action:     "archive",
		Field:      "not_registered",
	})

	if view.ActionFallback != "archive" {
		t.Fatalf("未登记动作应原样回显，实际 %q", view.ActionFallback)
	}
	if view.FieldFallback != "not_registered" {
		t.Fatalf("未登记字段应原样回显，实际 %q", view.FieldFallback)
	}
}

func TestEntityRowLabelsCarryKeyAndFallback(t *testing.T) {
	item := &masterdatadto.EntityHistoryResp{
		EntityType: masterdataenums.EntityInventorySource,
		LastAction: masterdataenums.ActionDelete,
		LastField:  "settle_price",
	}
	view := entityRow(item, masterDataFilter{}, "")
	if view.EntityTypeFallback != "货源" || view.LastActionFallback != "删除" || view.LastFieldFallback != "内部结算价" {
		t.Fatalf("实体清单的兜底展示名不对：%q / %q / %q",
			view.EntityTypeFallback, view.LastActionFallback, view.LastFieldFallback)
	}
	if view.EntityTypeKey == "" || view.LastActionKey == "" || view.LastFieldKey == "" {
		t.Fatalf("实体清单应带词条 key：%+v", view)
	}
}

func TestMasterDataOptionsCarryKeys(t *testing.T) {
	types := masterDataEntityTypeOptions()
	if len(types) != 3 {
		t.Fatalf("实体类型下拉应有 3 项，实际 %d", len(types))
	}
	for _, o := range types {
		if o.LabelKey == "" || o.LabelFallb == "" {
			t.Fatalf("下拉项应同时带 key 与兜底：%+v", o)
		}
	}
	actions := masterDataActionOptions()
	if len(actions) != 3 {
		t.Fatalf("动作下拉应有 3 项，实际 %d", len(actions))
	}
	for _, o := range actions {
		if o.LabelKey == "" || o.LabelFallb == "" {
			t.Fatalf("动作下拉项应同时带 key 与兜底：%+v", o)
		}
	}
}
