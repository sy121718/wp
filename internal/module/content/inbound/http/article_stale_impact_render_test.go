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
// 修复：双变量 range（_, page）。本用例钉住 Total >= 1 时页面完整渲染 + 路径进清单。
func TestArticlesListRenderWithStaleImpactPages(t *testing.T) {
	list := []*contentdto.ContentResp{
		{ID: "a1", Slug: "hello-world", Revision: 1, UpdatedAt: "2026-09-13 10:00",
			Data: map[string]any{"title": "第一篇"}},
	}
	data := articleListPageData(list, map[string]string{"a1": "/blog/hello-world"}, "", "")
	data["StaleImpact"] = gin.H{
		"Available": true,
		"Pages":     []gin.H{{"ID": "pg1", "Path": "/blog/hello-world", "ProjectID": "prj", "ProjectName": "站点"}},
		"Total":     1, "Truncated": false, "Limit": 30, "Hint": "",
	}
	body := renderAdminTemplate(t, "admin/articles.html", articleLayoutData(data))
	for _, want := range []string{
		"待重建影响面",
		"/blog/hello-world",
		"第一篇",
		// 新建入口（弃抽屉）是整页链接。
		"/admin/articles/new",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("待重建影响面存在时，列表页应完整渲染并包含 %q", want)
		}
	}
}
