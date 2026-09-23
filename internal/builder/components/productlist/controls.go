// controls.go — 筛选栏 / 工具条 / 分页的控件构造（issue #27）。
//
// 所有控件都是**链接**（<a>）而不是 select/checkbox：
//
//	· 无 JS 时它们是可点的普通链接（整页跳转，URL 语义参数照旧生效）；
//	· 有 HTMX 时同一批链接带 hx-get / hx-push-url，变成局部刷新；
//	· 键盘可达与读屏语义都是链接原生带来的，不需要额外 ARIA 体操。
//
// 每个控件同时给出三个 URL：href（降级整页跳转）、FragmentGet（片段请求）、PushURL（地址栏）。
// 三者的分工见 links.go。
package productlist

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// buildFilterSections 构造筛选栏（只包含作者勾选、且集合源确实给了值的块）。
//
// 每块的 Render 由**数据形状**决定：分类有 ParentID → 树；品牌 / 标签平铺 → 胶囊；
// 属性组是「组 → 值」→ 每组一个平铺块。检查器可覆盖（改 Render 字段即可）。
func buildFilterSections(p *Props, ctx *core.RenderContext, lc linkContext, view *View) []FilterSection {
	if p == nil {
		return nil
	}
	wanted := map[string]bool{}
	for _, name := range splitList(p.Filters) {
		wanted[name] = true
	}
	if len(wanted) == 0 {
		return nil
	}
	options := view.FilterOptions
	sections := make([]FilterSection, 0, 4)

	if wanted["categories"] && len(options.Categories) > 0 {
		view.ShowCategories = true
		// 分类**默认渲染成树**：它的数据自带 ParentID（工作台里作者真的建了层级），
		// 渲染成平铺就是在把树压平 —— 父子长得一模一样，用户分不出谁属于谁。
		selected := selectedIDs(p.FilterCategoryIDs, p.FilterCategoryID)
		childrenOf := childrenIndex(options.Categories)
		// 多选 / 单选：单选不渲染勾选框（纯链接树，对齐源站），点击即替换。
		multi := strings.TrimSpace(p.CategoryMulti) != "off"
		section := FilterSection{Title: "分类", Kind: "categories", Render: renderTree, CaretIcon: p.CaretIcon, Multi: multi}
		for _, item := range options.Categories {
			override := url.Values{}
			// 两套语义共用一条铁律：点父级筛的是**整棵子树**（选了 Alibarbar 却只筛到
			// 直接挂在它名下的商品，等于没表达「整个品牌」）。
			//
			//  · 多选（默认）：在已有选中集合上**切换**该子树（级联加入 / 级联移除）；
			//  · 单选（off）：**替换** —— 点未选中的换成它，点已选中的清空（取消）。
			//    单选下再点第二个分类应当是「换一个」而不是「并集」，否则它就不是单选了。
			if multi {
				override.Set(filterKeyCategoryIDs, strings.Join(cascadeSelection(item.ID, selected, childrenOf), ","))
			} else if selected[item.ID] {
				override.Set(filterKeyCategoryIDs, "")
			} else {
				override.Set(filterKeyCategoryIDs, strings.Join(cascadeSelection(item.ID, map[string]bool{}, childrenOf), ","))
			}
			// 单值键必须跟着清掉：集合源按「多值优先」会忽略它，但地址栏会一直挂着
			// 一个不生效的参数（用户看到「取消勾选后链接里还有个分类」）。
			override.Set(filterKeyCategoryID, "")
			override.Set("page", "") // 换筛选回到第 1 页，否则会落在越界页
			section.Options = append(section.Options, controlOption(lc, item.Name, selected[item.ID], override))
		}
		byID := make(map[string]ControlOption, len(section.Options))
		for i, item := range options.Categories {
			byID[item.ID] = section.Options[i]
		}
		section.Tree = buildFilterTree(options.Categories, selected, childrenOf, byID, p.CaretIcon, multi)
		sections = append(sections, section)
	}

	if wanted["brands"] && len(options.Brands) > 0 {
		view.ShowBrands = true
		selected := selectedIDs(p.FilterBrandIDs, p.FilterBrandID)
		section := FilterSection{Title: "品牌", Kind: "brands", Render: renderButtons}
		for _, item := range options.Brands {
			override := url.Values{}
			override.Set(filterKeyBrandIDs, strings.Join(toggleID(selected, item.ID), ","))
			override.Set(filterKeyBrandID, "")
			override.Set("page", "")
			section.Options = append(section.Options, controlOption(lc, item.Name, selected[item.ID], override))
		}
		sections = append(sections, section)
	}

	if wanted["tags"] && len(options.Tags) > 0 {
		view.ShowTags = true
		selected := selectedIDs(p.FilterTagIDs, p.FilterTagID)
		section := FilterSection{Title: "标签", Kind: "tags", Render: renderButtons}
		for _, item := range options.Tags {
			override := url.Values{}
			override.Set(filterKeyTagIDs, strings.Join(toggleID(selected, item.ID), ","))
			override.Set(filterKeyTagID, "")
			override.Set("page", "")
			section.Options = append(section.Options, controlOption(lc, item.Name, selected[item.ID], override))
		}
		sections = append(sections, section)
	}

	if wanted["attributes"] {
		// 属性多选：组内点一次加入、再点移除（组内 OR），组与组之间 AND。
		current := parseOptionPairs(p.FilterOptions)
		for _, group := range options.Attributes {
			if len(group.Values) == 0 {
				continue
			}
			view.ShowAttributes = true
			selected := map[string]bool{}
			for _, key := range current[group.Key] {
				selected[key] = true
			}
			section := FilterSection{Title: group.Name, Kind: "attributes", Render: renderButtons}
			for _, value := range group.Values {
				override := url.Values{}
				override.Set("option."+group.Key, strings.Join(toggleKey(selected, value.Key), ","))
				override.Set("page", "")
				section.Options = append(section.Options, controlOption(lc, value.Name, selected[value.Key], override))
			}
			sections = append(sections, section)
		}
	}
	return sections
}

// ControlOption 一个可点的交互项。
type ControlOption struct {
	Label string
	// Active 当前选中（渲染成高亮态 + aria-current）。
	Active bool
	// Disabled 不可用（如 popularity 排序：系统里没有热度数据）。
	Disabled bool
	// DisabledHint 不可用原因（title 属性，鼠标悬停可见）。
	DisabledHint string
	// Href 降级链接（只带语义参数；无 JS 时整页跳转）。
	Href string
	// FragmentGet 片段请求 URL（实例配置 + 当前语义参数 + 本次变化）。
	FragmentGet string
	// PushURL 地址栏要推的查询串（只带语义参数）。
	PushURL string
}

// FilterSection 筛选栏的一块（分类 / 品牌 / 标签 / 某个属性组）。
//
// **每块带自己的数据形状与渲染形态**：分类是树（有 ParentID），品牌 / 标签是平铺集合，
// 属性组是「组 → 值」的两层。早先这里只有一个平铺的 Options，构造时把 ParentID 丢掉，
// 于是树被压平成一个列表 —— 渲染出来父子完全一样，谁是谁分不出来。
//
// 形态与数据分开而不是「形态决定数据结构」：同一份数据（比如分类树）可以用折叠树渲染，
// 也可以用下拉渲染；调用方（模板 / 检查器）只换 Render，不必重建数据。
type FilterSection struct {
	// Title 块标题（分类 / 品牌 / 标签 / 属性组名）。
	Title string
	// Kind 块的种类（categories / brands / tags / attributes）—— 模板据此加类名。
	Kind string
	// Render 渲染形态（tree / buttons / checkbox / select / multi-select）。由每块的
	// **数据形状**决定默认值（见 buildFilterSections），检查器可覆盖。
	Render string
	// Options 平铺选项（Render 为 buttons / checkbox / select / multi-select 时使用）。
	Options []ControlOption
	// Tree 树形视图（Render 为 tree 时使用；深度优先展开成扁平条目）。
	//
	// Options 与 Tree 是同一批数据的两种**视图**：分类同时给出两者，
	// 于是「树」与「平铺」之间的切换只是换一个字段读，不需要重新查数据。
	Tree []TreeItem
	// CaretIcon 折叠图标变体（空 = 默认箭头；dot = 圆点；none = 无）。
	CaretIcon string
	// Multi 该块是否多选（分类树：false 时单选、不渲染勾选框）。
	Multi bool
}

// TreeItem 树条目：结构标签由 Go 侧拼好（OpenTags / MidTags / CloseTags），
// 模板只画中间的 <a>。
//
// 关键是**闭合的延迟**：父项的 <ul> 必须包住子项、在最后一个后代之后才闭合 ——
// 而扁平输出里父项只有一个输出点。解法是把闭合数量按「相邻两项的深度差」
// 摊到后一个项的 CloseTags 里（depth 下降 1 层 = 关 1 组 </ul></details></li>），
// 末项关 depth+1 组。于是任意深度的合法树都能配平，模板不需要递归。
type TreeItem struct {
	// Option 该节点的可点项（链接已按级联算好）。
	Option ControlOption
	// ID 实体 id。
	ID string
	// Depth 层级深度（0 = 根）。
	Depth int
	// State 三态：stateAll / statePartial / stateNone。
	State string
	// OpenTags 锚之前的开标签：<li（有子级时再加 <details><summary/>）。
	OpenTags string
	// MidTags 锚之后的标签：有子级时是 <ul>（子项由后续条目输出）。
	MidTags string
	// CloseTags 关标签：自身 </li> + 按「与下一项的深度差」补足的父层闭合组。
	CloseTags string
}

// 三态取值（TreeItem.State）。
const (
	// stateAll 该节点及其全部后代都选中。
	stateAll = "all"
	// statePartial 子树里有一部分选中（含「自己选中但子树没全选」）。
	statePartial = "partial"
	// stateNone 子树里一个都没选。
	stateNone = "none"
)

func buildFilterTree(options []core.CollectionFilterChoice, selected map[string]bool, childrenOf map[string][]string, opts map[string]ControlOption, caretIcon string, multi bool) []TreeItem {
	if len(options) == 0 {
		return nil
	}
	byID := make(map[string]core.CollectionFilterChoice, len(options))
	for _, opt := range options {
		if opt.ID != "" {
			byID[opt.ID] = opt
		}
	}
	children := map[string][]core.CollectionFilterChoice{}
	roots := make([]core.CollectionFilterChoice, 0, len(options))
	for _, opt := range options {
		// 孤儿（父不在结果里）当根：挂在任何地方都不对，留在原地至少看得见。
		_, parentKnown := byID[opt.ParentID]
		if opt.ParentID == "" || opt.ID == opt.ParentID || !parentKnown {
			roots = append(roots, opt)
			continue
		}
		children[opt.ParentID] = append(children[opt.ParentID], opt)
	}
	out := make([]TreeItem, 0, len(options))
	// walk **前序**：先追加自己，再递归子级。曾经写成后序，输出变成「子在前父在后」。
	//
	// 收尾配平：父项的 </ul></details></li> 必须等最后一个后代输出完才出现，
	// 而扁平输出里父项只有一个输出点 —— 解法是把闭合组摊到「后一项」头上：
	// 相邻两项深度下降 1 层 = 1 组；末项关 depth+1 组。
	var walk func(opt core.CollectionFilterChoice, depth int, seen map[string]bool) (allSel, anySel bool)
	walk = func(opt core.CollectionFilterChoice, depth int, seen map[string]bool) (allSel, anySel bool) {
		idx := len(out)
		out = append(out, TreeItem{Option: opts[opt.ID], ID: opt.ID, Depth: depth})
		kids, hasKids := children[opt.ID]
		// 深度上限：到顶的子树按叶子渲染（不展开 details），闭合才不会缺组。
		if depth >= maxFilterTreeDepth {
			hasKids = false
		}
		childAll, childAny := true, false
		if hasKids {
			next := make(map[string]bool, len(seen)+1)
			for k := range seen {
				next[k] = true
			}
			next[opt.ID] = true
			for _, child := range kids {
				if next[child.ID] {
					continue // 环：已经在祖先链上出现过，不再下去
				}
				a, y := walk(child, depth+1, next)
				if !a {
					childAll = false
				}
				if y {
					childAny = true
				}
			}
		}
		// 三态：全选 = 自己选中且（无子树或整棵子树都选中）；
		// 半选 = 没有全选、但自己或子树里有选中的。
		state := stateNone
		if !multi {
			// 单选：只有「选中 / 未选中」两态。子树推导与半选都属于多选语义 ——
			// 单选下父级显示半选没有意义（用户只能选一个），而且会让样式多一套分支。
			if selected[opt.ID] {
				state = stateAll
			}
		} else if selected[opt.ID] && childAll {
			state = stateAll
			allSel, anySel = true, true
		} else if selected[opt.ID] || childAny {
			state = statePartial
			anySel = true
		}
		item := &out[idx]
		item.State = state
		if hasKids {
			openAttr := ""
			if state != stateNone {
				openAttr = " open"
			}
			item.OpenTags = fmt.Sprintf(
				"<li class=\"sky-product-list-node\" data-depth=\"%d\" data-state=\"%s\">"+
					"<details class=\"sky-product-list-node-group\"%s>"+
					"<summary class=\"sky-product-list-node-caret-row\">",
				depth, state, openAttr)
			// 锚由模板输出在 summary 内（始终可见），随后闭合 summary、打开子级 <ul>。
			item.MidTags = "</summary><ul class=\"sky-product-list-node-children\">"
			// 自身 </li> 由最后一个后代的 CloseTags 补（见下），父项这里不关。
			item.CloseTags = ""
			// 折叠图标：**走基座图标库**（core.IconSVGClass，lucide 1868 枚），
			// 不再 CSS 手绘。空选 = 默认 chevron-right；dot / square / none 可选。
			iconName := "chevron-right"
			if caretIcon != "" {
				iconName = caretIcon
			}
			if iconName != "none" {
				if svg, ok := core.IconSVGClass(iconName, "sky-product-list-node-caret"); ok {
					item.OpenTags += svg
				}
			}
		} else {
			item.OpenTags = fmt.Sprintf("<li class=\"sky-product-list-node\" data-depth=\"%d\" data-state=\"%s\">", depth, state)
			item.CloseTags = "</li>"
		}
		return allSel, anySel
	}
	for _, root := range roots {
		walk(root, 0, map[string]bool{})
	}
	// 收尾配平：close 组数 = 相邻深度差（depth 下降几层就关几组），末项关 depth+1 组。
	for i := range out {
		var drop int
		if i+1 < len(out) {
			if drop = out[i].Depth - out[i+1].Depth; drop < 0 {
				drop = 0
			}
		} else {
			drop = out[i].Depth + 1
		}
		if drop > 0 {
			out[i].CloseTags += strings.Repeat("</ul></details></li>", drop)
		}
	}
	return out
}

// 渲染形态取值（FilterSection.Render）。
const (
	// renderTree 折叠树：父子嵌套 + 缩进 + 可折叠。分类的默认形态。
	renderTree = "tree"
	// renderButtons 平铺胶囊：值不多、想一眼看全时用（品牌 / 标签 / 属性值）。
	renderButtons = "buttons"
	// renderCheckbox / renderSelect / renderMultiSelect 数据侧已支持，模板逐步接入。
	renderCheckbox    = "checkbox"
	renderSelect      = "select"
	renderMultiSelect = "multi-select"
)

// maxFilterTreeDepth 筛选树的最大深度（坏数据兜底，正常分类树 2~3 层）。
const maxFilterTreeDepth = 5

// cascadeSelection 把「用户点了一个节点」展开成最终的选中集合（树形级联）。
//
// 1. 勾父级 ⇒ 子级全勾（递归）；2. 取消父级 ⇒ 子级全不勾；
// 3. 勾子级 ⇒ 只勾子级，父级态由子级推导（渲染期算三态），不在这里写。
func cascadeSelection(clicked string, selected map[string]bool, childrenOf map[string][]string) []string {
	next := make(map[string]bool, len(selected)+4)
	for id := range selected {
		next[id] = true
	}
	if next[clicked] {
		delete(next, clicked)
		for _, id := range collectSubtree(clicked, childrenOf) {
			delete(next, id)
		}
	} else {
		next[clicked] = true
		for _, id := range collectSubtree(clicked, childrenOf) {
			next[id] = true
		}
	}
	out := make([]string, 0, len(next))
	for id := range next {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// collectSubtree 深度优先收集某节点的全部后代 id（不含自身；环安全）。
func collectSubtree(root string, childrenOf map[string][]string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{root: true}
	var walk func(id string)
	walk = func(id string) {
		for _, child := range childrenOf[id] {
			if seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			walk(child)
		}
	}
	walk(root)
	return out
}

// childrenIndex 由平铺选项建「父 id → 子 id 列表」索引（级联与树共用一份）。
func childrenIndex(options []core.CollectionFilterChoice) map[string][]string {
	known := make(map[string]bool, len(options))
	for _, opt := range options {
		known[opt.ID] = true
	}
	out := make(map[string][]string, len(options))
	for _, opt := range options {
		// 孤儿与自引用不进索引（与 buildFilterTree 同一套判据，两处不能分叉）。
		if opt.ParentID == "" || opt.ID == opt.ParentID || !known[opt.ParentID] {
			continue
		}
		out[opt.ParentID] = append(out[opt.ParentID], opt.ID)
	}
	return out
}

// subtreeFullySelected 该节点的整棵子树是否全部选中（父级三态用）。
func subtreeFullySelected(id string, selected map[string]bool, childrenOf map[string][]string) bool {
	sub := collectSubtree(id, childrenOf)
	if len(sub) == 0 {
		return selected[id]
	}
	for _, child := range sub {
		if !selected[child] {
			return false
		}
	}
	return true
}

// subtreeAnySelected 该节点的子树里是否有任意一个选中（父级半选判定用）。
func subtreeAnySelected(id string, selected map[string]bool, childrenOf map[string][]string) bool {
	for _, child := range collectSubtree(id, childrenOf) {
		if selected[child] {
			return true
		}
	}
	return false
}

// selectedIDs 当前选中的 id 集合：多值优先，多值为空时退回单值。
//
// 这条规则与集合源解析期（parseCollectionFilter 的「多值优先于单值」）**必须一致**：
// 不一致的表现是高亮态与实际结果对不上（显示勾了 A、返回的却是 B 的商品）。
func selectedIDs(multi, single string) map[string]bool {
	out := map[string]bool{}
	for _, id := range splitList(multi) {
		out[id] = true
	}
	if len(out) == 0 {
		if id := strings.TrimSpace(single); id != "" {
			out[id] = true
		}
	}
	return out
}

// toggleID 在选中集合里切换一个 id，返回排序后的新列表（URL 稳定 ⇒ 缓存友好）。
func toggleID(selected map[string]bool, id string) []string {
	next := make([]string, 0, len(selected)+1)
	for cur := range selected {
		if cur != id {
			next = append(next, cur)
		}
	}
	if !selected[id] {
		next = append(next, id)
	}
	sort.Strings(next)
	return next
}

// toggleKey 同 toggleID，但作用于属性值 key（键类型是 string，语义完全相同）。
//
// 不把 toggleID 泛型化：项目里对 Go 泛型的用法有既定口径（必要才引入），
// 这里两个函数的实体只有三行，比给一个只在两处用的私有 helper 加类型参数更直白。
func toggleKey(selected map[string]bool, key string) []string {
	return toggleID(selected, key)
}

// buildSortOptions 排序下拉的选项（价格排序在 #28；热度是禁用占位）。
func buildSortOptions(p *Props, lc linkContext, view *View) []ControlOption {
	current := effectiveOrder(p)
	type item struct{ key, label string }
	items := []item{
		{OrderDefault, "默认排序"},
		{OrderNewest, "最新上架"},
		{OrderOldest, "最早上架"},
		// 评分（issue #29）：按评分降序，无评分的排最后。
		{OrderRatingDesc, "评分最高"},
		// 价格两条（issue #28）：按最低启用变体价升 / 降。
		{OrderPriceAsc, "价格从低到高"},
		{OrderPriceDesc, "价格从高到低"},
	}
	out := make([]ControlOption, 0, len(items)+1)
	for _, it := range items {
		override := url.Values{}
		if it.key == OrderDefault {
			override.Set("orderBy", "")
		} else {
			override.Set("orderBy", it.key)
		}
		override.Set("page", "")
		out = append(out, controlOption(lc, it.label, current == it.key, override))
	}
	// 热度：系统里没有销量 / 埋点数据，做成禁用占位而不是假装能排。
	out = append(out, ControlOption{
		Label: "按热度", Active: false, Disabled: true,
		DisabledHint: "需要销量或浏览数据（订单域落地后接入）",
	})
	view.ShowSort = true
	return out
}

// buildPageSizeOptions 每页条数选项。
func buildPageSizeOptions(p *Props, lc linkContext, view *View) []ControlOption {
	current := EffectivePageSize(p)
	sizes := []int{20, 40, 60}
	out := make([]ControlOption, 0, len(sizes))
	for _, size := range sizes {
		override := url.Values{}
		override.Set("pageSize", strconv.Itoa(size))
		override.Set("page", "")
		out = append(out, controlOption(lc, strconv.Itoa(size)+" 条", current == size, override))
	}
	view.ShowPageSize = true
	return out
}

// buildColumnOptions 网格密度选项（列表 / 2 / 3 / 4 列）。
func buildColumnOptions(p *Props, lc linkContext, view *View) []ControlOption {
	current := effectiveColumns(p)
	type item struct{ key, label string }
	items := []item{
		{LayoutList, "列表"}, {"2", "2 列"}, {"3", "3 列"}, {"4", "4 列"},
	}
	out := make([]ControlOption, 0, len(items))
	for _, it := range items {
		override := url.Values{}
		if it.key == LayoutList {
			override.Set("layout", LayoutList)
		} else {
			override.Set("layout", LayoutGrid)
			override.Set("columns", it.key)
		}
		out = append(out, controlOption(lc, it.label, current == it.key, override))
	}
	view.ShowColumns = true
	return out
}

// buildOnSaleOptions 「只看在售」开关（对应 #11 的 on_sale 自动标签判定：
// 存在启用变体且有划线价高于售价）。
//
// 做一个可点开关而不是复选框：复选框需要 JS 维护状态与提交，链接天然可点、可分享、
// 无 JS 时也照常工作（点击后 URL 带 onSale=true，整页刷新同样是筛过的结果）。
func buildOnSaleOptions(p *Props, lc linkContext, view *View) []ControlOption {
	active := strings.TrimSpace(p.OnlyOnSale) == "on"
	override := url.Values{}
	if active {
		override.Set("onSale", "")
	} else {
		override.Set("onSale", "true")
	}
	override.Set("page", "")
	view.ShowOnSale = true
	return []ControlOption{controlOption(lc, "只看在售", active, override)}
}

// buildPriceSection 价格块（issue #28）：预设档位 + 可拖动滑块。
//
// 显示规则（写死在这里，避免「配了档位却看不到块」这种猜谜）：
//
//	· PriceRanges 非空 → 显示价格块（含档位）；
//	· PriceSlider = on → 即使没有档位也显示（只想要滑块的情况）；
//	· PriceSlider = off → 不显示滑块（档位仍按上一条显示）。
func buildPriceSection(p *Props, lc linkContext, view *View) {
	ranges := splitList(p.PriceRanges)
	slider := priceSliderVisible(p, ranges)
	currentMin, currentMax := currentPriceFilter(p)

	options := make([]ControlOption, 0, len(ranges))
	for _, raw := range ranges {
		rng, label, err := ParsePriceRange(raw)
		if err != nil {
			continue // 形状在 validateExtra 已拦；这里静默跳过，不制造半个档位
		}
		active := priceRangeActive(rng, currentMin, currentMax)
		override := url.Values{}
		if active {
			// 再点一次 = 取消该档（与其它筛选同一交互约定）。
			override.Set("minPrice", "")
			override.Set("maxPrice", "")
		} else {
			override.Set("minPrice", trimNumber(rng.Min))
			if rng.Max != nil {
				override.Set("maxPrice", trimNumber(*rng.Max))
			} else {
				override.Set("maxPrice", "")
			}
		}
		override.Set("page", "") // 换价格回到第 1 页
		options = append(options, controlOption(lc, label, active, override))
	}
	if len(options) == 0 && !slider {
		return
	}
	view.HasPriceSection = true
	view.PriceOptions = options
	if !slider {
		return
	}

	boundMin, boundMax, err := EffectivePriceBounds(p)
	if err != nil {
		boundMin, boundMax = 0, 1000 // 同上：形状已在配置期拦下
	}
	view.ShowPriceSlider = true
	view.PriceBoundMin = trimNumber(boundMin)
	view.PriceBoundMax = trimNumber(boundMax)
	// 当前区间的两个端点：没设的端点回落到「滑块边界」，这样输入框里显示的总是个确切值
	// （空 value 的 number 输入框在浏览器里会显示成空，用户不知道范围是多少）。
	view.PriceFromValue = trimNumber(boundMin)
	view.PriceToValue = trimNumber(boundMax)
	if currentMin != nil {
		view.PriceFromValue = trimNumber(*currentMin)
	}
	if currentMax != nil {
		view.PriceToValue = trimNumber(*currentMax)
	}
	// 表单两条路：有 JS 走片段局部刷新（hx-get 带实例配置），没 JS 走原生 GET 到干净 URL
	// （action 只带语义参数，表单字段把 minPrice / maxPrice 附上去，冷启动补正接住结果）。
	view.PriceFragmentGet = lc.fragmentGet(url.Values{})
	view.PriceFormAction = lc.pushURL(url.Values{})
}

// priceSliderVisible 滑块是否渲染。
func priceSliderVisible(p *Props, ranges []string) bool {
	if !PriceSliderEnabled(p) {
		return false
	}
	if strings.TrimSpace(p.PriceSlider) == "on" {
		return true
	}
	return len(ranges) > 0
}

// currentPriceFilter 当前生效的价格区间（来自 props；非法值按「未设」处理）。
func currentPriceFilter(p *Props) (min, max *float64) {
	if p == nil {
		return nil, nil
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(p.FilterMinPrice), 64); err == nil && strings.TrimSpace(p.FilterMinPrice) != "" {
		min = &v
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(p.FilterMaxPrice), 64); err == nil && strings.TrimSpace(p.FilterMaxPrice) != "" {
		max = &v
	}
	return min, max
}

// priceRangeActive 当前区间是否正好等于这个档位（用于高亮）。
func priceRangeActive(rng PriceRange, min, max *float64) bool {
	if min == nil || *min != rng.Min {
		return false
	}
	if rng.Max == nil {
		return max == nil
	}
	return max != nil && *max == *rng.Max
}

// trimNumber 数字 → 最简字符串（100 而不是 100.000000；100.5 保留小数）。
func trimNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// buildRatingSection 评分块（issue #29）：每档一条「≥ N 星」的筛选链接。',
//
// 与其它筛选同构：无 JS 可点、推送干净的语义 URL、再点一次取消、参与高亮。
func buildRatingSection(p *Props, lc linkContext, view *View) {
	scores := splitList(p.RatingOptions)
	if len(scores) == 0 {
		return
	}
	current := strings.TrimSpace(p.FilterMinRating)
	options := make([]ControlOption, 0, len(scores))
	for _, raw := range scores {
		score := strings.TrimSpace(raw)
		active := current != "" && current == score
		override := url.Values{}
		if active {
			override.Set("minRating", "")
		} else {
			override.Set("minRating", score)
		}
		override.Set("page", "")
		options = append(options, controlOption(lc, "≥ "+score+" 星", active, override))
	}
	view.HasRatingSection = true
	view.RatingOptions = options
}

// controlOption 拼一个控件的三个 URL。
func controlOption(lc linkContext, label string, active bool, override url.Values) ControlOption {
	return ControlOption{
		Label:  label,
		Active: active,
		// Href 是**降级链接**（无 JS 时整页跳转）→ 绝对地址；
		// PushURL 是地址栏要变成的查询串（htmx 按当前路径推入历史）→ 保持相对。
		// 两者此前同值，于是降级链接只能是 "?page=2"（查询相对引用），
		// 不满足「站内地址一律绝对」的约定。
		Href:        lc.fallbackHref(override),
		FragmentGet: lc.fragmentGet(override),
		PushURL:     lc.pushURL(override),
	}
}

// pageOverride 翻页的语义参数覆盖（只改 page，其余筛选原样保留）。
func pageOverride(page int) url.Values {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	return v
}

// pageItems 生成窗口化页码：当前页两侧各 pageWindowSpan 个，首末两页恒在，
// 中间断开处给省略号。
//
// 为什么窗口化：一个 200 页的分类铺 200 个按钮既撑破版心、也没人这么用；
// 而「首末恒在」是为了让「跳到最后一页」永远只要一次点击 —— 那是长列表里
// 唯一真正常用的跳转。
//
// 返回两个等长切片：items 是页码按钮，gaps[i] 表示 items[i] 之前该渲染省略号。
func pageItems(lc linkContext, page, totalPages int) ([]ControlOption, []bool) {
	if totalPages <= 1 {
		return nil, nil
	}
	// 页码集合：1、末页，加上当前页 ± span。
	set := map[int]bool{1: true, totalPages: true}
	for p := page - pageWindowSpan; p <= page+pageWindowSpan; p++ {
		if p >= 1 && p <= totalPages {
			set[p] = true
		}
	}
	pages := make([]int, 0, len(set))
	for p := range set {
		pages = append(pages, p)
	}
	sort.Ints(pages)
	items := make([]ControlOption, 0, len(pages))
	gaps := make([]bool, 0, len(pages))
	prev := 0
	for _, p := range pages {
		// 相邻页码跳号 → 中间省略。只差一页时不省略：
		// 一个省略号省掉一个数字，反而更难读。
		gaps = append(gaps, prev != 0 && p-prev > 1)
		items = append(items, controlOption(lc, strconv.Itoa(p), p == page, pageOverride(p)))
		prev = p
	}
	return items, gaps
}

// pageWindowSpan 页码窗口半径（当前页两侧各显示几个）。
const pageWindowSpan = 2

// parseOptionPairs `key:v1,v2` → 每个属性组当前选中的值列表。
//
// 形状与集合源的 `option.<key>=v1,v2` 一致（冒号前是属性组、冒号后是逗号多值），
// 一个 key 出现在多个 pair 里就**并集**（而不是后者覆盖前者）：重复键在
// 手工拼的 URL 里是合法的，覆盖会让先写的那些值静默消失。
func parseOptionPairs(raw string) map[string][]string {
	out := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, pair := range strings.Split(raw, ",") {
		key, values, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if seen[key] == nil {
			seen[key] = map[string]bool{}
		}
		for _, value := range strings.Split(values, ",") {
			value = strings.TrimSpace(value)
			if value == "" || seen[key][value] {
				continue
			}
			seen[key][value] = true
			out[key] = append(out[key], value)
		}
	}
	return out
}
