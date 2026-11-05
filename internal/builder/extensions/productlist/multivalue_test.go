package productlist

import (
	"testing"
)

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

func TestCollectionFilterSingleValueStillWorks(t *testing.T) {
	f := collectionFilter(&Props{FilterCategoryID: "cat-only", FilterBrandID: "brand-only"})
	if f["categoryId"] != "cat-only" || f["brandId"] != "brand-only" {
		t.Fatalf("只配单值时应原样下推: %v", f)
	}
}

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

	for _, bad := range []string{"categoryIdMode", "brandIdMode", "tagIdMode"} {
		if _, ok := f[bad]; ok {
			t.Fatalf("下推了推导出来的错键 %s（契约里没有它）: %v", bad, f)
		}
	}
}
