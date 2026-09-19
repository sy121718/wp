package pipeline

// structure_template.go — 结构模板（页眉 / 页脚）在构建期的解析、优先级与回退。
//
// 为什么放在 pipeline 而不是 page 与 presentation 各一份：结构槽位的规则是
// 「模板优先 → 模板不可用则回退块绑定 → 按实际消费登记依赖」——两条构建路径
// （手工 Page / 自动发布实例）必须逐字一致。各写一份的代价不是多几行代码，
// 而是任何一次改动只会在其中一条生效，表现是「手工页面换了页眉模板、自动发布
// 详情页还挂着旧页眉」这种没有任何报错的静默分叉。
//
// 本文件只做「结构槽位 → 编译期绑定 + 依赖键」，不认识任何业务模块：
// 模板文档从 StructureTemplatePort 取（各消费者自带最窄适配，见 page / presentation）。

import (
	"context"
	"errors"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/pkg/logger"
)

// StructureTemplatePort 结构模板文档解析端口（消费者侧最窄接口）。
//
// 只表达「按工程作用域取某套结构模板的当前版本文档」这一件事：调用方不需要知道
// contenttemplate 的 DTO、版本表与类型校验。**返回错误、返回空文档、解析不出
// root 节点，三者一律等同「这套模板不可用」** —— 构建期回退到该槽位的块绑定，
// 绝不因为一个配错的模板让整站页眉页脚消失（存量站零回归的硬要求）。
type StructureTemplatePort interface {
	ResolveStructureDocument(ctx context.Context, projectID, templateID string) ([]byte, error)
}

// structureSlotResolver 把「结构模板虚拟引用」叠加在既有块解析器之上。
//
// 结构模板在构建期被解析成 root 节点，用 builder.StructureTemplateRef 生成一个
// 只存在于本次编译的引用 ID；布局槽节点因此仍走 core.layoutSlot 的既有通道
// （防环 / 深度上限 / ID 前缀重写 / main 地标跳过全部复用），不必另开一条模板通道。
type structureSlotResolver struct {
	inner core.BlockResolver
	refs  map[string][]*core.Node
}

// ResolveBlockRoot 实现 core.BlockResolver：虚拟引用优先，其余交给既有解析器。
func (r *structureSlotResolver) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	if nodes, ok := r.refs[blockID]; ok {
		return nodes, nil
	}
	return r.inner.ResolveBlockRoot(blockID)
}

// structureSlotPlan 单个槽位的编译决策（优先级与回退都只在这一处发生）。
type structureSlotPlan struct {
	// Slot 槽位名（header / footer / 其余白名单槽位）。
	Slot string
	// BlockID 交给 builder.StructureSlot 的值：模板槽位是构建期虚拟引用，其余是全局块 ID。
	BlockID string
	// TemplateID 非空 = 该槽位由结构模板提供（依赖按模板 + 模板内引用块登记）。
	TemplateID string
	// Nodes 模板展开出的 root 节点（仅 TemplateID 非空时有效）。
	Nodes []*core.Node
}

// deps 该槽位实际消费的依赖键。
//
// 关键在「实际消费」四个字：回退到块绑定后**不登记** content_template:{id}。
// 否则一次配错的模板会留下一条永远命不中的依赖行，读者会以为「改了那套模板
// 就会重建本页」，而本页根本没在用那套模板。
func (p structureSlotPlan) deps() []Dependency {
	if p.TemplateID == "" {
		k := BlockKey(p.BlockID)
		return []Dependency{{Kind: k.Kind, Key: k.Key}}
	}
	k := ContentTemplateKey(p.TemplateID)
	out := []Dependency{{Kind: k.Kind, Key: k.Key}}
	// 模板文档内部还可以用 core.globalref 引用块（页眉里的公告条等）：
	// 改那个块同样要重建引用页，而它不出现在引用页的文档里，静态扫描看不到。
	for _, blockID := range builder.ReferencedBlockIDs(p.Nodes) {
		bk := BlockKey(blockID)
		out = append(out, Dependency{Kind: bk.Kind, Key: bk.Key})
	}
	return out
}

// planStructureSlots 把结构绑定解成逐槽位的编译决策（模板优先 → 回退块）。
//
// 优先级与回退（零回归的硬要求）：
//   - 同一槽位同时绑定了模板与块时**模板优先**；
//   - 模板未绑定、解析失败、文档非法 → **回退到块绑定**并记日志；
//   - 两者都没有（或都不可用）→ 该槽位不产出绑定（页眉页脚整块不渲染，与改造前一致）。
func planStructureSlots(ctx context.Context, port StructureTemplatePort, projectID string,
	structure builder.StructureBindings) []structureSlotPlan {
	templateBindings := structure.TemplateBindings()
	blockBindings := structure.SlotBindings()
	if len(templateBindings) == 0 && len(blockBindings) == 0 {
		return nil
	}
	// SortedSlots 收的是「槽位 → 值」的 map（值只用于排序判定之外的场合），
	// 这里值留空：下面按槽位各自去查模板 / 块绑定。
	slotSet := map[string]string{}
	for slot := range blockBindings {
		slotSet[slot] = ""
	}
	for slot := range templateBindings {
		slotSet[slot] = ""
	}
	var out []structureSlotPlan
	for _, slot := range builder.SortedSlots(slotSet) {
		// 1) 模板优先。
		if templateID := strings.TrimSpace(templateBindings[slot]); templateID != "" {
			nodes, terr := structureTemplateNodes(ctx, port, projectID, templateID)
			if terr == nil && len(nodes) > 0 {
				out = append(out, structureSlotPlan{
					Slot: slot, TemplateID: templateID,
					BlockID: builder.StructureTemplateRef(templateID), Nodes: nodes,
				})
				continue
			}
			// 解析失败：回退块绑定（否则现存站的页眉页脚会整片消失）。
			logger.Scene("build").With("slot", slot).With("template_id", templateID).With("err", structureTemplateErrText(terr)).
				Warn("结构模板不可用，回退到该槽位的块绑定")
		}
		// 2) 块绑定（含上面的回退路径）。
		if blockID := strings.TrimSpace(blockBindings[slot]); blockID != "" {
			out = append(out, structureSlotPlan{Slot: slot, BlockID: blockID})
		}
	}
	return out
}

// BuildStructureSlots 把 settings.structure 解析成编译期槽位绑定 + 模板虚拟引用 + 依赖键。
//
// 返回值：
//   - slots 交给 builder.WithStructureSlots；
//   - resolver 在出现模板槽位时是叠加了解析器的包装（**调用方必须用它替换原来的
//     WithBlockResolver**，否则模板虚拟引用无人解析）；没有模板槽位时原样返回 inner；
//   - deps 是该绑定实际消费的依赖键（模板 / 模板内引用块 / 回退块）。
func BuildStructureSlots(ctx context.Context, port StructureTemplatePort, projectID string,
	structure builder.StructureBindings, inner core.BlockResolver) (
	slots []builder.StructureSlot, resolver core.BlockResolver, deps []Dependency) {
	plans := planStructureSlots(ctx, port, projectID, structure)
	if len(plans) == 0 {
		return nil, inner, nil
	}
	refs := map[string][]*core.Node{}
	for _, p := range plans {
		slots = append(slots, builder.StructureSlot{Slot: p.Slot, BlockID: p.BlockID})
		deps = append(deps, p.deps()...)
		if p.TemplateID != "" {
			refs[p.BlockID] = p.Nodes
		}
	}
	if len(refs) == 0 {
		return slots, inner, deps
	}
	return slots, &structureSlotResolver{inner: inner, refs: refs}, deps
}

// StructureSlotDependencies 只推导结构槽位的依赖键，不做编译。
//
// 给「构建之后才落库」的静态依赖扫描用（page 模块的 page_dependency.go）：
// 它必须与构建期用同一个 planner，否则会出现在产物里生效的绑定没被登记、
// 或一个已经回退掉的模板被登记成依赖 —— 两种都表现为「改了源头，页面不重建」。
func StructureSlotDependencies(ctx context.Context, port StructureTemplatePort, projectID string,
	structure builder.StructureBindings) []Dependency {
	plans := planStructureSlots(ctx, port, projectID, structure)
	var out []Dependency
	for _, p := range plans {
		out = append(out, p.deps()...)
	}
	return out
}

// structureTemplateNodes 读取结构模板的当前版本文档并解析为 root 节点。
func structureTemplateNodes(ctx context.Context, port StructureTemplatePort, projectID, templateID string) ([]*core.Node, error) {
	if port == nil {
		return nil, errors.New("结构模板端口未装配")
	}
	doc, err := port.ResolveStructureDocument(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	if len(doc) == 0 {
		return nil, errors.New("结构模板无可用文档")
	}
	parsed, err := builder.ParsePage(doc)
	if err != nil {
		return nil, err
	}
	return parsed.Root, nil
}

// structureTemplateErrText 日志用：nil 错误不产生 "null" 噪声。
func structureTemplateErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
