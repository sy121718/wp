package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// AtomRender 基座传给组件渲染函数的辅助产物（公共接线已由基座完成）。
type AtomRender struct {
	NodeID   string
	Classes  string // 已合并的 class 列表（节点类 wp-c-<id> + Advanced 自定义类）
	CustomID string // 自定义 Element ID（可空，已过白名单与全文档查重）
	CSS      *CSSBuckets
	TopLevel bool
	// Content CMS 内容解析器（透传自 RenderContext）：绑定组件（heading/text）经此解析字段。
	Content ContentResolver
}

// AtomSpec 原子组件规格：组件作者只填这三项（对标 WP Widget 三件套）。
type AtomSpec[P any] struct {
	// TypeName 组件类型标识（如 "core.spacer"）。
	TypeName string
	// Render 组件 HTML 渲染：返回该节点的完整 HTML 字符串。
	// 公共产物（class 合并/自定义 ID/Advanced CSS 编译）由基座完成后经 h 传入；
	// 组件自身样式经 h.CSS 追加。
	Render func(node *Node, p *P, h *AtomRender) (string, error)
	// ValidateExtra 关系性校验（可选）：互斥/依赖等字段级（ct tag）之外的规则。
	ValidateExtra func(p *P, nodeID string) error
}

// Atom 泛型原子组件基座，实现 Component 接口。
//
// 基座吸收全部公共样板：
//   - 节点 ID 白名单与全文档查重（ValidateNodeID）
//   - 叶子约束（原子组件不允许子节点）
//   - props JSON 解码
//   - 声明式字段校验（ct tag → ValidateSpec）
//   - Advanced 通用层校验与 CSS 编译（ValidateAdvanced / CompileAdvanced）
//   - class 合并与自定义 Element ID 织入
//
// 组件作者 = Props 结构体（ct tag）+ Render 函数 + 可选 ValidateExtra，
// 与 WP 写一个 widget 的工作量对齐（docs/02-C5）。
type Atom[P any] struct{ Spec AtomSpec[P] }

// Type 实现组件接口。
func (a Atom[P]) Type() string { return a.Spec.TypeName }

// PropsSpec 实现 SpecProvider：返回 Props 零值实例供声明式 Controls
// 生成 Inspector 面板 schema（docs/02-C3）。组件作者零配置。
func (a Atom[P]) PropsSpec() any { var p P; return &p }

// Validate 实现组件接口：公共校验管线 + 声明式 + 组件关系性 + Advanced。
func (a Atom[P]) Validate(node *Node, ids map[string]bool) (err error) {
	if err = ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 原子组件为叶子节点，不允许子节点", node.ID)
	}
	var p P
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if err = ValidateSpec(&p, node.ID); err != nil {
		return err
	}
	if a.Spec.ValidateExtra != nil {
		if err = a.Spec.ValidateExtra(&p, node.ID); err != nil {
			return fmt.Errorf("节点 %s: %w", node.ID, err)
		}
	}
	if adv := AdvancedOf(&p); adv != nil {
		return ValidateAdvanced(adv, node.ID, ids)
	}
	return nil
}

// Render 实现组件接口：解码 → Advanced 编译 → 组件渲染 → 写出。
func (a Atom[P]) Render(node *Node, topLevel bool, ctx *RenderContext) (err error) {
	var p P
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	var extraClasses []string
	var customID string
	if adv := AdvancedOf(&p); adv != nil {
		extraClasses, customID = CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	h := &AtomRender{
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		CSS:      ctx.CSS,
		TopLevel: topLevel,
		Content:  ctx.Content,
	}
	out, err := a.Spec.Render(node, &p, h)
	if err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	ctx.HTML.WriteString(out)
	return nil
}

// AdvancedOf 反射读取 P 的 Advanced 字段（约定：类型为 core.AdvancedProps、
// 字段名 Advanced）。原子组件 Props 必须内嵌该字段以获得通用层能力。
func AdvancedOf[P any](p *P) *AdvancedProps {
	v := reflect.ValueOf(p)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName("Advanced")
	if !f.IsValid() || f.Type() != reflect.TypeOf(AdvancedProps{}) {
		return nil
	}
	if !f.CanAddr() {
		return nil
	}
	return f.Addr().Interface().(*AdvancedProps)
}

// ValidateNodeID 节点 ID 白名单与全文档唯一性，并顺带校验编辑元数据 Name（03-A）。
func ValidateNodeID(id, name string, ids map[string]bool) (err error) {
	if len(id) < 1 || len(id) > 64 {
		return fmt.Errorf("无效的节点 ID: %q", id)
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Errorf("无效的节点 ID: %q", id)
		}
	}
	if ids[id] {
		return fmt.Errorf("节点 ID 重复: %q", id)
	}
	ids[id] = true
	if err = ValidateNodeName(name); err != nil {
		return err
	}
	return nil
}

// ValidateNodeName 编辑元数据 Name 白名单（仅编辑器显示名，最长为 100 可见字符）。
func ValidateNodeName(name string) (err error) {
	if len([]rune(name)) > 100 {
		return fmt.Errorf("节点名称过长（上限 100 字符）")
	}
	return nil
}
