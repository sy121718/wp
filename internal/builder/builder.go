// Package builder 实现 go_wp 页面文档（Page Document）到静态 HTML/CSS 的编译。
//
// 对应规范 docs/02-A《页面设置与容器规范》：
//   - PageSettings 为页面级全局环境配置，编译期直接作用于 <head> 与 <body>；
//   - 组件树节点按 type 分发到已注册组件（core/Registry），一个组件一个目录
//     （components/container、后续的 components/heading 等）；
//   - 所有响应式断点、布局、外观及动画参数均编译为纯净 CSS，浏览器端零 JavaScript 布局计算；
//   - 编译是确定性的：同一 Page Document 产生完全相同的输出字节。
package builder

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"

	// 内置组件注册（core.container 为组件树唯一结构载体，包 init 自注册）。
	_ "go_wp/internal/builder/components/container"
	// core.heading：标题组件（CMS 绑定发布期静态填入）。
	_ "go_wp/internal/builder/components/heading"
	// core.image：媒体引用组件（构建期经解析器注入变体）。
	_ "go_wp/internal/builder/components/image"
	"go_wp/internal/builder/core"
)

// Page 页面文档：页面级设置 + 顶级容器（Section）列表。
type Page struct {
	Settings PageSettings `json:"settings"`
	Root     []*core.Node `json:"root"`
}

// UnmarshalJSON 反序列化页面文档。
func (p *Page) UnmarshalJSON(data []byte) (err error) {
	type pageAlias Page // 避免递归
	var a pageAlias
	if err = json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = Page(a)
	return nil
}

// ParsePage 从 JSON 字节解析页面文档。
func ParsePage(data []byte) (p *Page, err error) {
	p = &Page{}
	if err = json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("页面文档解析失败: %w", err)
	}
	return p, nil
}

// CompiledPage 页面文档的静态编译输出。
type CompiledPage struct {
	// Title 页面标题，注入 <title>。
	Title string
	// MetaDescription 页面描述，注入 <meta name="description">。
	MetaDescription string
	// BodyClasses 注入 <body> 的 class 列表。
	BodyClasses []string
	// HTML 组件树 HTML（单层语义标签，无内联样式、无脚本）。
	HTML string
	// CSS 全部样式（含响应式媒体查询与入场动效关键帧）。
	CSS string
}

// CompileOption 编译选项。
type CompileOption func(*compileConfig)

// compileConfig 编译配置。
type compileConfig struct {
	media   core.MediaResolver
	content core.ContentResolver
}

// WithMediaResolver 注入媒体解析器（构建期媒体元数据注入，规范 docs/02-B §4）。
func WithMediaResolver(r core.MediaResolver) CompileOption {
	return func(c *compileConfig) { c.media = r }
}

// WithContentResolver 注入 CMS 内容解析器（构建期动态绑定静态填入，规范 docs/02-C1）。
func WithContentResolver(r core.ContentResolver) CompileOption {
	return func(c *compileConfig) { c.content = r }
}

// Compile 编译页面文档。确定性保证：同一输入产生完全相同的输出。
func Compile(p *Page, opts ...CompileOption) (res *CompiledPage, err error) {
	if p == nil {
		return nil, errors.New("页面文档为空")
	}
	cfg := &compileConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if err = validateSettings(&p.Settings); err != nil {
		return nil, fmt.Errorf("页面设置: %w", err)
	}
	ids := map[string]bool{}
	for i, n := range p.Root {
		if err = core.ValidateNode(n, ids); err != nil {
			return nil, fmt.Errorf("顶级节点 %d: %w", i, err)
		}
	}

	var b core.CSSBuckets
	compileSettingsCSS(&p.Settings, &b)

	var htmlBuf strings.Builder
	ctx := &core.RenderContext{HTML: &htmlBuf, CSS: &b, Media: cfg.media, Content: cfg.content}
	for _, n := range p.Root {
		if err = core.RenderNode(n, true, ctx); err != nil {
			return nil, err
		}
	}

	classes := []string{bodyClassPage}
	if p.Settings.Layout.Mode == LayoutBoxed {
		classes = append(classes, bodyClassBoxed)
	} else {
		classes = append(classes, bodyClassFull)
	}
	classes = append(classes, p.Settings.BodyClasses...)

	return &CompiledPage{
		Title:           p.Settings.SEO.Title,
		MetaDescription: p.Settings.SEO.Description,
		BodyClasses:     classes,
		HTML:            htmlBuf.String(),
		CSS:             b.String(),
	}, nil
}

// RenderDocument 将编译输出组装为完整 HTML 文档（用于预览与静态发布产物）。
func RenderDocument(c *CompiledPage) string {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n")
	sb.WriteString("<meta charset=\"utf-8\">\n")
	sb.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	if c.Title != "" {
		sb.WriteString("<title>")
		sb.WriteString(html.EscapeString(c.Title))
		sb.WriteString("</title>\n")
	}
	if c.MetaDescription != "" {
		sb.WriteString("<meta name=\"description\" content=\"")
		sb.WriteString(html.EscapeString(c.MetaDescription))
		sb.WriteString("\">\n")
	}
	sb.WriteString("<style>\n")
	sb.WriteString(c.CSS)
	sb.WriteString("\n</style>\n</head>\n<body")
	if len(c.BodyClasses) > 0 {
		sb.WriteString(" class=\"")
		sb.WriteString(strings.Join(c.BodyClasses, " "))
		sb.WriteString("\"")
	}
	sb.WriteString(">\n")
	sb.WriteString(c.HTML)
	sb.WriteString("\n</body>\n</html>\n")
	return sb.String()
}