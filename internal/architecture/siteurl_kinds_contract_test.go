// siteurl 实体类型清单与业务模块注册表的交叉校验（跨模块结构红线）。
//
// 背景：siteurl.KnownKinds 决定「站点设置页出现哪几行 URL 规则」，而实体类型的 owner 是
// content / product 模块 —— 两边靠人工同步。模块新增一个有公开详情页的类型而未同步时，
// 表现是**设置页静默少一行、路径派生返回空串**，全程不报错，只在「用户说这条路配不了」
// 时才会被发现。
//
// siteurl 自己的 TestDefaultPatternsCoverKnownKinds 守的是「DefaultPatterns 与 KnownKinds
// 两份内部清单互相对齐」，与该模块的真实类型集合无关，因此兜不住上面这种失配。本测试
// 把它补上：差集必须等于显式列出的「确实不出公开详情页」例外。
package architecture

import (
	"sort"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	productcontract "go_wp/internal/module/product/contract"
	"go_wp/internal/siteurl"
)

// noPublicDetailPageKinds 确实不出公开详情页的实体类型 —— 差集只允许是它们。
//
// 这份 map 是「人工判断」在本机制里的唯一残留，所以每一条都必须写明依据；
// 新增一条时先确认该类型真的没有面向访客的页面，而不是「暂时没配」。
var noPublicDetailPageKinds = map[string]string{
	"product_attribute": "商品属性组只有后台管理页（/product-attributes），不单独成公开页面，故无需 URL 规则",
}

// TestSiteURLKindsCoverRegisteredEntityTypes 断言两类失配都会让测试变红：
//   - 漏配：模块注册了、siteurl 没有（除例外清单外）—— 设置页会少一行；
//   - 孤儿：siteurl 有、模块没注册 —— 该类型的路径永远派生不出来。
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
