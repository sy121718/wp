package articlelist

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.articleList"

const SourceArticle = "content:article"

const (
	LayoutGrid = "grid"
	LayoutList = "list"
)

const (
	defaultLimit     = 6
	defaultColumns   = "3"
	defaultTitleTag  = "h3"
	defaultLabel     = "Read more"
	defaultEmptyText = "暂无文章"
	maxLimit         = 60
)

type Props struct {
	CollectionSource string `json:"collectionSource,omitempty" ct:"select,=未选择,content:article=文章列表,default=content:article,sec=collection,label=数据源"`
	CollectionLimit  int    `json:"collectionLimit,omitempty" ct:"slider,min=1,max=60,step=1,sec=collection,label=取几条"`
	OrderBy          string `json:"orderBy,omitempty" ct:"select,=默认,newest=最新创建,oldest=最早创建,default=,sec=collection,label=排序"`

	Layout   string `json:"layout,omitempty" ct:"select,grid=网格,list=列表,default=grid,sec=style,label=布局"`
	Columns  string `json:"columns,omitempty" ct:"select,auto=自适应,2=两列,3=三列,4=四列,default=3,sec=style,label=列数"`
	TitleTag string `json:"titleTag,omitempty" ct:"select,h2=二级标题,h3=三级标题,h4=四级标题,default=h3,sec=content,label=标题层级"`

	ShowImage    string `json:"showImage,omitempty" ct:"select,on=显示,off=隐藏,default=on,sec=content,label=封面图"`
	ShowExcerpt  string `json:"showExcerpt,omitempty" ct:"select,on=显示,off=隐藏,default=on,sec=content,label=摘要"`
	ExcerptLines int    `json:"excerptLines,omitempty" ct:"slider,min=0,max=6,step=1,sec=content,label=摘要行数"`
	Label        string `json:"label,omitempty" ct:"text,maxlen=30,sec=content,label=链接文案"`

	LinkPrefix string `json:"linkPrefix,omitempty" ct:"text,maxlen=80,sec=content,label=链接前缀"`
	EmptyText  string `json:"emptyText,omitempty" ct:"text,maxlen=50,sec=content,label=空态文案"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var articlelistLegacyKeys = []core.LegacyKey{
	{From: "linkText", To: "label"},
}

func (p *Props) NormalizeLegacy(raw json.RawMessage) error {
	return core.ApplyLegacyKeysInto(raw, p, articlelistLegacyKeys)
}

type Component struct{}

func (c *Component) CollectionProp() string { return "collectionSource" }

func (c *Component) Type() string { return Type }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,

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

func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}

	p, derr := core.DecodeProps[Props](node.Props)
	if derr != nil {
		return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, derr)
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}

	if s := strings.TrimSpace(p.CollectionSource); s != "" && s != SourceArticle {
		return fmt.Errorf("节点 %s: 数据源 %q 不支持（当前只认 content:article）", node.ID, s)
	}
	if p.CollectionLimit < 0 || p.CollectionLimit > maxLimit {
		return fmt.Errorf("节点 %s: 取几条 %d 超出 0~%d", node.ID, p.CollectionLimit, maxLimit)
	}
	return nil
}

func effectiveLimit(p *Props) int {
	if p == nil || p.CollectionLimit <= 0 {
		return defaultLimit
	}
	if p.CollectionLimit > maxLimit {
		return maxLimit
	}
	return p.CollectionLimit
}

func effectiveLayout(p *Props) string {
	if p != nil && p.Layout == LayoutList {
		return LayoutList
	}
	return LayoutGrid
}

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

		return "repeat(auto-fill, minmax(min(100%, 16rem), 1fr))"
	}
}

func effectiveTitleTag(p *Props) string {
	if p != nil {
		switch p.TitleTag {
		case "h2", "h3", "h4":
			return p.TitleTag
		}
	}
	return defaultTitleTag
}

func effectiveLines(p *Props) int {
	if p == nil || p.ExcerptLines < 0 {
		return 0
	}
	if p.ExcerptLines > 6 {
		return 6
	}
	return p.ExcerptLines
}

//go:embed articlelist.css
var articlelistCSS string

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

//go:embed article_list.jet
var articlelistTemplate string

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("article_list", articlelistTemplate)
}
