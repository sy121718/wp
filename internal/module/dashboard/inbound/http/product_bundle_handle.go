// product_bundle_handle.go — 后台捆绑配置页（issue #20）。
//
// 与商品页同族（挂在 productPageHandle 上）：选工程 → 选商品 → 配选项规则 → 保存。
// 表单是**原生并行数组**（variantId / required / defaultQty / minQty / maxQty 各自多值），
// 零自定义 JS 就能表达「一行的多个字段」—— 不用 JSON 字符串塞进隐藏域，
// 也就没有「前端拼 JSON 出错但服务端照单全收」的缝隙。
//
// 页面底部内嵌前台配置器片段（hx-get /_fragments/bundleConfigurator）：
// 运营在这里看到的就是访客看到的那一份渲染。
package dashboardhttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
)

// ProductBundlePage 捆绑配置页。
func (h *productPageHandle) ProductBundlePage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	selectedProject := strings.TrimSpace(c.Query("project"))
	if selectedProject == "" && len(projects) > 0 {
		selectedProject = projects[0].ID
	}
	var list []*productdto.ProductResp
	if selectedProject != "" {
		list, err = h.products.List(ctx, &productdto.ListReq{ProjectID: selectedProject, Size: 100})
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	selectedProduct := strings.TrimSpace(c.Query("product"))
	var detail *productdto.BundleConfigResp
	if selectedProduct != "" {
		detail, err = h.products.GetBundleConfig(ctx, &productdto.GetBundleConfigReq{ProductID: selectedProduct})
		if err != nil {
			// 配置读不出来（商品不存在 / 库存端口未接入）时仍然渲染页面骨架，
			// 把原因放在页面提示里 —— 比一个 500 空白页可诊断得多。
			detail = nil
		}
	}
	skus, serr := h.products.ListBundleSKUs(ctx, &productdto.ListBundleSKUReq{ProjectID: selectedProject})
	if serr != nil {
		c.String(http.StatusInternalServerError, serr.Error())
		return
	}
	c.HTML(http.StatusOK, "admin/product_bundle.html", withCSRF(c, gin.H{
		"title":           "捆绑配置",
		"menu":            "products",
		"Projects":        projects,
		"SelectedProject": selectedProject,
		"Products":        list,
		"SelectedProduct": selectedProduct,
		"Detail":          detail,
		"Rows":            bundleConfigRows(detail, skus),
		"SkuCount":        len(skus),
		"MaxOptions":      productdto.BundleMaxOptionsLimit,
		"Err":             strings.TrimSpace(c.Query("err")),
	}))
}

// ProductBundleSave 保存捆绑配置（POST，成功后 302 回本页）。
func (h *productPageHandle) ProductBundleSave(c *gin.Context) {
	ctx := c.Request.Context()
	productID := strings.TrimSpace(c.PostForm("productId"))
	if productID == "" {
		c.Redirect(http.StatusFound, "/admin/products/bundle?err="+url.QueryEscape("请先选择商品"))
		return
	}
	cfg := productdto.NewEmptyBundleConfig()
	if v, aerr := strconv.Atoi(strings.TrimSpace(c.PostForm("maxOptions"))); aerr == nil && v > 0 {
		cfg.MaxOptions = v
	}
	cfg.MinTotalQty = atoiOrZero(c.PostForm("minTotalQty"))
	cfg.MaxTotalQty = atoiOrZero(c.PostForm("maxTotalQty"))
	ids := c.PostFormArray("variantId")
	required := c.PostFormArray("required")
	defaults := c.PostFormArray("defaultQty")
	mins := c.PostFormArray("minQty")
	maxes := c.PostFormArray("maxQty")
	for i, vid := range ids {
		vid = strings.TrimSpace(vid)
		if vid == "" {
			continue
		}
		cfg.Options = append(cfg.Options, productdto.BundleOption{
			VariantID:  vid,
			Required:   atoiAt(required, i) == 1,
			DefaultQty: atoiAt(defaults, i),
			MinQty:     atoiAt(mins, i),
			MaxQty:     atoiAt(maxes, i),
		})
	}
	if _, err := h.products.SetBundleConfig(ctx, &productdto.SetBundleConfigReq{
		ProductID: productID,
		Config:    cfg,
		// 操作人只从会话取（主数据变更记录要记「谁改的」）。
		OperatorID: builtin.GetUsername(c),
	}); err != nil {
		c.Redirect(http.StatusFound, "/admin/products/bundle?product="+productID+"&err="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/products/bundle?product="+productID)
}

// bundleConfigRows 配置表单的行（已配置的项 + 若干空行，供继续添加）。
//
// 空行不是「占位符」而是可提交的完整行：留空即被保存逻辑跳过。
// 不用 JS 动态加行的代价是「一次最多加几行」，换来的是纯服务端表单的可测与可回放。
func bundleConfigRows(detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp) []gin.H {
	rows := make([]gin.H, 0, productdto.BundleMaxOptionsLimit)
	// 已配置项的可用量来自配置详情（service 已按真源批量取好），
	// 新加的空白行没有可用量可言（还没选 SKU）。
	availByID := map[string]int{}
	if detail != nil {
		for _, o := range detail.Options {
			availByID[o.VariantID] = o.Available
		}
		for _, o := range detail.Options {
			rows = append(rows, bundleRow(skus, o.VariantID, o.Required, o.DefaultQty, o.MinQty, o.MaxQty, availByID[o.VariantID]))
		}
	}
	blank := 3
	if len(rows) > 0 {
		blank = 2
	}
	for i := 0; i < blank && len(rows) < productdto.BundleMaxOptionsLimit; i++ {
		rows = append(rows, bundleRow(skus, "", true, 1, 1, 0, 0))
	}
	return rows
}

// bundleRow 一行的表单数据（SKU 下拉的选中态在这里算好，模板只做展示）。
func bundleRow(skus []*productdto.BundleSKUResp, variantID string, required bool, def, minQ, maxQ, available int) gin.H {
	options := make([]gin.H, 0, len(skus)+1)
	options = append(options, gin.H{"ID": "", "Label": "— 不选 —", "Selected": variantID == ""})
	for _, s := range skus {
		options = append(options, gin.H{
			"ID":       s.VariantID,
			"Label":    s.ProductName + " · " + s.SKUCode,
			"Selected": s.VariantID == variantID,
		})
	}
	return gin.H{
		"VariantID":  variantID,
		"Options":    options,
		"Required":   required,
		"DefaultQty": def,
		"MinQty":     minQ,
		"MaxQty":     maxQ,
		"Available":  available,
	}
}

// atoiAt 取并行数组的第 i 个并转整数（缺失 / 非法一律 0）。
//
// 不用「解析失败即报错」：这里的 0 会进入 service 的配置校验，
// 该拒的（负数、必选数量为 0 等）由那边统一给出可读原因，前端只负责搬运。
func atoiAt(values []string, i int) int {
	if i < 0 || i >= len(values) {
		return 0
	}
	return atoiOrZero(values[i])
}

// atoiOrZero 解析整数，非法一律 0。
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
