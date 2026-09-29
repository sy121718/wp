package masterdatahttp

// masterdata_label_i18n_test.go — 变更记录页的展示名必须**按当前语言取词**。
//
// 这一批改动的契约：dto 的 *Label 是 service 填的中文兜底（JSON API 契约不变），
// 页面出口用行上的**原始值**（EntityType / Action / Field）重新取词。两个方向都要钉：
//
//   - 命中词条 → 出译文（英文界面上不该是中文）；
//   - 缺词条 → 出中文兜底，**不是裸 key**（`admin.masterdata.field.product.slug` 摆到页面上，
//     运营只会来报「页面坏了」）；
//   - 未登记取值（空 key）→ 直接回显原值，不查表（不吞掉不认识的取值）。

import (
	"testing"

	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
)

// fallbackTr 取词桩：不命中，恒给兜底（等价于「词条缺失」或「i18n 未初始化」）。
func fallbackTr(_, fallback string) string { return fallback }

// keyTr 取词桩：命中，回显 key（用来分辨「走了取词」与「直接用了兜底」）。
func keyTr(key, _ string) string { return "T:" + key }

func TestChangeRowLabelsFollowLanguage(t *testing.T) {
	row := &masterdatadto.ChangeResp{
		EntityType: masterdataenums.EntityProductVariant,
		Action:     masterdataenums.ActionUpdate,
		Field:      "sku_code",
	}

	zh := changeRow(fallbackTr, row)
	if zh["EntityTypeLabel"] != "商品变体" || zh["ActionLabel"] != "修改" || zh["FieldLabel"] != "SKU 编码" {
		t.Fatalf("缺词条时应给中文兜底，实际 %v / %v / %v",
			zh["EntityTypeLabel"], zh["ActionLabel"], zh["FieldLabel"])
	}

	en := changeRow(keyTr, row)
	if en["EntityTypeLabel"] != "T:"+masterdataenums.LabelKeyProductVariant {
		t.Fatalf("实体类型没走取词：%v", en["EntityTypeLabel"])
	}
	if en["ActionLabel"] != "T:"+masterdataenums.LabelKeyActionUpdate {
		t.Fatalf("动作没走取词：%v", en["ActionLabel"])
	}
	if en["FieldLabel"] != "T:admin.masterdata.field.product_variant.sku_code" {
		t.Fatalf("字段没走取词：%v", en["FieldLabel"])
	}
}

func TestChangeRowUnknownFieldKeepsRawName(t *testing.T) {
	view := changeRow(keyTr, &masterdatadto.ChangeResp{
		EntityType: masterdataenums.EntityProduct,
		Action:     "archive", // 未登记的动作
		Field:      "not_registered",
	})
	// 未登记 → 空 key（不查表），原样回显。
	if view["ActionLabel"] != "archive" {
		t.Fatalf("未登记动作应原样回显，实际 %v", view["ActionLabel"])
	}
	if view["FieldLabel"] != "not_registered" {
		t.Fatalf("未登记字段应原样回显，实际 %v", view["FieldLabel"])
	}
}

func TestEntityRowLabelsFollowLanguage(t *testing.T) {
	item := &masterdatadto.EntityHistoryResp{
		EntityType: masterdataenums.EntityInventorySource,
		LastAction: masterdataenums.ActionDelete,
		LastField:  "settle_price",
	}
	view := entityRow(fallbackTr, item, masterDataFilter{}, "")
	if view["EntityTypeLabel"] != "货源" || view["LastActionLabel"] != "删除" || view["LastFieldLabel"] != "内部结算价" {
		t.Fatalf("实体清单的展示名应给中文兜底，实际 %v / %v / %v",
			view["EntityTypeLabel"], view["LastActionLabel"], view["LastFieldLabel"])
	}
}

func TestMasterDataOptionsFollowLanguage(t *testing.T) {
	types := masterDataEntityTypeOptions(keyTr)
	if len(types) != 3 {
		t.Fatalf("实体类型下拉应有 3 项，实际 %d", len(types))
	}
	for _, o := range types {
		if label, _ := o["Label"].(string); label == "" || label[:2] != "T:" {
			t.Fatalf("下拉项没走取词：%v", o)
		}
	}
	actions := masterDataActionOptions(keyTr)
	if len(actions) != 3 {
		t.Fatalf("动作下拉应有 3 项，实际 %d", len(actions))
	}
}
