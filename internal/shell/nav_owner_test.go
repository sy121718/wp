package shell_test

// nav_owner_test.go — 侧栏高亮的「归属」判据（原 navPathAlias 的替代）。
//
// 背景：高亮过去是「菜单路径 == 当前路径」精确匹配 + 一张手写的 `navPathAlias`
// （11 条子页面 → 父菜单）。那张表被删掉了，改成「当前路径按**路径段**最长前缀
// 归属到某个菜单项」（navOwnerPath）。这条判据错了不会报错 —— 表现只是「打开某页
// 时侧栏没有高亮」或「高亮了隔壁那一项」，所以必须有会变红的用例。
//
// 三条最容易写错的边界，各一条断言：
//   1. 子页面归属到父菜单（/admin/orders/new → /admin/orders）；
//   2. **按段比而不是按字符比**（/admin/mail/campaign 是 /admin/mail/campaigns 的
//      字符串前缀，按字符比会让 campaigns 页高亮到 campaign）；
//   3. 取最长（两个菜单都是前缀时，高亮最具体的那个）。

import (
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/shell"
)

func navTestTree() []admindto.MenuTreeNode {
	return []admindto.MenuTreeNode{
		{ID: 1, Title: "订单管理", Path: "/admin/orders", Type: 2},
		{ID: 2, Title: "销售概览", Path: "/admin/orders/overview", Type: 2},
		{
			ID: 3, Title: "营销", Type: 1,
			Children: []admindto.MenuTreeNode{
				{ID: 4, Title: "邮件活动", Path: "/admin/mail/campaign", Type: 2},
				{ID: 5, Title: "群发活动", Path: "/admin/mail/campaigns", Type: 2},
			},
		},
		{ID: 6, Title: "页面管理", Path: "/admin/pages", Type: 2},
		// 隐藏项不参与归属（不进侧栏，认了它就会出现「高亮指向看不见的菜单」）。
		{ID: 7, Title: "项目管理", Path: "/project", Type: 2, IsHidden: 1},
	}
}

// activePaths 返回本次渲染里被标记为高亮的路径集合。
func activePaths(t *testing.T, current string) map[string]bool {
	t.Helper()
	groups := shell.BuildNav(navTestTree(), current, func(key, fallback string) string { return fallback })
	got := map[string]bool{}
	var walk func([]shell.NavNode)
	walk = func(list []shell.NavNode) {
		for _, n := range list {
			if n.Active {
				got[n.Path] = true
			}
			walk(n.Children)
		}
	}
	for _, g := range groups {
		if g.Active && g.Path != "" {
			got[g.Path] = true
		}
		walk(g.Nodes)
	}
	return got
}

func TestNavHighlightOwnership(t *testing.T) {
	cases := []struct {
		name    string
		current string
		want    string
		notWant []string
	}{
		{name: "子页面归属父菜单", current: "/admin/orders/new", want: "/admin/orders", notWant: []string{"/admin/orders/overview"}},
		{name: "取最长（同级更具体的菜单）", current: "/admin/orders/overview", want: "/admin/orders/overview", notWant: []string{"/admin/orders"}},
		{name: "按路径段比而非字符前缀", current: "/admin/mail/campaigns", want: "/admin/mail/campaigns", notWant: []string{"/admin/mail/campaign"}},
		{name: "相邻的短名菜单自己命中", current: "/admin/mail/campaign", want: "/admin/mail/campaign", notWant: []string{"/admin/mail/campaigns"}},
		{name: "翻译工作台归属页面管理", current: "/admin/pages/translations", want: "/admin/pages"},
		{name: "精确命中菜单自身", current: "/admin/orders", want: "/admin/orders", notWant: []string{"/admin/orders/overview"}},
		{name: "尾部斜杠归一后仍命中", current: "/admin/orders/", want: "/admin/orders"},
		{name: "隐藏项不认领", current: "/project/settings", want: ""},
		{name: "无归属时不高亮", current: "/admin/unknown", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := activePaths(t, tc.current)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("当前路径 %s 不应有任何高亮，实际 %v", tc.current, got)
				}
				return
			}
			if !got[tc.want] {
				t.Fatalf("当前路径 %s 应高亮 %s，实际 %v", tc.current, tc.want, got)
			}
			for _, p := range tc.notWant {
				if got[p] {
					t.Fatalf("当前路径 %s 不应高亮 %s（同时高亮了 %v）", tc.current, p, got)
				}
			}
		})
	}
}
