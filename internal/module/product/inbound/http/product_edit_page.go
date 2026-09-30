// product_edit_page.go — 商品编辑整页（GET /admin/products/edit + POST /admin/products/update）。
//
// 为什么单独成页，而不是并进商品详情页：
//   - 详情页的职能是**子资源维护**（变体清单 / 评分 / 属性引用 / 分类与品牌 / 手工标签），
//     那里的每个表单都作用在「这个商品的某个子资源」上；
//   - 商品**自身**的字段（名称 / URL 段 / SKU / 状态 / 价格 / 单位 / 重量 / SEO / 图集）
//     自本页出现之前**只有创建时能填**，建完之后再也没有入口 —— 编辑入口缺位是真实缺口，
//     不是版式问题；
//   - 商品域翻译工作台（多语言）原先占着列表操作列的一格，本批收进编辑页：多语言改的是
//     这个商品的字段译文，与「改这个商品」是同一件事。
//
// 表单协议：字段名与 /api/product/update 的 UpdateReq 的 json 标签**逐字对齐**
// （name / slug / sku / status / defaultPrice / unit / weight / seoTitle / seoDescription /
// images / imageAlts / attributeIds / categoryIds / primaryCategoryId / brandId / tagIds），
// 改字段名等于改协议。
//
// 提交后回编辑页（PRG）而不是回列表：用户在这里改的是**这一个商品**，
// 弹回列表等于让他重新找一遍再点进来（与详情页的写操作同一口径）。
package producthttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"
)

// ProductEditPage GET /admin/products/edit：商品基本字段编辑页。
func (h *productPageHandle) ProductEditPage(c *gin.Context) {
	h.renderProductEditPage(c, strings.TrimSpace(c.Query("project")), strings.TrimSpace(c.Query("product")), "", false)
}

func (h *productPageHandle) renderProductEditPage(c *gin.Context, selected, productID, submitErr string, echo bool) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_edit", err)
		return
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	data := gin.H{
		"title":           MsgProductsTitle,
		"menu":            "products",
		"Projects":        projects,
		"SelectedProject": selected,
		"ProductID":       productID,
		"HasProduct":      false,
		"EditBlocked":     false,
		// 变体的两个抽屉（新建 / 生成组合）要归属仓下拉：仓库清单在取数段拿到后填进来
		// （下面 detail 读到之前先给空切片，模板的 len 判断自然跳过）。
		"WarehouseOptions": []gin.H{},
		// 回列表的链接与「取消」都回到用户来的地方（工程上下文保留）。
		"BackURL": productListURLFiltered(selected, "", "", 0, "", ""),
		// 状态下拉：当前商品读不出来时按「全部状态」那一档渲染（页面仍完整）。
		"Statuses": productStatusOptions(c, ""),
		"Err":      productPageErr(c),
		"Done":     productPageDone(c),
	}
	if submitErr != "" {
		data["Err"] = submitErr
		data["Done"] = ""
	}
	if selected != "" && productID != "" {
		// 取数顺序与列表页一致（分类 / 品牌 / 标签 / 仓库各取一次），
		// 因为行数据复用同一个 productRow 组装 —— 两页各算一遍必然分叉。
		flat, ferr := h.flatCategories(ctx, selected)
		if ferr != nil {
			shell.PageError(c, "product_edit", ferr)
			return
		}
		brands, berr := h.listBrands(ctx, selected)
		if berr != nil {
			shell.PageError(c, "product_edit", berr)
			return
		}
		tags, terr := h.listTags(ctx, selected)
		if terr != nil {
			shell.PageError(c, "product_edit", terr)
			return
		}
		warehouseOptions, werr := h.warehouseOptions(ctx, selected, shell.TranslateFor(c))
		if werr != nil {
			shell.PageError(c, "product_edit", werr)
			return
		}
		// 仓库清单要进模板（变体的两个抽屉用它渲染归属仓下拉）——
		// 只在 productRow 里用掉、忘了放进 data 的话，模板第一行的变量声明就会中断渲染。
		data["WarehouseOptions"] = warehouseOptions
		detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: productID, ProjectID: selected})
		if derr == nil && detail != nil {
			data["HasProduct"] = true
			row := h.productRow(ctx, selected, flat, brands, tags, warehouseOptions, detail, shell.TranslateFor(c))
			// productRow 的汇总段是给**列表列**用的（名称 / 状态 / 价格区间 / 分类 / 品牌 / 标签），
			// 编辑表单还要几个列表列不需要的字段（副标题 / 单位 / SEO 两栏）——
			// 在这里补，而不是往共享组装里塞：那会让列表页也背上只有编辑页才用的键。
			row["Subtitle"] = detail.Subtitle
			row["Unit"] = detail.Unit
			row["SEOTitle"] = detail.SEOTitle
			row["SEODescription"] = detail.SEODescription
			// 商品描述在库里是 {"html": "..."}（jsonb 对象），富文本字段要的是裸 HTML。
			// 这里破一次「表单字段名 = DTO json 标签」的例：两边形态本来就不同
			//（对象 vs HTML 字符串），硬对齐只会把包装逻辑推到前端散落脚本里。
			row["DescriptionHTML"] = productDescriptionHTML(detail.Description)
			data["Product"] = row
			data["Statuses"] = productStatusOptions(c, detail.Status)
			// 属性组勾选态（详情页用的是逗号分隔的 id 输入框，编辑页是勾选列表）：
			// 可选值来自工程属性组清单，勾选态由商品已引用的 id 决定。
			opts, aerr := h.attributeOptions(ctx, selected)
			if aerr != nil {
				// 选择器缺失与主动取消勾选在 POST 上同形；读故障时撤掉保存入口。
				data["EditBlocked"] = true
				data["Err"] = productInternalText(c, aerr)
			} else {
				data["AttributeChecks"] = checkedAttributeOptions(opts, detail.AttributeIDs)
			}
			// 图集与数值字段：表单里是文本（textarea / input），按行与可空文本回填。
			data["ImagesText"] = strings.Join(detail.Images, "\n")
			data["ImageAltsText"] = strings.Join(detail.ImageAlts, "\n")
			data["WeightText"] = nullableNumberText(detail.Weight)
			data["DefaultPriceText"] = nullableNumberText(detail.DefaultPrice)
			if echo {
				form := formEchoFrom(c)
				for field, key := range map[string]string{
					"name": "Name", "subtitle": "Subtitle", "slug": "Slug", "sku": "SKUCode",
					"unit": "Unit", "seoTitle": "SEOTitle", "seoDescription": "SEODescription",
				} {
					row[key] = form.value(field)
				}
				data["ImagesText"] = c.PostForm("images")
				data["ImageAltsText"] = c.PostForm("imageAlts")
				data["WeightText"] = c.PostForm("weight")
				data["DefaultPriceText"] = c.PostForm("defaultPrice")
				data["Statuses"] = productStatusOptions(c, form.value("status"))
				if checks, ok := data["AttributeChecks"].([]gin.H); ok {
					data["AttributeChecks"] = checkedProductEditOptions(checks, form.list("attributeIds"))
				}
				row["CategoryChecks"] = checkedProductEditOptions(row["CategoryChecks"].([]gin.H), form.list("categoryIds"))
				row["TagChecks"] = checkedProductEditOptions(row["TagChecks"].([]gin.H), form.list("tagIds"))
				row["PrimaryOptions"] = selectedProductEditOptions(row["PrimaryOptions"].([]gin.H), form.value("primaryCategoryId"))
				row["BrandOptions"] = selectedProductEditOptions(row["BrandOptions"].([]gin.H), form.value("brandId"))
			}
			// 捆绑容器：构成表在详情页只读展示，编辑入口在独立页 /admin/products/bundle ——
			// 这里只给一个链接标记（type=variant 时不给这个键，模板据 isset 整块跳过）。
			data["IsBundle"] = isBundleProduct(detail.Type)
			// 详情页模板面板：详情页只读展示绑定状态，写动作（进入自定义 / 编辑模板 /
			// 重新套用预设 / 回滚）都在本页 —— 与详情页共用同一份组装。
			data["TplPanel"] = h.detailTemplatePanel(ctx, selected, productID)
		}
	}
	c.HTML(http.StatusOK, "admin/product/product_edit.html", shell.Prepare(c, data))
}

func checkedProductEditOptions(options []gin.H, selected []string) []gin.H {
	for _, option := range options {
		id, _ := option["ID"].(string)
		option["Checked"] = containsString(selected, id)
	}
	return options
}

func selectedProductEditOptions(options []gin.H, selected string) []gin.H {
	for _, option := range options {
		id, _ := option["ID"].(string)
		option["Selected"] = id == selected
	}
	return options
}

// ProductsUpdate POST /admin/products/update：保存商品基本字段。
//
// 语义与 service 的 Update 一一对应（**整体替换** vs **不改**）：
//   - 文本字段总是提交（空串 = 清空该字段）；
//   - 多选字段（属性组 / 分类 / 标签）总是非 nil —— 一个都没勾是「解绑全部」，
//     不是「本次不改」（浏览器在没有任何勾选时根本不提交该字段，直接透传会把
//     「取消勾选全部」变成静默无操作）；
//   - 数值字段留空 = 不改（nil），不写 0：0 元与「没填」是两回事。
func (h *productPageHandle) ProductsUpdate(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	id := formProductID(c)
	if id == "" {
		c.Redirect(http.StatusFound, productListURL(projectID, listErrMark,
			productErrText(c, errors.New(productenums.ErrInvalidParam))))
		return
	}
	req := &productdto.UpdateReq{ID: id, ProjectID: projectID}

	// 名称必填：模板上有 required，但服务端不信任前端（缺了会静默保留旧名）。
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.Redirect(http.StatusFound, productEditLocation(projectID, id,
			productErrText(c, errors.New(productenums.ErrNameRequired))))
		return
	}
	req.Name = &name

	// URL 段：留空即不改（service 侧空串会被当成非法参数，且改名不该顺手清空路径）。
	if slug := strings.TrimSpace(c.PostForm("slug")); slug != "" {
		req.Slug = &slug
	}
	subtitle := strings.TrimSpace(c.PostForm("subtitle"))
	req.Subtitle = &subtitle
	unit := strings.TrimSpace(c.PostForm("unit"))
	req.Unit = &unit
	// 表单已不再提交 seoTitle / seoDescription（两者合并进商品名与副标题）。
	// **这里不能改成 req.SEOTitle = &""**：那会让「改个商品名顺手清空 SEO 标题」——
	// 两列保留着编辑者写过的历史值，不传即不改才是它们该有的归宿。
	// 商品描述：富文本字段给的是裸 HTML，入库形态是 {"html": "..."}。
	// 包装在这里做（不 trim：正文里的空白是有意义的排版）。
	if descHTML, derr := json.Marshal(map[string]string{"html": c.PostForm("descriptionHtml")}); derr == nil {
		req.Description = descHTML
	}
	if status := strings.TrimSpace(c.PostForm("status")); status != "" {
		req.Status = &status
	}
	// 主体 SKU：留空即不改（存量编码一律不重写；清空编码在 service 侧是明确拒绝的错误）。
	if sku := strings.TrimSpace(c.PostForm("sku")); sku != "" {
		req.SKUCode = &sku
	}
	if raw := strings.TrimSpace(c.PostForm("defaultPrice")); raw != "" {
		v, perr := parseFloat(raw)
		if perr != nil {
			h.productEditValidationFail(c, projectID, id,
				shell.TranslateFor(c)(productenums.ProductEditErrDefaultPriceInvalid, "默认价格格式无效，请输入数字"))
			return
		}
		req.DefaultPrice = &v
	}
	if raw := strings.TrimSpace(c.PostForm("weight")); raw != "" {
		v, perr := parseFloat(raw)
		if perr != nil {
			h.productEditValidationFail(c, projectID, id,
				shell.TranslateFor(c)(productenums.ProductEditErrWeightInvalid, "重量格式无效，请输入数字"))
			return
		}
		req.Weight = &v
	}
	// 图集与 alt：按行切分（textarea 一行一个），空文本 = 清空该字段。
	req.Images = splitFormLines(c.PostForm("images"))
	req.ImageAlts = splitFormLines(c.PostForm("imageAlts"))

	// 多选字段：**总是非 nil**（见方法注释）。
	req.AttributeIDs = postFormArrayAlways(c, "attributeIds")
	req.CategoryIDs = postFormArrayAlways(c, "categoryIds")
	req.TagIDs = postFormArrayAlways(c, "tagIds")
	primary := strings.TrimSpace(c.PostForm("primaryCategoryId"))
	req.PrimaryCategoryID = &primary
	brand := strings.TrimSpace(c.PostForm("brandId"))
	req.BrandID = &brand

	// 空 attributeIds 只有选项读取成功时才表示用户主动取消勾选。
	if _, err := h.attributeOptions(c.Request.Context(), projectID); err != nil {
		h.renderProductEditPage(c, projectID, id, productInternalText(c, err), true)
		return
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, productEditLocation(projectID, id, productErrText(c, err)))
		return
	}
	// 保存成功的回执走与其它页同一条读侧白名单（?done= 不是可信边界）：
	// 文案由 productNoticeTexts 登记，页面刷新后能看见「商品已保存」。
	c.Redirect(http.StatusFound, productEditLocationWith(projectID, id, "", productBulkTextOf(c, productSaved)))
}

func (h *productPageHandle) productEditValidationFail(c *gin.Context, projectID, id, msg string) {
	h.renderProductEditPage(c, projectID, id, msg, true)
}

// postFormArrayAlways 取同名多值字段，**总是**返回非 nil 切片。
//
// 「一个都没勾」与「本次不改这个字段」是两回事：UpdateReq 里 nil = 不改、
// 空数组 = 整体替换为空，而浏览器在没有任何勾选时根本不提交该字段
// （PostFormArray 给 nil）—— 直接透传会让「取消勾选全部」变成静默无操作。
func postFormArrayAlways(c *gin.Context, field string) []string {
	if vals := c.PostFormArray(field); len(vals) > 0 {
		return vals
	}
	return []string{}
}

// splitFormLines 把 textarea 的文本按行切成切片（去首尾空白、丢弃空行、**总是非 nil**）。
//
// 空文本给空切片而不是 nil：nil 在 UpdateReq 里表示「本次不改」，
// 而用户在编辑页清空图集是一个明确意图（清空），不是「什么都没做」。
func splitFormLines(raw string) []string {
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// nullableNumberText 可空数值 → 输入框文本（未设置给空串，不是 0）。
//
// 与列表展示用的 formatNullableAmount（空显示为 —）分开：— 是**展示**符号，
// 填进 input 的 value 会变成一个待提交的非法值。
func nullableNumberText(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// checkedAttributeOptions 属性组勾选列表：给每组补一个 Checked（商品是否引用它）。
func checkedAttributeOptions(options []gin.H, selected []string) []gin.H {
	out := make([]gin.H, 0, len(options))
	for _, o := range options {
		item := gin.H{}
		for k, v := range o {
			item[k] = v
		}
		id, _ := o["ID"].(string)
		item["Checked"] = containsString(selected, id)
		out = append(out, item)
	}
	return out
}

// productDescriptionHTML 从商品描述的 JSON 里取正文（富文本字段要裸 HTML）。
//
// 兼容两种存量形态：{"html": "..."} 与裸字符串。取不到就给空串 ——
// 这个值只用于**回填编辑器**，猜错会让编辑者看到别人的正文，比空着危险得多。
func productDescriptionHTML(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if s, ok := asMap["html"].(string); ok {
			return s
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return ""
}
