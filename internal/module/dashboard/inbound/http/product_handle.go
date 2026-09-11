// product_handle.go — 后台商品管理页（issue #5 / T3a）。
//
// 独立于 dashboard 的通用 Handle：只依赖 product 契约与 project 契约，
// 避免把商品依赖掺进 dashboard 的通用装配。
//
// 交互遵循后台规范：GET 渲染完整页，POST 处理完 302 回列表（原生表单 + csrf_token 隐藏域）。
package dashboardhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

// productPageHandle 商品后台页处理器。
type productPageHandle struct {
	products productcontract.ProductService
	projects projectcontract.ProjectService
}

// NewProductPageHandle 构造。
func NewProductPageHandle(products productcontract.ProductService, projects projectcontract.ProjectService) *productPageHandle {
	return &productPageHandle{products: products, projects: projects}
}

// ProductsPage 商品管理页：工程切换 + 商品列表 + 每个商品的变体面板 + 内联新建表单。
func (h *productPageHandle) ProductsPage(c *gin.Context) {
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
	rows := make([]gin.H, 0, 50)
	if selected != "" {
		list, lerr := h.products.List(ctx, &productdto.ListReq{ProjectID: selected, Size: 100})
		if lerr != nil {
			c.String(http.StatusInternalServerError, lerr.Error())
			return
		}
		for _, p := range list {
			// 列表项不含变体明细，逐个取详情（上限 100，后台页可接受）。
			detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
			if derr != nil {
				continue
			}
			rows = append(rows, gin.H{
				"ID": detail.ID, "Name": detail.Name, "Slug": detail.Slug,
				"Status":   detail.Status,
				"PriceMin": detail.PriceMin, "PriceMax": detail.PriceMax,
				"VariantCount": detail.VariantCount,
				"Variants":     detail.Variants,
			})
		}
	}
	c.HTML(http.StatusOK, "admin/products.html", gin.H{
		"title":           "商品",
		"menu":            "products",
		"Projects":        projects,
		"SelectedProject": selected,
		"Products":        rows,
	})
}

// ProductsCreate 新建商品（自动生成首个变体），完成后回到列表。
func (h *productPageHandle) ProductsCreate(c *gin.Context) {
	req := &productdto.CreateReq{
		ProjectID: c.PostForm("projectId"),
		Name:      c.PostForm("name"),
		Slug:      c.PostForm("slug"),
	}
	if price := strings.TrimSpace(c.PostForm("defaultPrice")); price != "" {
		if v, perr := parseFloat(price); perr == nil {
			req.DefaultPrice = &v
		}
	}
	if _, err := h.products.Create(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+req.ProjectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+req.ProjectID)
}

// ProductsVariantCreate 为商品新增变体（未填字段继承商品级默认值）。
func (h *productPageHandle) ProductsVariantCreate(c *gin.Context) {
	req := &productdto.CreateVariantReq{
		ProductID: c.PostForm("productId"),
		SKUCode:   strings.TrimSpace(c.PostForm("skuCode")),
	}
	if price := strings.TrimSpace(c.PostForm("price")); price != "" {
		if v, perr := parseFloat(price); perr == nil {
			req.Price = &v
		}
	}
	projectID := c.PostForm("projectId")
	if _, err := h.products.CreateVariant(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
}

// ProductsVariantDelete 删除变体。
func (h *productPageHandle) ProductsVariantDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteVariant(c.Request.Context(), &productdto.DeleteVariantReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
}

// ProductsDelete 删除商品（连带变体）。
func (h *productPageHandle) ProductsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.Delete(c.Request.Context(), &productdto.DeleteReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
}
