package producthttp

// product_new_page.go — 商品新建整页（docs/04-C-instance-override.md §5，弃抽屉）：
// 左侧基础信息表单（字段名与原抽屉逐字一致，复用 /admin/products/create），
// 右侧模板卡片区（默认模板 + 候选列表 + 预览入口）；模板绑定与可视化自定义
// 在创建后的详情页继续（create 不落实例，绑定发生在首次发布）。

import (
	"net/http"
	"net/url"
	"strings"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"

	"github.com/gin-gonic/gin"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
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
	warehouseOptions, werr := h.warehouseOptions(ctx, selected, shell.TranslateFor(c))
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
	c.HTML(http.StatusOK, "admin/product/products_new.html", shell.Prepare(c, gin.H{
		"title": shell.TranslateFor(c)(productenums.ProductsCreateSubmit, "新建商品"), "menu": "products",
		"Projects": projects, "SelectedProject": selected,
		"WarehouseOptions": warehouseOptions, "AttributeOptions": attributeOptions,
		"WarehouseSKUOptions": warehouseSKUGroups,
		"Templates":           tplRows, "DefaultTemplateID": defaultID, "TemplatesAvail": tplAvail,
		"Err": productPageErr(c),
	}))
}

// —— 商品建表单的失败分档出口（渐进增强；片段见 partials/product_create_form.html）——

// productCreateFormFields 建表单片段的回填字段清单（片段与 handler 之间的协议）。
//
// 改片段里的 `name=` 必须同步改这里：helper 按这份清单给每个字段补零值键，漏列的字段
// 在失败片段里读不到 —— 而 Jet 读缺失的 map 键会**从那一行截断整页**（HTTP 仍 200，
// 错误槽之后的表单整块消失），现象是「保存失败后表单没了」，且没有任何报错。
var productCreateFormFields = []string{
	"projectId", "name", "type", "slug", "skuSource", "sku", "warehouseSku",
	"externalSku", "defaultPrice", "quantity", "trackQuantity",
	"attributeIds", "warehouseIds",
}

// productCreateFormData 建表单片段的渲染 data（**只在提交失败重渲染时**用）。
//
// 首屏（新建整页 / 列表页抽屉）不注入 FormEcho* 三键 —— 片段以 cfFill 开关区分
// 「空表单」与「回填表单」，两个页面 handler 因此不必为它加键。
func (h *productPageHandle) productCreateFormData(c *gin.Context, projectID, submitErr string) (gin.H, error) {
	ctx := c.Request.Context()
	projectID = strings.TrimSpace(projectID)
	warehouses, err := h.warehouseOptions(ctx, projectID, shell.TranslateFor(c))
	if err != nil {
		return nil, err
	}
	attributes, err := h.attributeOptions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	skuGroups, err := h.warehouseSKUOptions(ctx, projectID, warehouses)
	if err != nil {
		return nil, err
	}
	data := formEchoData(c, productCreateFormFields...)
	data["SelectedProject"] = projectID
	data["WarehouseOptions"] = warehouses
	data["AttributeOptions"] = attributes
	data["WarehouseSKUOptions"] = skuGroups
	data["SubmitErr"] = submitErr
	return data, nil
}

// productNewURL 商品新建整页的回跳地址（PRG）——新建表单失败的**唯一落点**。
//
// 为什么落回本页而不是列表页：表单就在本页，而本页页头已有 ?err= 渲染位
// （products_new.html 的错误槽 + product.err.go 的白名单取词）。回列表页会让用户
// 以为「提交成功才跳走的」，还要重新找一遍新建入口；更糟的是那条路的 Err 槽**从来没有
// 生产者**（全仓库没有第二处重定向到 /admin/products/new?err=），等于错误被静默吞掉。
func productNewURL(projectID, errMsg string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if e := strings.TrimSpace(errMsg); e != "" {
		q.Set("err", e)
	}
	if enc := q.Encode(); enc != "" {
		return "/admin/products/new?" + enc
	}
	return "/admin/products/new"
}

// productCreateFail 商品建表单的失败出口（分档口径唯一，别在调用点各写一份头判断）。
//
//	· htmx 请求：200 + 片段（错误槽 + 回填后的表单）—— 整页原地留住已填内容；
//	· 原生请求：302 + ?err= 回**本页**（无 JS 环境的行为：不跳走、错误显示在表单上方）。
//
// **成功路径也必须分档**（ProductsCreate 用 redirectWhere）：htmx 的 XHR 会自己跟随 302，
// 最终响应里读不到 Location，于是整页 HTML 会被塞进表单的位置里。
func (h *productPageHandle) productCreateFail(c *gin.Context, projectID, msg string) {
	if !isHXRequest(c) {
		c.Redirect(http.StatusFound, productNewURL(projectID, msg))
		return
	}
	data, err := h.productCreateFormData(c, projectID, msg)
	if err != nil {
		// 回填片段取数再失败：退回整页跳转（错误仍显示在新建页的表单上方），
		// 不把半个片段当成功返回。
		logger.Scene("product").With("project", projectID).Error(err, "商品新建失败后重建表单片段失败，退回新建页")
		redirectWhere(c, productNewURL(projectID, msg))
		return
	}
	c.HTML(http.StatusOK, "admin/product/product_create_form.html", shell.Prepare(c, data))
}
