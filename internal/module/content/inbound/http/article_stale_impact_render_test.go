package contenthttp

import (
	"github.com/gin-gonic/gin"
	contentdto "go_wp/internal/module/content/dto"
	"strings"
	"testing"
)

// 文章列表页在「存在待重建页面」时的完整渲染回归。
//
// 背景（切角色当天的真实故障）：StaleImpact.Pages 的 Jet range 写成了单变量形式
//
//	{{range page := .StaleImpact.Pages}} —— Jet 单变量 range 遍历**切片**时绑定的是
//	**下标**（int），不是元素（与 html/template 语义不同）。于是 page.Path 在运行期
//	报 "can't evaluate index (Path) in type int"，整页以 200 渲染成半截：列表、
//	<template>、抽屉、全部脚本都没有 —— 「创建文章」按钮点击毫无反应。
//	且该分支只在 Total >= 1（真有待重建页面）时进入，空数据的渲染测试永远发现不了。
//
// 修复：双变量 range（_, page）。
//
// **陷阱的落点已经变了**（清单从列表页移进了抽屉片段）：本用例因此拆成两半 ——
//
//	① 列表页有 stale 时只该给一行可点徽章，**不得**再渲染清单（否则又回到「13 条把列表顶出首屏」）；
//	② 清单本身在 admin/partials/stale_pages_drawer.html 里渲染，那里用的是双变量 range。
//
// 第 ② 半条同时是对那句话的回归：单变量 range 遍历切片时绑定的是**下标**，
// 表现是路径一栏全变成 0/1/2 —— 不会报错、不会中断，只有断言具体内容才能发现。
func TestArticlesListRenderWithStaleImpactPages(t *testing.T) {
	list := []*contentdto.ContentResp{
		{ID: "a1", Slug: "hello-world", Revision: 1, UpdatedAt: "2026-09-13 10:00",
			Data: map[string]any{"title": "第一篇"}},
	}
	data := articleListPageData(list, map[string]string{"a1": "/blog/hello-world"}, "", "")
	data["StaleImpact"] = gin.H{
		"Available": true,
		"Pages":     []gin.H{{"ID": "pg1", "Path": "/zh/stale-only-page", "ProjectID": "prj", "ProjectName": "站点"}},
		"Total":     1, "Truncated": false, "Limit": 8, "Hint": "",
	}
	body := renderAdminTemplate(t, "admin/content/articles.html", articleLayoutData(data))
	for _, want := range []string{
		"待重建影响面",                                         // 抽屉标题（data-drawer-title）
		`data-drawer-url="/admin/articles/stale/drawer"`, // 页头入口
		"第一篇", // 文章列表本身照常渲染
		"/admin/articles/new",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("待重建影响面存在时，列表页应完整渲染并包含 %q", want)
		}
	}
	// 反向：清单条目不得出现在列表页上（该字符串只存在于抽屉片段的数据里，
	// 若它出现在这里，说明清单又被摊回了首屏）。
	if strings.Contains(body, "/zh/stale-only-page") {
		t.Error("列表页渲染了待重建清单条目 —— 清单应只在抽屉片段里渲染")
	}
}

// TestArticleStaleDrawerRendersRows 抽屉片段：清单在这里渲染（双变量 range 的现在时）。
func TestArticleStaleDrawerRendersRows(t *testing.T) {
	data := gin.H{
		"Rows": []gin.H{
			{"ID": "pg1", "Path": "/zh/stale-only-page", "ProjectName": "站点", "Published": true},
			{"ID": "pg2", "Path": "/never-live", "ProjectName": "站点", "Published": false},
		},
		"Total": 2, "Truncated": false, "Limit": 8, "MultiProject": false,
	}
	body := renderAdminTemplate(t, "admin/partials/stale_pages_drawer.html", data)
	// 状态文案（「有更新未发布」/「未上线」）不在这里断言：本包的 render 助手走真实 i18n，
	// 拿到的可能是译文而不是兜底中文。那两档的断言在 internal/templates 的
	// stale_pages_drawer_test.go（那里 t 走兜底），此处只钉结构与 id 传递。
	for _, want := range []string{
		"/zh/stale-only-page", "/never-live",
		`href="/workbench?id=pg1"`,
		`data-drawer-readonly`, // 只读抽屉的显式声明（drawer.js 校验）
	} {
		if !strings.Contains(body, want) {
			t.Errorf("抽屉片段缺少 %q", want)
		}
	}
	// 单变量 range 陷阱的回归判据：路径列出现聚合成下标就说明 range 写错了。
	if strings.Contains(body, "<td>0</td>") || strings.Contains(body, "<td>1</td>") {
		t.Error("路径列渲染成了下标 —— range 又写成了单变量形式（Jet 会绑定下标）")
	}
}
