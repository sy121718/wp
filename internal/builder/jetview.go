// jetview.go — 组件渲染从「Go 字符串拼接」迁移到 Jet 模板的转换层（Phase 0 + Phase 1）。
//
// 职责划分：
//   - nodeViewOf 把 core.Node 树转换为 nodeView 视图树（props 解码、CSS 生成、校验与递归驱动）；
//   - Jet 模板（button.jet / container.jet / <组件>.jet）只根据 nodeView 的字段拼装 HTML；
//   - CSS 复用组件包导出的 CompileCSS（与旧 render 内部逻辑完全一致），保证字节等价。
//
// 这是与 builder.Compile 并行的新路径：旧 render 输出保持不变，字节等价由
// jetview_test.go 的对比测试证明。
package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/CloudyKit/jet/v6"

	buttonPkg "go_wp/internal/builder/components/button"
	containerPkg "go_wp/internal/builder/components/container"
	counterPkg "go_wp/internal/builder/components/counter"
	dividerPkg "go_wp/internal/builder/components/divider"
	headingPkg "go_wp/internal/builder/components/heading"
	imagePkg "go_wp/internal/builder/components/image"
	infoboxPkg "go_wp/internal/builder/components/infobox"
	listPkg "go_wp/internal/builder/components/list"
	socialbuttonsPkg "go_wp/internal/builder/components/socialbuttons"
	spacerPkg "go_wp/internal/builder/components/spacer"
	textPkg "go_wp/internal/builder/components/text"
	videoPkg "go_wp/internal/builder/components/video"
	"go_wp/internal/builder/core"
)

// nodeView 组件渲染视图：Node 树 → view 树的中间表示。
//
// 通用字段（Type/Template/Classes/Props/Children）承载结构与上下文；
// V 是各组件 BuildView 预计算的渲染数据（标量字段/切片），模板经 .V.XXX 访问；
// button/container 保留拍平字段（Tag/Attrs/Text/Icon*/Shape*）向后兼容既有模板。
type nodeView struct {
	Type     string      // 组件类型标识（core.button / core.container / ...）
	Template string      // 模板名（button / container / heading / ...）
	NodeID   string      // 节点 ID
	Classes  string      // 已合并 class（wp-c-<id> [+ wp-section] [+ 自定义类]）
	CustomID string      // 自定义 Element ID（空则无）
	TopLevel bool        // 是否页面第一层顶级 Section
	Props    any         // 解码后的组件 props
	Children []*nodeView // 子节点视图（container 专用）

	// V 组件 BuildView 预计算的渲染视图数据（模板经 .V.XXX 访问）。
	V any

	// --- button / container 拍平字段（Phase 0 样板，向后兼容） ---
	Tag         string // 语义标签（a/button/div/section/...）
	Attrs       string // 前导空格 + 属性串（已转义）
	Text        string // button 文本
	IconPrefix  string // button 前缀图标 HTML（空则无）
	IconSuffix  string // button 后缀图标 HTML（空则无）
	ShapeTop    string // container 顶部形状分隔线 SVG（空则无）
	ShapeBottom string // container 底部形状分隔线 SVG（空则无）
}

// nodeViewOf 把单个 Node 转换为 nodeView（含递归 children，CSS 加入顺序对齐旧路径）。
func nodeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	switch node.Type {
	case buttonPkg.Type:
		return buttonViewOf(node, topLevel, ctx)
	case containerPkg.Type:
		return containerViewOf(node, topLevel, ctx)
	case headingPkg.Type:
		return headingViewOf(node, topLevel, ctx)
	case textPkg.Type:
		return textViewOf(node, topLevel, ctx)
	case imagePkg.Type:
		return imageViewOf(node, topLevel, ctx)
	case dividerPkg.Type:
		return dividerViewOf(node, topLevel, ctx)
	case spacerPkg.Type:
		return spacerViewOf(node, topLevel, ctx)
	case listPkg.Type:
		return listViewOf(node, topLevel, ctx)
	case infoboxPkg.Type:
		return infoboxViewOf(node, topLevel, ctx)
	case socialbuttonsPkg.Type:
		return socialbuttonsViewOf(node, topLevel, ctx)
	case videoPkg.Type:
		return videoViewOf(node, topLevel, ctx)
	case counterPkg.Type:
		return counterViewOf(node, topLevel, ctx)
	default:
		return nil, fmt.Errorf("nodeView: 不支持的组件类型 %q", node.Type)
	}
}

// buttonViewOf 转换 button 节点（对应 core.Atom 基座的 Render 流程）。
func buttonViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p buttonPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	// Advanced 通用层：CSS 编译 + 自定义 class/customID（与 Atom.Render 一致）。
	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	// 组件样式（与 render 内部 compileCSS 一致）。
	buttonPkg.CompileCSS(node.ID, &p, ctx.CSS)

	// 渲染视图数据（标签 + 属性 + 图标）。
	view, err := buttonPkg.BuildView(&p, ctx.Content)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	return &nodeView{
		Type:       buttonPkg.Type,
		Template:   "button",
		NodeID:     node.ID,
		Classes:    strings.Join(classes, " "),
		CustomID:   customID,
		TopLevel:   topLevel,
		Props:      p,
		Tag:        view.Tag,
		Attrs:      view.Attrs,
		Text:       view.Text,
		IconPrefix: view.IconPrefix,
		IconSuffix: view.IconSuffix,
	}, nil
}

// containerViewOf 转换 container 节点（对应 Container.Render 流程）。
func containerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p containerPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	cls := core.NodeClass(node.ID)
	if topLevel {
		cls += " " + core.SectionClass
	}

	// 先递归 children：CSS 加入顺序为「子节点先、容器自身后」，与 Container.Render 一致。
	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	// 容器自身样式（与 Render 内部 compileCSS 一致）。
	containerPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view := containerPkg.BuildView(node, &p)

	return &nodeView{
		Type:        containerPkg.Type,
		Template:    "container",
		NodeID:      node.ID,
		Classes:     cls,
		TopLevel:    topLevel,
		Props:       p,
		Children:    children,
		Tag:         view.Tag,
		Attrs:       view.Attrs,
		ShapeTop:    view.ShapeTop,
		ShapeBottom: view.ShapeBottom,
	}, nil
}

// headingViewOf 转换 heading 节点（对应 core.Atom 基座的 Render 流程）。
func headingViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p headingPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	headingPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view, err := headingPkg.BuildView(&p, ctx.Content)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	return &nodeView{
		Type:     headingPkg.Type,
		Template: "heading",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// textViewOf 转换 text 节点（对应 core.Atom 基座的 Render 流程）。
func textViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p textPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	textPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view, err := textPkg.BuildView(&p, ctx.Content)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	return &nodeView{
		Type:     textPkg.Type,
		Template: "text",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// imageViewOf 转换 image 节点（对应 core.Atom 基座的 Render 流程）。
// 说明：image 的 customID 位置随点击动作分支变化（img 前 / a 上 / 忽略），
// 故 class 与 customID 传入 BuildView 预计算完整 HTML，模板仅原样输出。
func imageViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p imagePkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)
	classStr := strings.Join(classes, " ")

	imagePkg.CompileCSS(node.ID, &p, ctx.CSS)

	view, err := imagePkg.BuildView(node, &p, classStr, customID, ctx.Content)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	return &nodeView{
		Type:     imagePkg.Type,
		Template: "image",
		NodeID:   node.ID,
		Classes:  classStr,
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// dividerViewOf 转换 divider 节点（对应 core.Atom 基座的 Render 流程）。
func dividerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p dividerPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	dividerPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view := dividerPkg.BuildView(&p)

	return &nodeView{
		Type:     dividerPkg.Type,
		Template: "divider",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// spacerViewOf 转换 spacer 节点（对应 core.Atom 基座的 Render 流程）。
func spacerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p spacerPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	spacerPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view := spacerPkg.BuildView(&p)

	return &nodeView{
		Type:     spacerPkg.Type,
		Template: "spacer",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// listViewOf 转换 list 节点（对应 Component.Render 流程，无 Advanced 层）。
func listViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p listPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	listPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := listPkg.BuildView(&p)

	return &nodeView{
		Type:     listPkg.Type,
		Template: "list",
		NodeID:   node.ID,
		Classes:  core.NodeClass(node.ID),
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// infoboxViewOf 转换 infobox 节点（对应 Component.Render 流程，无 Advanced 层）。
func infoboxViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p infoboxPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	infoboxPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := infoboxPkg.BuildView(&p)

	return &nodeView{
		Type:     infoboxPkg.Type,
		Template: "infobox",
		NodeID:   node.ID,
		Classes:  core.NodeClass(node.ID),
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// socialbuttonsViewOf 转换 socialbuttons 节点（对应 Component.Render 流程，无 Advanced 层）。
func socialbuttonsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p socialbuttonsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	socialbuttonsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := socialbuttonsPkg.BuildView(&p)

	return &nodeView{
		Type:     socialbuttonsPkg.Type,
		Template: "socialbuttons",
		NodeID:   node.ID,
		Classes:  core.NodeClass(node.ID),
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// videoViewOf 转换 video 节点（对应 Component.Render 流程，无 Advanced 层）。
func videoViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p videoPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	videoPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := videoPkg.BuildView(&p)

	return &nodeView{
		Type:     videoPkg.Type,
		Template: "video",
		NodeID:   node.ID,
		Classes:  core.NodeClass(node.ID),
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// counterViewOf 转换 counter 节点（对应 Component.Render 流程，无 Advanced 层）。
func counterViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p counterPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	counterPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := counterPkg.BuildView(&p)

	return &nodeView{
		Type:     counterPkg.Type,
		Template: "counter",
		NodeID:   node.ID,
		Classes:  core.NodeClass(node.ID),
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// renderView 用 Jet 渲染单个 nodeView 树到 w（递归由 container.jet 的 include 驱动）。
func renderView(v *nodeView, set *jet.Set, w io.Writer) error {
	tpl, err := set.GetTemplate(v.Template)
	if err != nil {
		return err
	}
	return tpl.Execute(w, nil, v)
}
