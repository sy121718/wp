// product_page_resp.go — 后台页面的回执形状（页面视图 / 片段视图 / 行与选项值对象）。
//
// 与 product_page_req.go 成对：入参一边、回执一边。原先的 product_view.go 把两边混在一个文件里，
// 定位一个类型要先读它是进还是出；拆开之后「页面 GET 的入参」与「渲染用的整页数据」各自只有一处。
//
// **导出字段名就是模板契约**：internal/templates/admin 下 product*.jet 里的 .Field 必须在这里有
// 同名导出字段，字段被改名或删掉 = 渲染期 500（htmx 不 swap，点一下没反应），
// 双向对账由 inbound/http 下的 *_template_test.go 用反射 + 模板源码钉住。
//
// 页面出口用 PageStatus() / PageNoStore() 两个方法给状态（见 internal/view.Result）：
// 零值 = 「200 且可缓存」，实现它们的是**失败态为 nil-safe 的整页视图**。

package productdto

import (
	"net/http"

	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/pagination"
)

// ProductNewPageResp 商品新建整页的渲染视图。
type ProductNewPageResp struct {
	// Title / Menu 是 layout 的小写壳键（`{{.title}}` / `{{.menu}}`）。
	// Title 放 enums 的 i18n key，翻译由 adminui.Prepare 做（放 key 而不是译文，
	// 是因为译文按请求语言取）。
	Title string `view:"title"`
	Menu  string `view:"menu"`

	// Projects 工程清单与本次选中的工程：整页的工程上下文（失败页也带着它，
	// 模板的失败分支不读，但页壳与返回链接的上下文不因此缺一块）。
	// 形状与工程契约的 List 返回值一致（不为了「视图里用指针」多转一趟）。
	Projects        []projectdto.ProjectResp
	SelectedProject string

	// —— 建表单片段要的三份选项（事实 + 词条 key，取词在渲染期）——
	WarehouseOptions    []WarehouseOptionResp
	AttributeOptions    []AttributeOptionResp
	WarehouseSKUOptions []WarehouseSKUGroupResp

	// —— 右侧模板卡片区 ——
	// TemplatesAvail 为假 = 模板能力未装配（未注入端口）；为真而 Templates 为空 =
	// 「读到了、没有模板」。两档在页面上是两句不同的提示，不能合并。
	Templates         []EntityTemplateOptionResp
	DefaultTemplateID string
	TemplatesAvail    bool

	// LoadFailed 本次请求**没读到**（不是「读到了、只是空的」）；LoadError 是归口后的
	// 当前语言文字，非空即这一页失败了。失败优先于空态（docs/rules/template-boundary.md）。
	LoadFailed bool
	LoadError  string

	// HTTPStatus 这一屏的 HTTP 档位：200 读到 / 400 参数不合法 / 500 主数据没读到。
	// 导出是因为写它的 service 在另一个包（dto 的写者只有 service 的读模型）。
	// 它会被 UI 线的 ViewMap 摊平成一个模板键，但没有任何模板读它 —— 摊平的键
	// 多一个不影响渲染，页面上也不会出现它。
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductNewPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
func (v ProductNewPageResp) PageNoStore() bool { return v.LoadFailed }

// ProductListPageResp 商品列表整页的渲染视图（读模型产出 + 行装配原料 + 控制器补的派生值）。
//
// 它是这一屏**唯一的模板数据源**（UI 线的 `adminui.Page(tpl, h)` 只收这一份视图，没有额外 args），
// 因此字段分三类，各自有明确的写者：
//   - **service 的读模型写**：Projects / SelectedProject / Filter* / Page / Limit / Total /
//     RowDetails / Categories / Brands / Tags / 三组选项 / Rules / Roundings（取数结果）；
//   - **控制器写**：Title / Menu（页壳键，title 的 i18n key 常量是 http 包的 MsgProductsTitle）、
//     Form（改价表单初值）/ Statuses（状态下拉）/
//     Pagination / ListQuery —— 这些的**计算工具还被别的页面共用**（详情页回显 / 独立定价页），
//     搬进 service 等于让那几页各留一份，理由见 service/product_list_page.go 文件头；
//     **Products（成品行）不在此列** —— 行装配已下沉进读模型（service/product_row.go），
//     控制器不再拼行；只服务行装配的分类树扁平行（Flat）随之下沉，视图不再有那个字段；
//   - **控制器在失败时写**：LoadError（按当前语言归口；LoadFailed / HTTPStatus 由读模型给）。
type ProductListPageResp struct {
	// Title 放 enums 的 i18n key（取词在渲染期，理由同 ProductNewPageResp）。
	Title string `view:"title"`
	Menu  string `view:"menu"`

	// Projects 工程清单与本次选中的工程：整页的工程上下文（失败页也带着它）。
	Projects        []projectdto.ProjectResp
	SelectedProject string

	// FilterKeyword / FilterStatus 回显；Statuses 是下拉选项；Page / Limit / Total 是分页
	// **事实**（基地址与翻页链接由 ListQuery + Pagination 承载，本层不拼 URL）。
	FilterKeyword string
	FilterStatus  string
	Statuses      []StatusOptionResp
	Page          int
	Limit         int
	Total         int64

	// 行装配的原料：RowDetails 是**本页的列表项**（P0-1 之后不再逐行取详情 ——
	// 逐商品 s.Get 已删除，行缺的挂载事实与评分投影按本页 id 批量补，
	// 见 service/product_row.go 的 productRowsFromList）；
	// Categories 是**分类树**（分类选择器按树读，见 products.html）。行本身由读模型装配进
	// Products。
	RowDetails []*ProductResp
	Categories []*CategoryResp
	Brands     []*BrandResp
	Tags       []*TagResp

	// 页面上三处抽屉/表单要的选项（事实 + 词条 key，取词在渲染期）。
	WarehouseOptions    []WarehouseOptionResp
	AttributeOptions    []AttributeOptionResp
	WarehouseSKUOptions []WarehouseSKUGroupResp

	// 批量改价抽屉的规则清单、尾数处理清单与表单初值。
	Rules     []*PricingRuleTypeResp
	Roundings []*PricingRoundingOptionResp
	Form      PricingForm

	// Products 成品行（读模型装配，形状见 ProductRowResp）；Pagination 分页条事实；
	// ListQuery 筛选上下文（表单 action 的 query，写动作失败时由 pkg/backpath 读回，
	// 见 product_jump.go 的 productListBackKeys）。
	Products   []ProductRowResp
	Pagination pagination.Page
	ListQuery  string

	// LoadFailed 本次请求**没读到**（不是「读到了、只是空的」）；LoadError 由控制器
	// 按当前语言归口后填。
	LoadFailed bool
	LoadError  string

	// HTTPStatus 这一屏的档位：200 读到 / 400 参数不合法 / 500 主数据没读到（导出理由同
	// ProductNewPageResp.HTTPStatus）。
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductListPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
func (v ProductListPageResp) PageNoStore() bool { return v.LoadFailed }

// StatusOptionResp 状态下拉的一项（值 + 词条 key + 是否选中）。
//
// LabelKey / LabelFallback 而不是成品译文的理由同 WarehouseOptionResp：取词在渲染期，
// 控制器提前翻好就把这一句锁死在一种语言（英文界面上留中文，不报错、门禁也不红）。
type StatusOptionResp struct {
	Value         string
	LabelKey      string
	LabelFallback string
	Selected      bool
}

// ProductDetailPageResp 商品详情整页的渲染视图（读模型产出 + 行装配原料 + 控制器补的派生值）。
//
// 字段的写者与 ProductListPageResp 同一套约定：
//   - **service 写**：Projects / SelectedProject / ProductID / HasProduct / WarehouseOptions /
//     Detail / Categories / Brands / Tags（取数结果）；
//   - **控制器写**：Title / Menu（页壳）、BackURL（URL 形状属 HTTP）、Bundle / TplPanel
//     （两块面板各自的取数与组装）；
//   - **读模型写行**：Product 成品行由 service 的行装配产出（service/product_row.go），
//     控制器只在 type=bundle 时往里补 Bundle；
//   - **控制器在失败时写**：LoadError。
type ProductDetailPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string
	ProductID       string
	// HasProduct 为假 = 没读到商品（不存在 / 没给参数）：模板据此走空态分支，
	// 与「读不出来」（LoadFailed）是两档 —— 前者再试也没用，后者可能重试就成功。
	HasProduct bool

	// WarehouseOptions 行数据组装（库存列）要按工程仓库清单补齐未入库的仓 —— 与列表页 /
	// 编辑页共用同一份组装，取数口径不能分叉。
	WarehouseOptions []WarehouseOptionResp

	// —— 行装配原料与成品 ——
	// Detail 是这一行的商品详情；Categories 是**分类树**（分类选择器按树读）；行本身由读模型
	// 装配进 Product（service/product_row.go，三页共用同一份组装）。
	Detail     *ProductResp
	Categories []*CategoryResp
	Brands     []*BrandResp
	Tags       []*TagResp

	// —— 装配的产物 ——
	// Product 是成品行（读模型写，其中的 Bundle 面板由控制器在 type=bundle 时补）；
	// TplPanel 是详情模板面板（控制器组装）。
	Product  *ProductRowResp
	TplPanel map[string]any
	// BackURL 回列表的链接（带工程上下文，不掉回默认工程）。
	BackURL string

	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductDetailPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 非 200 的档位都不该被缓存 —— **404 也一样**：商品可能刚被删，也可能刚被建回来，
// 重试必须真的回到服务端确认。这与「失败页」不是同一件事：404 走的是模板自带的空态分支
// （那是「读到了、库里没有」，不是「没读到」）。
func (v ProductDetailPageResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductEditPageResp 商品编辑整页的渲染视图（读模型产出 + 行装配原料 + 控制器补的派生值与回显）。
//
// 与 ProductDetailPageResp 的两点不同：
//   - 多一组 AttributeOptions —— 编辑表单的勾选列表用属性组候选，详情页用的是逗号分隔的 id 输入框；
//   - 多一个**控制器侧**的字段：EditBlocked（属性组读不到时撤掉保存入口）。它回答的是
//     「能不能改」，不是「库里有什么」，本层不参与。
//
// **没有表单回显（echo）**：保存已迁到 JSON 写基座，失败只在原页的结果区出现，
// 用户的输入从头到尾没离开过 DOM —— 服务端不再有「拿这次提交覆盖读回来的值」这一层。
type ProductEditPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string
	ProductID       string
	// HasProduct 为假 = 没读到商品：模板据此走空态分支（与详情页同一档）。
	HasProduct bool

	WarehouseOptions []WarehouseOptionResp
	AttributeOptions []AttributeOptionResp

	// —— 行装配原料与成品 ——
	// Detail 是这一行的商品详情；Categories 是**分类树**（分类选择器按树读）；行本身由读模型
	// 装配进 Product（service/product_row.go，三页共用同一份组装）。
	Detail     *ProductResp
	Categories []*CategoryResp
	Brands     []*BrandResp
	Tags       []*TagResp

	// —— 装配的产物 ——
	// Product 是成品行（读模型写）；TplPanel 是详情模板面板。
	Product  *ProductRowResp
	TplPanel map[string]any
	BackURL  string

	// Err 表单校验失败 / 选项读取失败的原地重渲染文案（控制器填）。
	Err string
	// EditBlocked 属性组读不到时撤掉保存入口：选择器缺失与主动取消勾选在 POST 上同形，
	// 读故障时不能装作「用户取消了全部引用」。
	EditBlocked bool
	// AttributeOptionsErr 属性组候选读取失败的原因（模板不读）。
	// 控制器据此把 EditBlocked 置真并按当前语言归口文案 —— 本层不认识 HTTP，只交出事实。
	AttributeOptionsErr error

	// —— 表单字段的回填文本与勾选态（控制器算；echo=true 时用**这次提交的值**覆盖）——
	// Statuses 当前选中档；AttributeChecks 属性组勾选列表；四个 *Text 是图集与数值字段的
	// 文本形态（表单里是 textarea / input，库里是可空数值与切片）。
	Statuses         []StatusOptionResp
	AttributeChecks  []AttributeCheckOptionResp
	ImagesText       string
	ImageAltsText    string
	WeightText       string
	DefaultPriceText string
	// IsBundle 捆绑容器：构成表在详情页只读展示，编辑入口在独立页 —— 这里只给链接标记
	//（type=variant 时为假，模板据 isset 整块跳过）。
	IsBundle bool

	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductEditPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
//
// 判据与详情页一致：**非 200 一律 no-store**。404「商品不存在」不走失败槽（那是「读到了、
// 库里没有」，模板给的是空态），但它同样是一次不该被缓存的结果 —— 商品刚被建回来时，
// 缓存的 404 会把运营挡在门外。
func (v ProductEditPageResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// WarehouseOptionResp 仓库下拉 / 勾选项的渲染视图。
//
// Label 只拼**数据**（仓名 + 编码）；「默认仓」后缀是词条，用 DefaultSuffixKey +
// DefaultSuffixFallback 交给模板取词 —— 控制器提前翻好就等于把这一句锁死在一种语言
// （英文界面上留中文，不报错、门禁也不红）。
//
// DefaultSuffixKey 的词条真源在库存模块（enums 与词条都只有一条）：两个页面显示
// 同一个后缀，改词条要两处同步。
type WarehouseOptionResp struct {
	ID        string
	Code      string
	Name      string
	Label     string
	IsDefault bool

	DefaultSuffixKey      string
	DefaultSuffixFallback string
}

// WarehouseSKUItemResp 「从仓库选」的一个候选货。
type WarehouseSKUItemResp struct {
	// SKUCode 一律是**裸码**（仓库侧的名字，不带仓码前缀）：前端把它填进「SKU 编码」框时，
	// 服务端按 (仓, 裸码) 复核并复用那一行 —— 带上前缀会被当成一条新货去建行。
	SKUCode string
	// ExternalSKU 该仓登记的对方编码（可能为空）。
	ExternalSKU string
	// Label 候选的显示串（我们的 SKU，对方编码跟在后面 —— 运营手上可能是其中任意一个）。
	Label string
}

// WarehouseSKUGroupResp 一个仓的候选货集合（模板按仓分组渲染 optgroup / datalist）。
type WarehouseSKUGroupResp struct {
	WarehouseID string
	// WarehouseCode 仓短码：前端预览「将要生成的主体 SKU」用（服务端才是真源）。
	WarehouseCode string
	Label         string
	IsDefault     bool
	Items         []WarehouseSKUItemResp
}

// AttributeOptionResp 属性组勾选列表的一项。
type AttributeOptionResp struct {
	ID string
	// Label 名字 + 标识 key：商品详情页与属性页都按 key 指代属性组，
	// 只给名字会在同名属性组之间选错。
	Label       string
	IsVariation bool
}

// EntityTemplateOptionResp 商品详情模板候选的一项（卡片区只展示名字与「默认」徽标）。
type EntityTemplateOptionResp struct {
	ID   string
	Name string
}

// AttributeCheckOptionResp 带勾选态的属性组项（商品编辑页的勾选列表）。
//
// 与 AttributeOptionResp 是**两个形状**而不是给它加一个 Checked 字段：勾选态是
// 「这一屏 + 这一行数据」的结论（新建页没有它），塞进公共项会让新页的视图里多出一个
// 恒为假的字段，读者要在两处之间确认它到底谁在写。
//
// 内嵌而不是复制字段：模板里 `{{a.ID}}` / `{{a.Label}}` / `{{if a.Checked}}` 的写法不变，
// 「标签口径」仍然只有 AttributeOptionResp 一处。Jet 走 reflect.FieldByName，**提升字段
// 照常可读**（已实测；端到端判据是 public/test/product/feature/product_edit_page_test.go
// 的 TestProductEditPageRejectsInvalidNumbersWithoutPartialWrite，它断言渲染结果里出现
// `value="<属性组 id>" checked`）—— 若哪天这条断了，那条用例当场变红。
type AttributeCheckOptionResp struct {
	AttributeOptionResp
	Checked bool
}

// ScoreResp SEO 评分视图（服务端渲染，客户端只处理「点击建议跳转」）。
type ScoreResp struct {
	OK       bool
	Total    int
	Grade    string
	Sections []ScoreSectionResp
	// ProfileType / ProfileReason 本次评分所用的页型与调权理由（审计 SEO-016）。
	// 文档要求调权在结果里回显（docs/02-E1 §5）：不显示的话，编辑者看到同一份内容
	// 在商品页比文章页高几分时无从解释。空 = 用默认权重。
	ProfileType   string
	ProfileReason string
	// Duplicates 与本页标题重复的其它页面（审计 SEO-018 编辑期轻量版）。
	// **列出冲突页面本身**而不是只报数量：只报「有重复」运营不知道该去改哪一页。
	Duplicates    []string
	DuplicateNote string
	// Empty 空态文案（读不到实体时给一句可读的话，而不是让面板整体不可用）。
	Empty     string
	SerpTitle string
	SerpURL   string
	SerpDesc  string
}

// ScoreSectionResp 单个评分维度。
type ScoreSectionResp struct {
	Label      string
	Color      string
	ColorLabel string
	Score      int
	Max        int
	Issues     []ScoreIssueResp
}

// ScoreIssueResp 单条未达标检查（Target 非空表示可点击定位）。
type ScoreIssueResp struct {
	Text   string
	Target string
}

// TranslationLangOptionResp 工作台语言下拉项。
type TranslationLangOptionResp struct {
	Code   string
	Label  string
	Active bool
}

// ProductTranslationResp 工作台一行（一个可翻译取值）。
type ProductTranslationResp struct {
	Context    string
	Field      string
	FieldLabel string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	// EngineTone 译文来源的徽标分档（i18n.ContentEngineTone）：模板据此选 class，
	// 不在模板里写 engine == "ai"（字符串字面量只该出现在常量定义处）。
	EngineTone string
	// Rich 富文本 / 多段文本字段（描述、属性值数组）：译文需与原文同形。
	Rich bool
}

// ProductTranslationGroupResp 按实体分组的行集合。
type ProductTranslationGroupResp struct {
	Key string
	// EntityType / EntityID 该组对应的实体（译文保存后据此精确标记自动发布实例）。
	EntityType string
	EntityID   string
	// Title 该组的**实体名**（商品名 / 分类名…）：它是数据，不取词。
	Title string
	// TitleKey / TitleFallback 分组标题里实体类型名的事实（「商品分类」「商品品牌」…）。
	// 页面用 `{{tr(g.TitleKey, g.TitleFallback)}} · {{g.Title}}` 拼 —— 实体类型是词条、
	// 实体名是数据，两样不能提前拼成一串（拼了就锁死在一种语言）。
	TitleKey      string
	TitleFallback string
	Subtitle      string
	Rows          []ProductTranslationResp
}

// ProjectTranslationOptionResp 工程下拉项。
type ProjectTranslationOptionResp struct {
	ID       string
	Name     string
	Selected bool
}

// ProductCreateFormResp 商品建表单的渲染视图（本模块第五个读模型，与四个整页同一形态）。
//
// 「表单」不是另一种响应形态：后端给数据、Jet 渲染 HTML —— 这份视图撑的就是**用数据渲染出来的
// 那一段 HTML**（首屏整页里用，提交失败重渲染时也用同一份），两者共用同一个装配助手，
// 不另造一套。
//
// 与四个整页视图的两点不同：
//   - 没有工程清单（不渲染工程下拉）：工程来自表单字段，这里只带 id；
//   - 没有回填槽（SubmitErr / FormEcho）：提交已迁到 JSON 写基座（fetch 提交、失败时原页
//     保留输入），服务端不再重渲染表单，「本次提交了什么」也不再需要进视图。
type ProductCreateFormResp struct {
	SelectedProject string

	WarehouseOptions    []WarehouseOptionResp
	AttributeOptions    []AttributeOptionResp
	WarehouseSKUOptions []WarehouseSKUGroupResp

	// LoadFailed 取数失败：渲染方据此退回整页提示（这一段 HTML 撑不起来时不留半个表单）。
	// 这里**没有 LoadError** —— 失败不留在这份视图里，驱动原文只走 err 返回值进日志。
	LoadFailed bool
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductCreateFormResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 取数失败时不该被缓存：重试必须真的回到服务端。
func (v ProductCreateFormResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductPricingPageResp 定价工具页的渲染视图（本模块第六个读模型）。
//
// 这一屏**没有失败档**：三处取数各自降级（工程清单读不到 → 空下拉；规则与尾数处理是
// 注册表快照，本身无 error；留痕台账读不到 → 空台账），页面主体（改价表单）始终撑得起来。
// 但仍实现 view.Result：装配缺陷那一条 500 要靠它传出去
// （PageStatus 返回 0 时渲染出口按 200 处理，所以正常档不必显式设码）。
type ProductPricingPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string

	// Rules / Roundings 参考表；Form 是表单初值（首次 GET）或本次提交的回显值（试算失败）。
	Rules     []*PricingRuleTypeResp
	Roundings []*PricingRoundingOptionResp
	Form      PricingForm

	// HasPreview 为假 = 还没试算过（模板整块跳过）；摘要与明细都在 service 算好，
	// 模板只做展示（也避免模板解指针）。
	HasPreview     bool
	PreviewSummary map[string]any
	PreviewRows    []map[string]any

	// History 调价留痕台账（批次 + 逐变体明细）；Err 是试算失败的成品串。
	History []map[string]any
	Err     string

	// ListQuery 页面上下文（表单 action 的 query，写动作的提示页由 pkg/backpath 读回）：
	// URL 形状属 HTTP，由控制器填。
	ListQuery string

	// HTTPStatus 这一屏的档位：正常恒 200（本页三处取数各自降级，没有取数失败档）。
	// 唯一的非 200 来源是**装配缺陷**（控制器断言不到读模型）——那时给 500，
	// 让「没接上」与「这一屏本来就没数据」在页面上看得出区别。
	HTTPStatus int
}

// ProductAttributesPageResp 属性库整页的渲染视图。
//
// 两类字段分得很清楚：
//   - **取数结果**（AttributeDetails / Total / Projects …）由 service 写；
//   - **装配产物**（Attributes / AttrCreateForm / Pagination / ListQuery / LoadError）
//     由控制器写 —— 行里挂着两个抽屉表单的数据，它们要 csrf 与取词函数（gin.Context 的产物）。
//
// 为什么用 AttributeDetails 而不是让 Attributes 直接装 []*AttributeResp：模板读的
// `.Attributes` 是有 `EditForm` / `ValuesForm` 两键的**成品行**（同 ProductListPageResp 的
// RowDetails → Products 分工）。两个键混用会让「原料」与「成品」在模板里看起来是同一件事。
type ProductAttributesPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string

	// FilterKeyword / FilterVariation 回显，Filtered 是空态分档依据（「筛出来是空的」与
	// 「这个工程本来就没有属性组」必须给不同文案）。
	FilterKeyword   string
	FilterVariation string
	Filtered        bool

	// AttributeDetails 本页每行的属性组（含属性值）：行装配的原料。
	AttributeDetails []*AttributeResp
	// Attributes 成品行（模板 `{{range .Attributes}}` 读 ID / Key / Name / IsVariation /
	// Sort / ValueCount / EditForm / ValuesForm）。`map[string]any` 与 gin.H 同型，
	// dto 因此不必 import gin。
	Attributes []map[string]any
	// AttrCreateForm 「新建属性组」抽屉的表单数据（页头与空态两个入口共用同一份）。
	AttrCreateForm map[string]any

	// Total / Page / Limit 分页**事实**；Pagination 由控制器按当前请求补基地址后注入。
	Total      int64
	Page       int
	Limit      int
	Pagination pagination.Page

	// ListQuery 筛选上下文（表单 action 的 query）：写动作失败时由 pkg/backpath 读回，
	// 回跳列表页不丢关键词 / 变体筛选 / 页码。URL 形状属 HTTP，由控制器填。
	ListQuery string

	// LoadFailed 本次请求**没读到**（不是「读到了、只是空的」）；LoadError 由控制器按
	// 当前语言归口后填。HTTPStatus：200 读到 / 400 参数不合法 / 500 主数据没读到。
	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductAttributesPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
func (v ProductAttributesPageResp) PageNoStore() bool { return v.LoadFailed }

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductPricingPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 装配缺陷页不该被缓存：重试必须真的回到服务端。
func (v ProductPricingPageResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// DetailTemplateProductResp 这一屏展示的商品（只四个字段，不给整份商品）。
type DetailTemplateProductResp struct {
	ID   string
	Name string
	Slug string
	// URLPath 发布路径的**表单初值**（按选中工程的 URL 规则派生，用户可改）。
	// 不是已发布路径 —— 后者在 InstanceURL 里。
	URLPath string
}

// DetailTemplateOptionResp 模板候选的渲染行。
//
// IsBound / IsDefault 由**本屏**算出（实例实际绑的那一套 / 当前解析到的默认那一套），
// 不复用内容模板模块的 IsDefault：那是 (工程, 类型) 作用域的生效标记，
// 与「这个商品的实例绑在谁身上」不是一回事，两者混用会让「已绑定」被显示成「类型默认」。
type DetailTemplateOptionResp struct {
	ID           string
	Name         string
	DraftVersion int64
	UpdatedAt    string
	IsBound      bool
	IsDefault    bool
}

// ProductDetailTemplatePageResp 「详情页模板」整页的渲染视图（本模块第八个读模型）。
//
// Ready 与 LoadFailed 是两件事：Ready = 「能不能给这一屏干活」（缺模板能力时为假，
// 走页内提示而不是失败页），LoadFailed = 「取数没读到」（失败槽）。两者混成一个布尔时
// 「未装配」会被显示成「数据读不出来」，而前者要人去查装配、后者重试就好。
type ProductDetailTemplatePageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string

	// Ready 主数据齐了（含 Product 与 Templates）；为假时控制器走「未装配」或
	// 「缺商品」两个出口，不渲染模板清单。
	Ready bool

	Product           DetailTemplateProductResp
	Templates         []DetailTemplateOptionResp
	TemplateCount     int
	BoundTemplateID   string
	DefaultTemplateID string
	InstanceExists    bool
	InstanceStatus    string
	InstanceURL       string

	// NoProduct 没给 product 参数：控制器据此走引导出口（整页提示回列表）而不是渲染本页。
	// 它不是参数非法 —— 本页本来就有「还没选商品」的入口形态。
	NoProduct bool

	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductDetailTemplatePageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端。
func (v ProductDetailTemplatePageResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// BundleCandidateResp 候选 SKU 下拉项（id + 可读标签）。
type BundleCandidateResp struct {
	ID    string
	Label string
}

// BundleSourceWarehouseGroupResp 来源仓下拉项 + 该仓的仓库 SKU 勾选清单。
type BundleSourceWarehouseGroupResp struct {
	WarehouseID string
	Label       string
	IsDefault   bool
	Selected    bool
	Items       []WarehouseSKUItemResp
}

// ProductBundlePageResp 捆绑配置页的渲染视图（本模块第九个读模型）。
//
// 取数原料与装配产物分开命名：Detail / Skus / Warehouses / WarehouseGroups / Products
// 是读回来的，Rows / CandidateSKUs / Sources / SourceProducts / SourceWarehouses 是装出来的。
// 混在一个字段名下会让人以为模板里看到的就是库里的东西。
type ProductBundlePageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string
	Products        []*ProductResp
	SelectedProduct string
	// Detail 库里的捆绑配置（写失败重渲染时 Config 已被 Submitted 覆盖）。
	Detail *BundleConfigResp
	// Skus 本工程的候选 SKU（成员标签与候选下拉都从它来）。
	Skus []*BundleSKUResp

	// Rows 成员表行；CandidateSKUs 候选下拉；SkuCount 候选条数（模板提示用）。
	Rows          []BundleOptionResp
	CandidateSKUs []BundleCandidateResp
	SkuCount      int
	// MaxOptions 成员数上限（表单约束 + 空行补齐的判据）。
	MaxOptions int

	// 成员来源面板：三种来源 + 各自的候选数据。槽位收事实（Key + 中文兜底 / 数据名），取词在模板。
	Sources        []map[string]any
	Source         string
	SourceProducts []map[string]any
	// SourceAttributes 与 SourceWarehouses 的选择依据分别是「选中来源商品」与「选中来源仓」。
	SourceAttributes  []*AttributeResp
	SourceWarehouses  []BundleSourceWarehouseGroupResp
	SourceWarehouseID string

	// Cfg* 配置数值的回填源（库里的配置；读不出来时是零值 + SaveBlocked）。
	CfgMaxOptions  int
	CfgMinTotalQty int
	CfgMaxTotalQty int

	// SaveBlocked 配置读不出来时禁止保存：无法保证这次提交的是完整配置，
	// 宁可让用户先「重新载入」，也不要缺配置误写。
	SaveBlocked bool

	// ResolveURL / ListQuery 两个 URL 形状（HTTP 边界，控制器给）：成员来源面板的解析接口、
	// 以及页面链接要带回的筛选上下文。名字即模板键。
	ResolveURL string
	ListQuery  string

	// LoadError 由控制器按当前语言归口后填（与前八屏同一分工：文案是 HTTP 侧的取词）。
	LoadFailed bool
	LoadError  string
	// HTTPStatus 这一屏的档位；正常恒 200。
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductBundlePageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端。
func (v ProductBundlePageResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductTranslationsPageResp 商品译文工作台的一屏。
//
// 取数结果（ProjectOptions / Langs / SourceLang / Groups）与保存结论（Saved / Written /
// LangInvalid / Errors）合在一份视图里：保存路径**先重建这一屏再校验**，两者本来就是
// 同一次取数的产物 —— 拆成两份会让「保存失败后重渲的那一页」与「GET 出来的那一页」
// 慢慢长成两个形状。
type ProductTranslationsPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`
	// ProjectID / ProductID / Lang 实际生效的三个上下文（空工程已回退到第一个工程、
	// 空语言已落到第一个目标语言）—— 表单回填与保存校验都用它们，不用请求里那个原始值。
	ProjectID string
	ProductID string
	Lang      string
	Langs     []TranslationLangOptionResp
	// SourceLang 站点源语言（= 站点默认语言）；判定依据见 service 的 translationSourceLang。
	SourceLang string
	// NoTargetLang 没有任何「非源语言」的启用语言：没有可翻译的目标，页面给可行动空态。
	NoTargetLang   bool
	Groups         []ProductTranslationGroupResp
	RowCount       int
	Done           int
	Total          int
	ProjectOptions []ProjectTranslationOptionResp

	// Errors 逐行 / 页面级错误（校验或写入失败时重渲本页）。元素是**事实**
	//（哪一行 + 词条 key + 中文兜底），取词留给页面的 tf。
	Errors []TranslationRowErrorResp
	// LangInvalid 目标语言不在站点启用清单里（保存被拒，页面重渲并列出结论）。
	LangInvalid bool
	// Saved 这次保存已经落库（或确认无变化）：控制器据此走提示页出口。
	Saved bool
	// Written 本次实际写入条数（0 = 没有需要写入的变化）。
	Written int
}

// CategoryParentInfoResp 「父级不在候选列表里」那一项的**事实**：名称是数据，跨工程是布尔。
//
// 拆成两个字段而不是一个拼好的串：跨工程那句说明是文案，必须在渲染期按请求语言取词
// （en-US 后台下也该是英文）—— 在 Go 里拼串等于把它钉死在中文。
type CategoryParentInfoResp struct {
	Name       string
	OutOfScope bool
}

// ProductCategoriesPageResp 分类管理页的一屏。
type ProductCategoriesPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string

	// FilterKeyword 回显；Filtered 是空态分档依据（「筛出来是空的」与「这个工程还没有分类」
	// 必须给不同文案）。
	FilterKeyword string
	Filtered      bool

	// CategoryDetails 本页的分类树（**已按 DFS 扁平**，父在前、子紧随，模板按 Depth 缩进）：
	// 行装配的原料。
	CategoryDetails []*CategoryResp
	// PickFlat 工程内**全部分类**（已扁平）：父级下拉的原料。
	// 不是当前这一页 —— 树状列表之后没有面包屑兜底，缺项会直接表现为
	//「新建子分类时选不到父级」。
	PickFlat []*CategoryResp
	// ParentLabels 「父级不在候选列表里」的那些分类 → 父级事实（见 CategoryParentInfoResp）。
	// 只对本页里真正缺失的那几个父级查一次库。
	ParentLabels map[string]CategoryParentInfoResp

	// MatchTotal 是**命中条数**（提示文案用）；Total（= 命中所属的根分类数）给分页条算总页数。
	// Categories 成品行（模板 `{{range .Categories}}` 读 ID / Name / Slug / Label / ParentID /
	// Sort / Depth / HasChildren / Matched / SearchMode / ParentOutOfScope / EditForm）；
	// CategoryCreateForm 「新建分类」抽屉的表单数据（页头与空态两个入口共用同一份）。
	// 两者都由控制器装配（抽屉要 csrf 与 text.Inject 注入的取词函数）。
	Categories         []CategoryRowResp
	CategoryCreateForm map[string]any
	// Options 父级下拉的选项（本工程分类的扁平列表 + 层级缩进标签）：列表页的筛选区与
	// 抽屉里都读它。**只收本工程**的分类（见 service.CategoriesInProject）。
	// Options 父级下拉的选项（map：它和品牌下拉共用产品行下拉的渲染（productPickOptions），
	// 形状由那套「ID / Label / LabelKey / LabelFallback / Selected」统一决定，不是分类专有）。
	Options []map[string]any

	MatchTotal int64
	Total      int64
	Page       int
	Limit      int
	Pagination pagination.Page

	// ListQuery 筛选上下文（表单 action 的 query）：写动作失败时由 pkg/backpath 读回。
	ListQuery string

	// LoadFailed 本次请求**没读到**（不是「读到了、只是空的」）；LoadError 由控制器按当前
	// 语言归口后填。HTTPStatus：200 读到 / 400 参数不合法 / 500 主数据没读到。
	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductCategoriesPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
// 404 空态同样标 no-store —— 分类可能刚被建回来，缓存的 404 会把运营挡在门外。
func (v ProductCategoriesPageResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductBrandsPageResp 品牌管理页的一屏。
type ProductBrandsPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string
	FilterKeyword   string
	Filtered        bool

	// BrandDetails 本页品牌：行装配的原料（每行的编辑抽屉要 csrf 与取词函数，留控制器）。
	BrandDetails []*BrandResp
	// Brands 成品行（含每行的 EditForm）；BrandCreateForm 内联新建表单的数据。
	Brands          []BrandRowResp
	BrandCreateForm map[string]any

	Total      int64
	Page       int
	Limit      int
	Pagination pagination.Page
	ListQuery  string

	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductBrandsPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 非 200 的档位都不该被缓存：重试必须真的回到服务端。
func (v ProductBrandsPageResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductTagsPageResp 标签管理页的一屏。
type ProductTagsPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string
	FilterKeyword   string
	Filtered        bool

	// TagDetails 本页标签：行装配的原料（行级编辑片段与新建片段留控制器 —— 它们要 csrf
	// 与取词函数）。命中商品**不在首屏取**（审计 PERF-02），由展开区按页拉片段。
	TagDetails []*TagResp
	// RuleTypes 内置规则类型清单：全页只查一次，行级片段 / 页面级新建片段 / 规则说明共享同一份。
	RuleTypes []*TagRuleTypeResp
	// Tags 成品行（含每行的 EditForm）；TagCreateForm 新建抽屉的数据（与行级片段同源）。
	Tags          []TagRowResp
	TagCreateForm map[string]any

	Total      int64
	Page       int
	Limit      int
	Pagination pagination.Page
	ListQuery  string

	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（view.Result）；零值 = 200。
func (v ProductTagsPageResp) PageStatus() int { return v.HTTPStatus }

// PageNoStore 非 200 的档位都不该被缓存：重试必须真的回到服务端。
func (v ProductTagsPageResp) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// BundlePanelResp 商品详情页「捆绑构成」区块的数据（只读表）。
//
// 这是**片段根**的下一层，字段按模板的读法命名（b.Loaded / b.Options / o.SKUCode），
// 所以它是 struct 而不是 map：根 map 只为了 .["t"] 取词，区块自身没有这个诉求。
type BundlePanelResp struct {
	Loaded bool
	// ErrorText 配置读不出来时的可读原因（区块留在页面上并说明，不是整块消失）。
	ErrorText string
	// BasePrice 容器价（套餐金额的唯一来源），读不出来时给 —。
	BasePrice   string
	Options     []BundlePanelRowResp
	OptionCount int
}

// BundlePanelRowResp 构成表的一行：成员 SKU 的规则、可用量与后台价格口径。
//
// 成员挂牌价（ItemPrice）只作**参考**：套餐金额只由容器价得出，成员价不参与
// 任何计价与前台展示（迁移 238 的类型不变量）；成员成本只进后台口径。
type BundlePanelRowResp struct {
	VariantID   string
	SKUCode     string
	ProductName string
	Required    bool
	DefaultQty  int
	MinQty      int
	MaxQty      int
	Available   int
	ItemPrice   string
	CostPrice   string
	Enabled     bool
	// Missing 配置指向的变体已不存在（与「存在但被停用」是两回事：状态文案与
	// 可行动作都不同，故不合并成一个 bool）。
	Missing bool
	// SourceParts 来源快照（仅展示）：词条 Key + 兜底 + 可选数据值，取词与分段
	// 拼接在模板；空 = 手工指定 / 历史配置。
	SourceParts []BundleSourcePart
}

// BrandRowResp 品牌列表的一行（成品行：模板直接读字段）。
//
// 与 BrandResp 的区别同分类那对：BrandResp 是取数结果，这边是列表页要展示的那几列。
// EditForm 与 CategoryRowResp 同一理由留在 map（抽屉模板的数据根要支持 .["t"] 索引）。
type BrandRowResp struct {
	ID, Name, Slug, Logo, Description string
	UpdatedAt                         string
	Sort                              int
	EditForm                          map[string]any
}

// CategoryRowResp 分类列表的一行。
//
// 与 CategoryResp 的区别：那边是**取数结果**（service 读出树的节点），这边是**成品行**
// （含编辑抽屉的表单数据、命中标记、跨工程徽章），装配在控制器、由模板直接读。
//
// EditForm 仍是 map：抽屉模板的数据根必须支持 .["t"] 索引（text.Inject 注入的
// 取词函数），根是 struct 时 Jet 的索引写法渲染失败 —— 行元素可以是 struct，根不行。
type CategoryRowResp struct {
	ID, Name, Slug, Label, ParentID string
	Sort, Depth                     int
	HasChildren, Matched            bool
	SearchMode, ParentOutOfScope    bool
	EditForm                        map[string]any
}
