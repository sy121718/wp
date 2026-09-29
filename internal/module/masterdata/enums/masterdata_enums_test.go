package masterdataenums

import "testing"

// TestFieldLabelPairsAreComplete 每条字段标签都必须「key + 中文兜底」成对，且 key 不重复。
//
// 为什么值得钉：漏登记不会报错，只会**静默回落字段名**（英文键摆到页面上）；
// key 写重了也不会报错，只会让两条字段共用一个词条（改了 A 顺带改了 B）。
func TestFieldLabelPairsAreComplete(t *testing.T) {
	seen := make(map[string]string, len(fieldLabelPairs))
	for field, pair := range fieldLabelPairs {
		if pair.Key == "" || pair.Fallback == "" {
			t.Fatalf("%s 的 key / 中文兜底不能为空", field)
		}
		if prev, dup := seen[pair.Key]; dup {
			t.Fatalf("key 重复：%s 与 %s 都用 %s", prev, field, pair.Key)
		}
		seen[pair.Key] = field
	}
	// 22 条 = 商品 5 + 变体 9 + 货源 8（与 master_data_changes 的快照字段对齐）。
	if len(fieldLabelPairs) != 22 {
		t.Fatalf("字段标签应有 22 条，实际 %d", len(fieldLabelPairs))
	}
}

// TestEntityTypeAndActionLabels 已登记取值给出 key + 兜底；未知取值给出空 key + 原值。
func TestEntityTypeAndActionLabels(t *testing.T) {
	if p := EntityTypeLabel(EntityInventorySource); p.Key != LabelKeyInventorySource || p.Fallback != LabelInventorySource {
		t.Fatalf("货源应给出 %s / %s，实际 %+v", LabelKeyInventorySource, LabelInventorySource, p)
	}
	if p := ActionLabel(ActionDelete); p.Key != LabelKeyActionDelete || p.Fallback != LabelActionDelete {
		t.Fatalf("删除应给出 %s / %s，实际 %+v", LabelKeyActionDelete, LabelActionDelete, p)
	}
	// 未登记：空 key（调用方据此跳过查表）+ 原值（不吞掉不认识的取值）。
	if p := EntityTypeLabel("unknown_entity"); p.Key != "" || p.Fallback != "unknown_entity" {
		t.Fatalf("未知实体应回显原值，实际 %+v", p)
	}
	if p := ActionLabel("archive"); p.Key != "" || p.Fallback != "archive" {
		t.Fatalf("未知动作应回显原值，实际 %+v", p)
	}
}

// TestFieldLabelLookup 已登记字段给出 key + 兜底；未登记字段空 key + 字段名。
func TestFieldLabelLookup(t *testing.T) {
	p := FieldLabel(EntityProductVariant, "sku_code")
	if p.Key != "admin.masterdata.field.product_variant.sku_code" || p.Fallback != "SKU 编码" {
		t.Fatalf("sku_code 的标签不对：%+v", p)
	}
	if p := FieldLabel(EntityProduct, "not_a_field"); p.Key != "" || p.Fallback != "not_a_field" {
		t.Fatalf("未登记字段应回显字段名，实际 %+v", p)
	}
	// 字段名相同但实体不同 → 两条独立词条（不能靠字段名单独命中）。
	if a, b := FieldLabel(EntityProduct, "name"), FieldLabel(EntityInventorySource, "name"); a.Key == b.Key {
		t.Fatalf("商品名与货源名不该共用词条：%s", a.Key)
	}
}
