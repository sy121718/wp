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
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/facing"
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

// FormValueHas 同名多值里是否存在 want（勾选态判据）。
//
// **必须遍历全部值，不能用 url.Values.Get**：Get 返回第一个，而「同名隐藏域打底 +
// 同名复选框」的形态（hidden "0" 在前、checkbox "1" 在后）让 Get 恒取到 "0" ——
// 于是勾选永远为假，而且不报任何错：页面照样 200，只是用户的勾选被静默丢弃。
func FormValueHas(vals url.Values, name, want string) bool {
	for _, v := range vals[name] {
		if strings.TrimSpace(v) == want {
			return true
		}
	}
	return false
}

// attrRowPrefix 属性值行的表单字段前缀（values[0].id → "values[0]."）。
const attrRowPrefix = "values["

// AttrRowIndexes 表单里出现过的行下标（数值升序）。
//
// n 不保证连续（删了中间一行再提交），故先收集再排序；按 map 遍历顺序取会让
// 「删中间一行再提交」后的行序随机跳变。
func AttrRowIndexes(vals url.Values) []int {
	indexes := make([]int, 0)
	seen := map[int]bool{}
	for key := range vals {
		if !strings.HasPrefix(key, attrRowPrefix) {
			continue
		}
		rest := key[len(attrRowPrefix):]
		close := strings.IndexByte(rest, ']')
		if close <= 0 || !strings.HasPrefix(rest[close:], "].") {
			continue
		}
		idx, err := strconv.Atoi(rest[:close])
		if err != nil || idx < 0 || seen[idx] {
			continue
		}
		seen[idx] = true
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	return indexes
}

// AttrEchoRowsFromForm 从表单的 values[n].* 重建有序行（值编辑器的 add / remove 用）。
//
// 行编辑只重建**原始值与位置**：归一 / 去重 / 排序仍由 service 在落库时决定。
func AttrEchoRowsFromForm(vals url.Values) []AttrValueFormRow {
	rows := make([]AttrValueFormRow, 0)
	for _, idx := range AttrRowIndexes(vals) {
		base := fmt.Sprintf("values[%d].", idx)
		rows = append(rows, AttrValueFormRow{
			Index: idx, ID: vals.Get(base + "id"), Key: vals.Get(base + "key"),
			Label: vals.Get(base + "label"), Sort: vals.Get(base + "sort"),
			Enabled: FormValueHas(vals, base+"enabled", "1"),
		})
	}
	return rows
}

// AttributeRowsFormReq 属性值编辑片段（POST /admin/product-attributes/value-rows）的入参。
type AttributeRowsFormReq struct {
	// Action add / remove；其余值按「只重建」处理。
	Action string `form:"action"`
	// RemoveIndex remove 时删除的行下标；非法值按 -1（不删）处理。
	RemoveIndex string `form:"removeIndex"`
	// GroupID 值编辑器所属的属性组；"new" 表示新建抽屉。
	GroupID string `form:"groupId"`
	// Rows 回渲染的行（含下标、原始值与勾选态）。
	Rows []AttrValueFormRow
}

// BindForm 收 values[n].* 行（带下标，反射绑定覆盖不到）。
func (r *AttributeRowsFormReq) BindForm(vals url.Values) error {
	r.Rows = AttrEchoRowsFromForm(vals)
	return nil
}

// RemoveIndexValue removeIndex 的解析结果（非法 / 缺省 → -1）。
func (r *AttributeRowsFormReq) RemoveIndexValue() int {
	n, err := strconv.Atoi(strings.TrimSpace(r.RemoveIndex))
	if err != nil {
		return -1
	}
	return n
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

// PricingFormReq 定价表单（页面 POST：试算 / 批量改价抽屉）。
//
// 规则参数**平铺在顶层**（multiplier / amount / margin），不套 ruleParams 子对象：
// 页面表单本来就是平铺的，而「哪个规则用哪个参数」的判定只有 Rule() 一份 ——
// 抽屉若另抄一份，最先出问题的是「填了 amount 却被当成 multiplier」这类静默错配。
type PricingFormReq struct {
	ProjectID  string `form:"projectId"`
	RuleType   string `form:"ruleType"`
	Multiplier string `form:"multiplier"`
	Amount     string `form:"amount"`
	Margin     string `form:"margin"`
	Rounding   string `form:"rounding"`
	Scope      string `form:"scope"`
	TargetID   string `form:"targetId"`
	// —— 筛选集范围条件（Scope=filter 时生效）——
	Status     string `form:"status"`
	Keyword    string `form:"keyword"`
	CategoryID string `form:"categoryId"`
	BrandID    string `form:"brandId"`
	TagID      string `form:"tagId"`
	Note       string `form:"note"`
}

// Form 回渲染用的表单值（试算失败时原地带回，用户接着改）。
func (r *PricingFormReq) Form() PricingForm {
	return PricingForm{
		ProjectID: r.ProjectID, RuleType: r.RuleType, Multiplier: r.Multiplier,
		Amount: r.Amount, Margin: r.Margin, Rounding: r.Rounding, Scope: r.Scope,
		TargetID: r.TargetID, Status: r.Status, Keyword: r.Keyword,
		CategoryID: r.CategoryID, BrandID: r.BrandID, TagID: r.TagID, Note: r.Note,
	}
}

// PricingParams 按规则类型从平铺的字符串参数拼规则参数（只取本类型用得到的键）。
//
// 非法数字**不给键** —— service 统一报「参数缺失或不是数字」，这里不重复一套校验；
// NaN / 正负无穷同档（它们进了算价会得到一整批 NaN 价格）。
func PricingParams(ruleType, multiplier, amount, margin string) json.RawMessage {
	params := map[string]any{}
	switch ruleType {
	case productenums.PricingRuleCostMultiple:
		if v, err := OptFloat("multiplier", multiplier); err == nil && v != nil {
			params["multiplier"] = *v
		}
	case productenums.PricingRuleCostMarkup, productenums.PricingRuleFixedPrice:
		if v, err := OptFloat("amount", amount); err == nil && v != nil {
			params["amount"] = *v
		}
	case productenums.PricingRuleTargetMargin:
		if v, err := OptFloat("margin", margin); err == nil && v != nil {
			params["margin"] = *v
		}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

// Rule 归一为 service 入参：只取本规则类型用得到的参数。
//
// 非法数字**不给键** —— service 统一报「参数缺失或不是数字」，这里不重复一套校验；
// NaN / 正负无穷同档（它们进了算价会得到一整批 NaN 价格）。
func (r *PricingFormReq) Rule() *PricingRuleReq {
	return &PricingRuleReq{
		ProjectID: r.ProjectID, RuleType: r.RuleType, Rounding: r.Rounding,
		Scope: r.Scope, TargetID: r.TargetID, Status: r.Status, Keyword: r.Keyword,
		CategoryID: r.CategoryID, BrandID: r.BrandID, TagID: r.TagID,
		RuleParams: PricingParams(r.RuleType, r.Multiplier, r.Amount, r.Margin),
	}
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

// TranslationRowErrorResp 译文工作台的一行错误：Label 是「哪一行」（语境名，可空 = 页面级），
// Msg 是结论事实。
//
// 不把 Label 拼进 Msg.Fallback：拼了就成成品串，模板再取词也救不回来
// （中文永远粘在英文词条前面）。也不把 Label 塞进 Msg.Args —— 那个位置留给词条自身的 `%s`。
type TranslationRowErrorResp struct {
	Label string
	Msg   facing.Msg
}

// AttrValueFormRow 留存失败回显所需的原始索引与控件值；写入解析仍保持原有的空行过滤。
type AttrValueFormRow struct {
	Index   int
	ID      string
	Key     string
	Label   string
	Sort    string
	Enabled bool
}

// BundleSourcePart 成员来源快照的**一段**：段的种类 + 该段的数据值。
//
// 段交给模板：分隔符「·」、每一段前面的词条（从仓库选 / 仓库 SKU / 外部编码）都在
// 渲染期决定。后端只说「这是第几类段、值是什么」—— 拼成一句话就等于把语言锁死在一种。
type BundleSourcePart struct {
	// Kind 段种类：source（来源种类）/ warehouseSku（仓库 SKU）/ externalSku（外部编码）。
	Kind string
	// Value 该段的数据值（来源种类常量 / SKU 编码 / 对方编码）。
	Value string
}

// BundleSourcePart 的段种类取值（模板按它选词条）。
const (
	BundleSourcePartSource       = "source"
	BundleSourcePartWarehouseSKU = "warehouseSku"
	BundleSourcePartExternalSKU  = "externalSku"
)

// PricingForm 定价表单的取值（试算后要原样回填，用户接着点应用）。
type PricingForm struct {
	ProjectID  string
	RuleType   string
	Multiplier string
	Amount     string
	Margin     string
	Rounding   string
	Scope      string
	TargetID   string
	Status     string
	Keyword    string
	CategoryID string
	BrandID    string
	TagID      string
	Note       string
}

// ProductCreateFormReq 商品建表单的取数入参。
type ProductCreateFormReq struct {
	// ProjectID 表单里的工程 id；空串 = 没给，候选一律给空列表（表单照常渲染）。
	ProjectID string
}

// ProductPricingPageReq 定价工具页的取数入参。
//
// 三个字段都是**写路径的产物**（试算失败要原地重渲染，把用户填的表单与试算结果带回来）：
// 读模型不认识 gin.Context，提交值只能由控制器读出来后经这里传进去 —— 与编辑页的表单回显
// 同一分工（service 只认数据，不认请求）。
type ProductPricingPageReq struct {
	// ProjectID 查询串里的工程 id；空串 = 取工程清单的第一个。
	// Form.ProjectID 非空时以表单为准（试算/应用路径带着它提交）。
	ProjectID string `form:"project"`
	// Form 表单回显（试算路径给本次提交值；首次 GET 为 nil = 用默认初值）。
	Form *PricingForm
	// Preview 试算结果（只有试算路径给；nil = 还没试算过）。
	Preview *PricingPreviewResp
	// ErrMsg 试算失败的**成品串**（例外形态：这一句来自 productErrText，已按当前语言翻好，
	// 与「提示页出口」同类 —— 表单要留在眼前，所以不跳转）。
	ErrMsg string
}

// ProductAttributesPageReq 属性库整页的取数入参（四个都是查询串参数）。
//
// Variation 的归一不在这里（`?variation=yes` 会被归一成「全部」，见 service 的读模型）：
// 归一写在控制器时，筛选回显与过滤条件会各拿一份原始串。
type ProductAttributesPageReq struct {
	ProjectID string `form:"project"`
	Keyword   string `form:"keyword"`
	Variation string `form:"variation"`
	// Page 原始页码；下限保护与按总数收敛都在读模型里。
	Page int `form:"page"`
}

// ProductDetailTemplatePageReq 「详情页模板」整页的取数入参（两个查询串参数）。
type ProductDetailTemplatePageReq struct {
	// ProjectID 工程 id；空串 = 取工程清单的第一个。
	ProjectID string `form:"project"`
	// ProductID 商品 id；空串 = 还没选商品（菜单入口），控制器据此走引导出口。
	ProductID string `form:"product"`
}

// ProductBundlePageReq 捆绑配置页的取数入参。
//
// 五个上下文字段（全部来自 query，页面用 GET 重新渲染）：
//
//	· ProjectID / ProductID 页面的两个选择器（入口有两套字段名，控制器认完再传进来）；
//	· Source / SourceProductID / SourceWarehouseID 成员来源面板的当前上下文
//	  （用 GET 重新渲染，不是前端拼参数 —— 属性值勾选清单与仓库 SKU 候选都由服务端给）。
//
// 没有「提交快照」这一档：保存已迁到 JSON 写基座，基座在原页发 fetch、失败只写结果区，
// 用户的输入从未离开 DOM，因此页面装载只回答「库里配了什么」。
type ProductBundlePageReq struct {
	ProjectID       string `form:"project"`
	ProductID       string `form:"product"`
	Source          string `form:"source"`
	SourceProductID string `form:"sourceProduct"`
	// SourceWarehouseID 选中来源仓（仓库来源面板据此只列那个仓的一屏候选）。
	SourceWarehouseID string `form:"sourceWarehouse"`
}

// BindQuery 兼容捆绑页入口的两套键名：菜单与列表页给 project/product，
// 片段与旧链接给 projectId/productId（见 ProductBundlePageReq 的注释）。
// 两套都空时保持空串 —— 页面回落到工程清单的第一个。
func (r *ProductBundlePageReq) BindQuery(q url.Values) error {
	if r.ProjectID == "" {
		r.ProjectID = strings.TrimSpace(q.Get("projectId"))
	}
	if r.ProductID == "" {
		r.ProductID = strings.TrimSpace(q.Get("productId"))
	}
	return nil
}

// BundleOptionResp 成员表的一行（渲染行，非原始配置）。
//
// 与原始 BundleOption 分开：Available（真源可用量）与 SKULabel 都是这一屏才算得出的，
// 混进配置结构会让「配置里存的字段」与「页面显示的字段」看起来是同一件事。
type BundleOptionResp struct {
	VariantID  string
	SKULabel   string
	Required   bool
	DefaultQty int
	MinQty     int
	MaxQty     int
	// Available 真源可用量；新增的空白行没有可用量可言（还没选 SKU），恒为 0。
	Available int
	// 来源快照（仅作溯源与展示，不是身份）：四个字段原样回填进隐藏域；
	// SourceParts 是展示用的段（种类 + 数据值），词条在模板取。
	SourceKind   string
	WarehouseID  string
	WarehouseSKU string
	ExternalSKU  string
	SourceParts  []BundleSourcePart
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
type SaveProductTranslationsReq struct {
	ProjectID string
	ProductID string
	Lang      string
	Contexts  []string
	Hashes    []string
	Targets   []string
}

// ProductCategoryParentsReq 分类抽屉「父级」候选的查询串参数（搜索式，一屏一页）。
type ProductCategoryParentsReq struct {
	ProjectID string `form:"project"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
}

// CategoryParentOptionsResp 父级候选片段的数据根。
//
// 是 map 而不是结构体：片段模板用 .["t"] 取词，Jet 的索引写法只对 map 生效。
type CategoryParentOptionsResp map[string]any

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

// ProductCategoriesPageReq 分类管理页的取数入参。
type ProductCategoriesPageReq struct {
	ProjectID string `form:"project"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
}

// ProductBrandsPageReq 品牌管理页的取数入参。
type ProductBrandsPageReq struct {
	ProjectID string `form:"project"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
}

// ProductTagsPageReq 标签管理页的取数入参。
type ProductTagsPageReq struct {
	ProjectID string `form:"project"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
}
