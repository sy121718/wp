// inventory_handle.go — 后台库存管理页（issue #15）。
//
// 与商品后台页同一模式：独立于 dashboard 的通用 Handle，只依赖 inventory 契约、
// product 契约与 project 契约；GET 渲染完整页，POST 处理完 302 回列表
// （原生表单 + csrf_token 隐藏域），错误经 ?err= 回显。
//
// 页面承担两条验收：
//
//	· 验收 1：可建仓库并设置一个默认仓（列表里能直接看到哪个是默认仓）；
//	· 验收 4：可查看某 SKU 在各仓的库存（按商品 / 变体选一个 SKU，或用编码直接查）。
//
// 「库存」这一列读的是仓库模块的真源，不是 product_variants.stock_total 缓存 ——
// 商品页的「库存缓存」列明确标了它是缓存，两者不可混用。
package dashboardhttp

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

// inventoryPageHandle 库存后台页处理器。
type inventoryPageHandle struct {
	inventory inventorycontract.InventoryService
	projects  projectcontract.ProjectService
	// products 只用于「选哪个 SKU 看库存」的下拉（商品 → 变体），不参与任何库存判断。
	products productcontract.ProductService
}

// NewInventoryPageHandle 构造。
func NewInventoryPageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService, products productcontract.ProductService) *inventoryPageHandle {
	return &inventoryPageHandle{inventory: inventory, projects: projects, products: products}
}

// InventoryPage 库存管理页：工程切换 + 仓库列表 + 内联新建表单 + 某 SKU 各仓库存。
func (h *inventoryPageHandle) InventoryPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	warehouses, err := h.listWarehouses(ctx, selected)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	options, err := h.variantOptions(ctx, selected)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// 查询目标：variantId 优先（下拉选变体），其次直接给 SKU 编码。
	sku := strings.TrimSpace(c.Query("sku"))
	variantID := strings.TrimSpace(c.Query("variantId"))
	if variantID != "" {
		if sku == "" {
			sku = skuOfVariant(options, variantID)
		}
	}
	stockRows := []gin.H{}
	if sku != "" {
		rows, serr := h.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
			ProjectID: selected, SKUCode: sku,
		})
		if serr != nil {
			c.String(http.StatusInternalServerError, serr.Error())
			return
		}
		for _, r := range rows {
			stockRows = append(stockRows, gin.H{
				"WarehouseName": r.WarehouseName, "WarehouseCode": r.WarehouseCode,
				"VariantID": r.VariantID, "SKUCode": r.SKUCode,
				"Quantity": r.Quantity, "UpdatedAt": r.UpdatedAt,
			})
		}
	}

	c.HTML(http.StatusOK, "admin/inventory.html", withCSRF(c, gin.H{
		"title":           "库存管理",
		"menu":            "inventory",
		"Projects":        projects,
		"SelectedProject": selected,
		"Warehouses":      warehouses,
		"VariantOptions":  options,
		"SelectedSKU":     sku,
		"SelectedVariant": variantID,
		"StockRows":       stockRows,
		"Err":             strings.TrimSpace(c.Query("err")),
	}))
}

// InventoryWarehouseCreate 新建仓库（工程内第一个仓自动成为默认仓）。
func (h *inventoryPageHandle) InventoryWarehouseCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.CreateWarehouseReq{
		ProjectID: projectID,
		Code:      strings.TrimSpace(c.PostForm("code")),
		Name:      strings.TrimSpace(c.PostForm("name")),
		IsDefault: c.PostForm("isDefault") != "",
		Sort:      parseIntOr(c.PostForm("sort"), 0),
	}
	if _, err := h.inventory.CreateWarehouse(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/inventory?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory?project="+projectID)
}

// InventoryWarehouseUpdate 修改仓库（改名 / 改短码 / 排序 / 状态）。
func (h *inventoryPageHandle) InventoryWarehouseUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	code := strings.TrimSpace(c.PostForm("code"))
	name := strings.TrimSpace(c.PostForm("name"))
	status := strings.TrimSpace(c.PostForm("status"))
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	if status == "" {
		status = "active"
	}
	req := &inventorydto.UpdateWarehouseReq{
		ID: c.PostForm("id"), Code: &code, Name: &name, Status: &status, Sort: &sortValue,
	}
	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/inventory?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory?project="+projectID)
}

// InventoryWarehouseDefault 切换默认仓（同工程唯一；「未指定仓库」的兜底）。
func (h *inventoryPageHandle) InventoryWarehouseDefault(c *gin.Context) {
	projectID := c.PostForm("projectId")
	yes := true
	req := &inventorydto.UpdateWarehouseReq{ID: c.PostForm("id"), IsDefault: &yes}
	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/inventory?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory?project="+projectID)
}

// InventoryWarehouseDelete 删除仓库（默认仓 / 有非零库存时服务端拒绝）。
func (h *inventoryPageHandle) InventoryWarehouseDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.inventory.DeleteWarehouse(c.Request.Context(), &inventorydto.DeleteWarehouseReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/inventory?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory?project="+projectID)
}

// listWarehouses 某工程的仓库（模板直接渲染默认仓徽标与状态）。
func (h *inventoryPageHandle) listWarehouses(ctx context.Context, projectID string) (out []gin.H, err error) {
	out = []gin.H{}
	if projectID == "" {
		return out, nil
	}
	rows, err := h.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	for _, w := range rows {
		out = append(out, gin.H{
			"ID": w.ID, "Code": w.Code, "Name": w.Name, "Status": w.Status,
			"IsDefault": w.IsDefault, "Sort": w.Sort,
			"StatusLabel": warehouseStatusLabel(w.Status),
		})
	}
	return out, nil
}

// variantOptions 某工程全部商品的变体下拉项（SKU 查询的入口；上限 100 个商品，与商品页一致）。
func (h *inventoryPageHandle) variantOptions(ctx context.Context, projectID string) (out []gin.H, err error) {
	out = []gin.H{}
	if projectID == "" {
		return out, nil
	}
	list, err := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Size: 100})
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
		if derr != nil {
			continue
		}
		for _, v := range detail.Variants {
			out = append(out, gin.H{
				"VariantID": v.ID, "SKUCode": v.SKUCode,
				"Label": detail.Name + " · " + v.SKUCode,
			})
		}
	}
	return out, nil
}

// skuOfVariant 从下拉项里按变体 id 取 SKU 编码（找不到返回空串，页面提示无匹配）。
func skuOfVariant(options []gin.H, variantID string) string {
	for _, o := range options {
		if id, _ := o["VariantID"].(string); id == variantID {
			code, _ := o["SKUCode"].(string)
			return code
		}
	}
	return ""
}

// warehouseStatusLabel 仓库状态 → 展示文案。
func warehouseStatusLabel(status string) string {
	if status == "disabled" {
		return "已停用"
	}
	return "启用中"
}
