package productlist

import (
	"testing"
)

// TestCollectionFilterMultiValueWinsOverSingle 多值维度下推时单值维度必须让位。
//
// 曾经的写法是「先 delete 单值键、再在后面的循环里无条件写回单值键」—— 删在前等于白删，
// 两个键一起进 SQL 并 AND：表现为「品牌勾了两个，只出第一个品牌的商品」。
// 这条路径只有真点一次筛选才走得到，单看每个函数都是对的。
func TestCollectionFilterMultiValueWinsOverSingle(t *testing.T) {
	p := &Props{
		FilterCategoryID:  "cat-single",
		FilterCategoryIDs: "cat-a,cat-b",
		FilterBrandID:     "brand-single",
		FilterBrandIDs:    "brand-a,brand-b",
		FilterTagID:       "tag-single",
		FilterTagIDs:      "tag-a",
	}
	f := collectionFilter(p)
	for single, multi := range map[string]string{
		"categoryId": "categoryIds",
		"brandId":    "brandIds",
		"tagId":      "tagIds",
	} {
		if _, ok := f[single]; ok {
			t.Fatalf("多值 %s 已下推时不应同时带单值 %s: %v", multi, single, f)
		}
		if f[multi] == "" {
			t.Fatalf("多值 %s 应已下推: %v", multi, f)
		}
	}
}

// TestCollectionFilterSingleValueStillWorks 只配单值时行为不变（多值优先不能把单值吃掉）。
func TestCollectionFilterSingleValueStillWorks(t *testing.T) {
	f := collectionFilter(&Props{FilterCategoryID: "cat-only", FilterBrandID: "brand-only"})
	if f["categoryId"] != "cat-only" || f["brandId"] != "brand-only" {
		t.Fatalf("只配单值时应原样下推: %v", f)
	}
}

// TestCollectionFilterMultiValueModeKeysAreContractKeys 多值语义键必须是契约里的那几个。
//
// 曾经的写法是从多值键名推导语义键（去掉尾字母再加 Mode）："brandIds" → "brandIdMode"，
// 而契约里的键是 "brandMode"。这个错**没有任何一层会报**：集合源的维度白名单只认
// "brandMode"，拼错的键被当成未知维度静默丢弃 —— 表现是「all 语义点了没反应，
// 出来还是并集」，与分类 / 标签的 all 行为正好相反。
//
// 所以这条用例断言的是**键名本身**，而不是「模式被下推了没有」。
func TestCollectionFilterMultiValueModeKeysAreContractKeys(t *testing.T) {
	p := &Props{
		FilterCategoryIDs:  "cat-a,cat-b",
		FilterCategoryMode: "all",
		FilterBrandIDs:     "brand-a,brand-b",
		FilterBrandMode:    "all",
		FilterTagIDs:       "tag-a,tag-b",
		FilterTagMode:      "all",
	}
	f := collectionFilter(p)
	for _, key := range []string{"categoryMode", "brandMode", "tagMode"} {
		if f[key] != "all" {
			t.Fatalf("语义键 %s 未按契约下推（实际 %q）: %v", key, f[key], f)
		}
	}
	// 推导出来的错键绝不能出现（它会被集合源静默丢弃，是最难查的一种失败）。
	for _, bad := range []string{"categoryIdMode", "brandIdMode", "tagIdMode"} {
		if _, ok := f[bad]; ok {
			t.Fatalf("下推了推导出来的错键 %s（契约里没有它）: %v", bad, f)
		}
	}
}
