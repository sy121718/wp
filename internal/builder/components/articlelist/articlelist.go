// Package articlelist 实现 core.articleList 文章列表组件。
//
// 为什么要有它：go_wp 一直**没有文章列表组件** —— content:article 集合只有
// core.cardstack 支持，而 cardstack 是「卡片堆叠 / 悬停扇形展开」的展示件，
// 不是列表：它把 N 张卡叠在同一个 grid cell 里，页面上只看得见最上面一张。
// 拿它当博客列表用的直接后果是「文章一条都看不见、也点不进去」。
//
// 与 core.productList 的分工：那个吃 content:product（价格、划线价、规格），
// 这个吃 content:article（封面、标题、摘要）。两者共用同一套「集合源 + 排序 +
// 截断 + 卡片字段」的形状，但字段口径不同，不硬塞进一个组件里。
//
// 链接怎么来：文章集合项没有 url 字段（只有 id/slug/revision + 白名单字段），
// 所以链接 = LinkPrefix（作者填的站内逻辑路径，默认 "/"）+ slug，并经构建期的
// 站内链接本地化器处理（多语言前缀在那一层加）。
package articlelist

import (
	_ "embed" // articlelist.css / article_list.jet 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.articleList"

// SourceArticle 集合源（当前只认这一种）。
const SourceArticle = "content:article"

// 布局。
const (
	LayoutGrid = "grid"
	LayoutList = "list"
)

// 缺省值。
const (
	defaultLimit     = 6
	defaultColumns   = "3"
	defaultTitleTag  = "h3"
	defaultLinkText  = "Read more"
	defaultEmptyText = "暂无文章"
	maxLimit         = 60
)

// Props 文章列表属性。
type Props struct {
	// —— 取数 ——
	CollectionSource string `json:"collectionSource,omitempty" ct:"select,=未选择,content:article=文章列表,default=content:article,sec=collection,label=数据源"`
	CollectionLimit  int    `json:"collectionLimit,omitempty" ct:"slider,min=1,max=60,step=1,sec=collection,label=取几条"`
	OrderBy          string `json:"orderBy,omitempty" ct:"select,=默认,newest=最新创建,oldest=最早创建,default=,sec=collection,label=排序"`

	// —— 布局 ——
	Layout   string `json:"layout,omitempty" ct:"select,grid=网格,list=列表,default=grid,sec=style,label=布局"`
	Columns  string `json:"columns,omitempty" ct:"select,auto=自适应,2=两列,3=三列,4=四列,default=3,sec=style,label=列数"`
	TitleTag string `json:"titleTag,omitempty" ct:"select,h2=二级标题,h3=三级标题,h4=四级标题,default=h3,sec=content,label=标题层级"`

	// —— 卡片内容 ——
	ShowImage    string `json:"showImage,omitempty" ct:"select,on=显示,off=隐藏,default=on,sec=content,label=封面图"`
	ShowExcerpt  string `json:"showExcerpt,omitempty" ct:"select,on=显示,off=隐藏,default=on,sec=content,label=摘要"`
	ExcerptLines int    `json:"excerptLines,omitempty" ct:"slider,min=0,max=6,step=1,sec=content,label=摘要行数"`
	LinkText     string `json:"linkText,omitempty" ct:"text,maxlen=30,sec=content,label=链接文案"`
	// LinkPrefix 链接前缀（站内逻辑路径，与 slug 拼接成 href）。
	// "/" 表示文章挂在站点根下（与本仓库默认的 article 路径模式 "/{slug}" 一致）。
	LinkPrefix string `json:"linkPrefix,omitempty" ct:"text,maxlen=80,sec=content,label=链接前缀"`
	EmptyText  string `json:"emptyText,omitempty" ct:"text,maxlen=50,sec=content,label=空态文案"`

	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Component 文章列表组件（手工组件：集合语义 + 自定义视图装配）。
type Component struct{}

// CollectionProp 实现 core.CollectionProvider：集合源取自 Props.CollectionSource。
// 手写组件必须显式声明，否则依赖登记看不见它 —— 表现是「新增文章后列表页永不重建」。
func (c *Component) CollectionProp() string { return "collectionSource" }

// Type 组件类型。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema。
func (c *Component) PropsSpec() any { return &Props{} }

// Palette 实现 core.PaletteProvider：组件库呈现元数据。
func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		// 与 core.productList 同一口径：**不给 collectionSource**。
		// 刚拖出来的组件还没挑数据源，此时渲染空态而不是报错 ——
		// 组件库的「插入即可编译」契约要求默认 props 就能编出一份产物
		// （defaults_contract_test.go 的 TestPaletteInsertNodesCompile 钉住这一点）。
		// 数据源的下拉默认值由控件声明里的 default=content:article 提供，
		// 那是检查器行为，与「插入后的初始 AST」是两件事。
		DefaultProps: map[string]any{
			"collectionLimit": defaultLimit,
			"layout":          LayoutGrid,
			"columns":         defaultColumns,
			"titleTag":        defaultTitleTag,
			"showImage":       "on",
			"showExcerpt":     "on",
		},
		DisplayName: "文章列表",
		Hint:        "按集合源自动出文章卡片",
	}
}

// Validate 校验节点（含 props 解码与控件约束）。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
	// 数据源留空是合法中间态（刚拖出来还没挑源）：渲染空态，不查库、不报错。
	if s := strings.TrimSpace(p.CollectionSource); s != "" && s != SourceArticle {
		return fmt.Errorf("节点 %s: 数据源 %q 不支持（当前只认 content:article）", node.ID, s)
	}
	if p.CollectionLimit < 0 || p.CollectionLimit > maxLimit {
		return fmt.Errorf("节点 %s: 取几条 %d 超出 0~%d", node.ID, p.CollectionLimit, maxLimit)
	}
	return nil
}

// effectiveLimit 取几条（缺省 6，封顶 60）。
func effectiveLimit(p *Props) int {
	if p == nil || p.CollectionLimit <= 0 {
		return defaultLimit
	}
	if p.CollectionLimit > maxLimit {
		return maxLimit
	}
	return p.CollectionLimit
}

// effectiveLayout 布局归一。
func effectiveLayout(p *Props) string {
	if p != nil && p.Layout == LayoutList {
		return LayoutList
	}
	return LayoutGrid
}

// effectiveColumns 列数归一（列表布局恒单列）。
func effectiveColumns(p *Props) string {
	if effectiveLayout(p) == LayoutList {
		return "1"
	}
	if p == nil {
		return defaultColumns
	}
	switch p.Columns {
	case "2", "3", "4":
		return p.Columns
	default:
		return "auto"
	}
}

// gridTemplateColumns 网格列模板（窄视口由 CSS 覆盖为单列）。
func gridTemplateColumns(layout, cols string) string {
	if layout == LayoutList {
		return "1fr"
	}
	switch cols {
	case "2":
		return "repeat(2, minmax(0, 1fr))"
	case "3":
		return "repeat(3, minmax(0, 1fr))"
	case "4":
		return "repeat(4, minmax(0, 1fr))"
	default:
		// 自适应：单列最小宽度用 min(100%, …) 封顶，大卡片 + 窄屏不会横向溢出。
		return "repeat(auto-fill, minmax(min(100%, 16rem), 1fr))"
	}
}

// effectiveTitleTag 标题标签（白名单内，越界回落 h3）。
func effectiveTitleTag(p *Props) string {
	if p != nil {
		switch p.TitleTag {
		case "h2", "h3", "h4":
			return p.TitleTag
		}
	}
	return defaultTitleTag
}

// effectiveLines 摘要行数（0 = 不截断）。
func effectiveLines(p *Props) int {
	if p == nil || p.ExcerptLines < 0 {
		return 0
	}
	if p.ExcerptLines > 6 {
		return 6
	}
	return p.ExcerptLines
}

// articlelistCSS 组件样式源。
//
//go:embed articlelist.css
var articlelistCSS string

// CompileCSS 文章列表样式（静态规则；列数由模板内联样式给出）。
//
// 导出：与 productlist 同形，由 builder 的视图装配层调用（组件包不 import builder）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"isList": core.BoolVar(effectiveLayout(p) == LayoutList),
		"lines":  strconv.Itoa(effectiveLines(p)),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, articlelistCSS, vars); err != nil {
		panic(fmt.Sprintf("articlelist 组件样式解析失败: %v", err))
	}
}

// articlelistTemplate 组件模板（与 .go / .css 同目录）。
//
//go:embed article_list.jet
var articlelistTemplate string

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("article_list", articlelistTemplate)
}
