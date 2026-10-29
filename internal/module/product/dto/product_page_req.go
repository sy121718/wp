// product_page_req.go — 后台页面（GET 页面 / POST 片段）的入参形状与字段解析。
//
// 页面请求与接口请求走同一个绑定入口（pkg/binding）：控制器里不再出现 c.Query /
// c.PostForm，字段名（表单 name、查询串键名）只在这里声明一次。
//
// 页面入参有两类形态，分别有明确的归属：
//   - **平铺字段**：结构体上 `form:"…"` 就能表达，绑定交给反射；
//   - **带前缀 / 带下标的字段**（attr:<属性组 id>、values[n].id）：反射表达不了，
//     由 DTO 实现 BindForm，解析规则留在 DTO 里 —— 控制器不碰 url.Values。
package productdto

import (
	"net/url"
	"sort"
	"strings"

	json "go_wp/pkg/jsonx"
)

// variantSelectionPrefix 变体勾选在表单里的键前缀（attr:<属性组 id> = 属性值 id 多值）。
//
// 生成（落库）、预览（不落库）、捆绑成员解析三条路径共用同一个前缀与同一条收拢规则：
// 各写一份必然分叉（一个认前缀、另一个漏读，表现为「预览有行、保存却没数据」）。
const variantSelectionPrefix = "attr:"

// SelectionsFromForm 收表单里按前缀提交的勾选（键升序，稳定输出）。
func SelectionsFromForm(vals url.Values) []VariantSelectionReq {
	keys := make([]string, 0, len(vals))
	for key := range vals {
		if strings.HasPrefix(key, variantSelectionPrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]VariantSelectionReq, 0, len(keys))
	for _, key := range keys {
		attrID := strings.TrimPrefix(key, variantSelectionPrefix)
		if attrID == "" {
			continue
		}
		out = append(out, VariantSelectionReq{AttributeID: attrID, ValueIDs: vals[key]})
	}
	return out
}

// ScorePanelReq 编辑期评分片段（三个面板共用：工程 + 实体 id）。
type ScorePanelReq struct {
	ProjectID string `form:"projectId"`
	// ProductID 商品评分面板用。
	ProductID string `form:"productId"`
	// ID 分类 / 品牌评分面板用。
	ID string `form:"id"`
}

// VariantPreviewFormReq 生成组合「预览」的表单入参（不落库）。
type VariantPreviewFormReq struct {
	ProductID   string `form:"productId"`
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	// OptionValues 清单里已有组合的 option_values（每行一条 JSON 串）。
	OptionValues []string `form:"optionValues"`
	// Selections 属性组合勾选（attr:<属性组 id>）。
	Selections []VariantSelectionReq
}

// BindForm 收 attr: 前缀的勾选。
func (r *VariantPreviewFormReq) BindForm(vals url.Values) error {
	r.Selections = SelectionsFromForm(vals)
	return nil
}

// DetailTemplatePreviewReq 详情页模板预览（POST /admin/products/template/preview）。
type DetailTemplatePreviewReq struct {
	ProjectID  string `form:"projectId"`
	ProductID  string `form:"productId"`
	TemplateID string `form:"templateId"`
}

// ProductNewPageReq 商品新建整页的取数入参。
type ProductNewPageReq struct {
	// ProjectID 查询串里的工程 id；空串 = 取工程清单的第一个。
	ProjectID string `form:"project"`
}

// ProductListPageReq 商品列表整页的取数入参。
//
// Page 是**请求给的原始页码**：`?page=abc` 在绑定期就是 400（类型不合法是参数问题），
// `?page=-3` / 缺省 0 由读模型的下限保护退回 1 —— 页码归一只有一处，在 service。
type ProductListPageReq struct {
	// ProjectID 查询串里的工程 id；空串 = 取工程清单的第一个。
	ProjectID string `form:"project"`
	Keyword   string `form:"keyword"`
	Status    string `form:"status"`
	Page      int    `form:"page"`
}

// ProductDetailPageReq 商品详情整页的取数入参。
type ProductDetailPageReq struct {
	// ProjectID 查询串里的工程 id；空串 = 取工程清单的第一个。
	ProjectID string `form:"project"`
	// ProductID 查询串里的商品 id；空串 = 没给参数（与「商品已被删除」同一档：404）。
	ProductID string `form:"product"`
}

// ProductEditPageReq 商品编辑整页的取数入参。
type ProductEditPageReq struct {
	// ProjectID 查询串 / 表单里的工程 id；空串 = 取工程清单的第一个。
	ProjectID string `form:"project"`
	// ProductID 要编辑的商品 id；空串 = 没给参数 → **200 空表单**（不是 404）：
	// 编辑页是写入口，让运营看到一个空表单比看到一个 404 有用（详情页反过来）。
	// 给了 id 但库里没有才是 404（与详情页同一档：空态、不进失败槽）。
	ProductID string `form:"product"`
}

// ProductDetailTemplatePageReq 「详情页模板」整页的取数入参（两个查询串参数）。
type ProductDetailTemplatePageReq struct {
	// ProjectID 工程 id；空串 = 取工程清单的第一个。
	ProjectID string `form:"project"`
	// ProductID 商品 id；缺失由绑定入口返回参数错误。
	ProductID string `form:"product" binding:"required"`
}

// ProductTranslationsPageReq 商品译文工作台的取数入参（工程 / 商品 / 目标语言，三者都可空）。
type ProductTranslationsPageReq struct {
	ProjectID string `form:"project"`
	ProductID string `form:"product"`
	Lang      string `form:"lang"`
}

// SaveProductTranslationsReq 保存提交：三组并行数组按行一一对应（与表单字段同形）。
//
// 数组原样带过来，长度对齐与逐行校验都在读模型侧做 —— 控制器只负责把表单读成数组。
// SaveProductTranslationsReq 整表提交的译文（JSON 体；字段名与表单一致，前端只声明 data-write-array）。
//
// 三组并行数组按行一一对应：长度对齐与逐行校验在读模型侧做（那里才有候选原文）。空 target
// 表示「本行不写入」（不删除库中已有译文），因此数组里必须保留空串占位 —— 剔掉会让后面所有行
// 错位一格，而错位在服务端看起来只是「这一行的译文与原文对不上」。
type SaveProductTranslationsReq struct {
	ProjectID string   `json:"projectId" form:"projectId"`
	ProductID string   `json:"productId" form:"productId" binding:"required"`
	Lang      string   `json:"lang" form:"lang"`
	Targets   []string `json:"rowTarget" form:"rowTarget"`
	Contexts  []string `json:"rowContext" form:"rowContext"`
	Hashes    []string `json:"rowHash" form:"rowHash"`
}

// ProductPreviewFrameReq 商品预览帧的查询串参数（键名沿用编辑页里的 productId）。
type ProductPreviewFrameReq struct {
	ProjectID string `form:"project"`
	ProductID string `form:"productId"`
}

// ProductSeoDrawerReq 商品 SEO 抽屉的查询串参数（与预览帧同形）。
type ProductSeoDrawerReq struct {
	ProjectID string `form:"project"`
	ProductID string `form:"productId"`
}

// ServiceReq 预览入参 → service 的请求形状。
//
// 清单里已有的组合原样回传给 service 去重（每行一条 JSON 串，空行跳过）：前端不需要懂 optionKey。
func (r *VariantPreviewFormReq) ServiceReq() *PreviewVariantReq {
	req := &PreviewVariantReq{
		ProductID:   r.ProductID,
		ProjectID:   r.ProjectID,
		WarehouseID: r.WarehouseID,
		Selections:  r.Selections,
	}
	for _, raw := range r.OptionValues {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			req.ExistingOptionValues = append(req.ExistingOptionValues, json.RawMessage(trimmed))
		}
	}
	return req
}
