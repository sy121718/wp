package producthttp

// product_new_page.go — 商品新建整页（docs/04-C-instance-override.md §5，弃抽屉）：
// 左侧基础信息表单（字段名与原抽屉逐字一致，复用 /admin/products/create），
// 右侧模板卡片区（默认模板 + 候选列表 + 预览入口）；模板绑定与可视化自定义
// 在创建后的详情页继续（create 不落实例，绑定发生在首次发布）。

import (
	"net/http"
	"strings"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
)

// ProductNewPage GET /admin/products/new：商品新建整页。
func (h *productPageHandle) ProductNewPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "products_new", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 建表单片段需要的数据（与列表页同一 helper 同款口径）：属性组勾选列表 +
	// 「从仓库选」候选。两处取数分叉会让某条入口静默少字段 —— 本会话实测过。
	warehouseOptions, werr := h.warehouseOptions(ctx, selected)
	if werr != nil {
		shell.PageError(c, "products_new", werr)
		return
	}
	attributeOptions, aerr := h.attributeOptions(ctx, selected)
	if aerr != nil {
		shell.PageError(c, "products_new", aerr)
		return
	}
	warehouseSKUGroups, wserr := h.warehouseSKUOptions(ctx, selected, warehouseOptions)
	if wserr != nil {
		shell.PageError(c, "products_new", wserr)
		return
	}
	// 模板清单与默认模板：卡片展示用；未装配模板能力时右侧给降级提示。
	var tplRows []*contenttemplatedto.TemplateResp
	defaultID := ""
	tplAvail := false
	if h.templates != nil {
		tplAvail = true
		if rows, terr := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: productEntityType}); terr == nil {
			tplRows = rows
		}
		if tpl, rerr := h.templates.ResolveTemplate(ctx, productEntityType); rerr == nil {
			defaultID = tpl.TemplateID
		}
	}
	c.HTML(http.StatusOK, "admin/products_new.html", shell.Prepare(c, gin.H{
		"title": "新建商品", "menu": "products",
		"Projects": projects, "SelectedProject": selected,
		"WarehouseOptions": warehouseOptions, "AttributeOptions": attributeOptions,
		"WarehouseSKUOptions": warehouseSKUGroups,
		"Templates": tplRows, "DefaultTemplateID": defaultID, "TemplatesAvail": tplAvail,
		"Err": productPageErr(c),
	}))
}
