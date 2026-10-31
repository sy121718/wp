package masterdatahttp

import (
	"testing"

	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
)

func fallbackTr(_, fallback string) string { return fallback }

func keyTr(key, _ string) string { return "T:" + key }

func TestChangeRowLabelsFollowLanguage(t *testing.T) {
	row := &masterdatadto.ChangeResp{
		EntityType: masterdataenums.EntityProductVariant,
		Action:     masterdataenums.ActionUpdate,
		Field:      "sku_code",
	}

	zh := changeRow(fallbackTr, row)
	if zh.EntityTypeLabel != "商品变体" || zh.ActionLabel != "修改" || zh.FieldLabel != "SKU 编码" {
		t.Fatalf("缺词条时应给中文兜底，实际 %v / %v / %v",
			zh.EntityTypeLabel, zh.ActionLabel, zh.FieldLabel)
	}

	en := changeRow(keyTr, row)
	if en.EntityTypeLabel != "T:"+masterdataenums.LabelKeyProductVariant {
		t.Fatalf("实体类型没走取词：%v", en.EntityTypeLabel)
	}
	if en.ActionLabel != "T:"+masterdataenums.LabelKeyActionUpdate {
		t.Fatalf("动作没走取词：%v", en.ActionLabel)
	}
	if en.FieldLabel != "T:admin.masterdata.field.product_variant.sku_code" {
		t.Fatalf("字段没走取词：%v", en.FieldLabel)
	}
}

func TestChangeRowUnknownFieldKeepsRawName(t *testing.T) {
	view := changeRow(keyTr, &masterdatadto.ChangeResp{
		EntityType: masterdataenums.EntityProduct,
		Action:     "archive",
		Field:      "not_registered",
	})

	if view.ActionLabel != "archive" {
		t.Fatalf("未登记动作应原样回显，实际 %v", view.ActionLabel)
	}
	if view.FieldLabel != "not_registered" {
		t.Fatalf("未登记字段应原样回显，实际 %v", view.FieldLabel)
	}
}

func TestEntityRowLabelsFollowLanguage(t *testing.T) {
	item := &masterdatadto.EntityHistoryResp{
		EntityType: masterdataenums.EntityInventorySource,
		LastAction: masterdataenums.ActionDelete,
		LastField:  "settle_price",
	}
	view := entityRow(fallbackTr, item, masterDataFilter{}, "")
	if view.EntityTypeLabel != "货源" || view.LastActionLabel != "删除" || view.LastFieldLabel != "内部结算价" {
		t.Fatalf("实体清单的展示名应给中文兜底，实际 %v / %v / %v",
			view.EntityTypeLabel, view.LastActionLabel, view.LastFieldLabel)
	}
}

func TestMasterDataOptionsFollowLanguage(t *testing.T) {
	types := masterDataEntityTypeOptions(keyTr)
	if len(types) != 3 {
		t.Fatalf("实体类型下拉应有 3 项，实际 %d", len(types))
	}
	for _, o := range types {

		if o.Label == "" || o.Label[:2] != "T:" {
			t.Fatalf("下拉项没走取词：%v", o)
		}
	}
	actions := masterDataActionOptions(keyTr)
	if len(actions) != 3 {
		t.Fatalf("动作下拉应有 3 项，实际 %d", len(actions))
	}
}
