package architecture

import (
	"sort"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	productcontract "go_wp/internal/module/product/contract"
	"go_wp/internal/siteurl"
)

var noPublicDetailPageKinds = map[string]string{
	"product_attribute": "商品属性组只有后台管理页（/product-attributes），不单独成公开页面，故无需 URL 规则",
}

func TestSiteURLKindsCoverRegisteredEntityTypes(t *testing.T) {
	registered := map[string]bool{}
	for _, k := range contentcontract.EntityTypes() {
		registered[k] = true
	}
	for _, k := range productcontract.EntityTypes() {
		registered[k] = true
	}

	known := make(map[string]bool, len(siteurl.KnownKinds))
	for _, k := range siteurl.KnownKinds {
		known[k.Kind] = true
	}

	var missing, orphan []string
	for k := range registered {
		if known[k] {
			continue
		}
		if _, exempt := noPublicDetailPageKinds[k]; !exempt {
			missing = append(missing, k)
		}
	}
	for k := range known {
		if !registered[k] {
			orphan = append(orphan, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(orphan)

	if len(missing) > 0 {
		t.Errorf("已注册且有公开详情页、但 siteurl.KnownKinds 里没有的实体类型：%v\n"+
			"  → 加进 KnownKinds（默认路径 + 展示名）；若确实不出公开页，加进 "+
			"noPublicDetailPageKinds 并写明依据", missing)
	}
	if len(orphan) > 0 {
		t.Errorf("siteurl.KnownKinds 里有、但没有任何模块注册的实体类型（孤儿键）：%v\n"+
			"  → 该类型的详情页路径永远派生不出来，删除或改正键名", orphan)
	}
}
