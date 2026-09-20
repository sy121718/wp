package pipeline

// structure_template.go — 结构模板（页眉 / 页脚）在构建期的解析、优先级与回退。
//
// 为什么放在 pipeline 而不是 page 与 presentation 各一份：结构槽位的规则是
// 「模板优先 → 模板不可用时回退块绑定 → 按实际消费登记依赖」——两条构建路径
// （手工 Page / 自动发布实例）必须逐字一致。各写一份的代价不是多几行代码，
// 而是任何一次改动只会在其中一条生效，表现是「手工页面换了页眉模板、自动发布
// 详情页还挂着旧页眉」这种没有任何报错的静默分叉。
//
// 本文件只做「结构槽位 → 编译期绑定 + 依赖键」，不认识任何业务模块：
// 模板文档从 StructureTemplatePort 取（各消费者自带最窄适配，见 page / presentation）。
//
// 模式（审计 ARCH-05）：回退**只在预览模式**成立。发布模式下，显式绑定了模板却拿不到
// （不存在 / 跨工程 / 文档非法）直接失败 —— 一次配错的模板绑定不该让一个缺了页眉的
// 页面发布上线、而构建接口还返回成功。判据只有一条：**没绑定允许降级，绑定了但拿不到
// 就是失败**；「拿到了但是空的」不算拿不到（作者的正常中间态），降级 + 记诊断。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/pkg/logger"
)

// StructureTemplatePort 结构模板文档解析端口（消费者侧最窄接口）。
//
// 只表达「按工程作用域取某套结构模板的当前版本文档」这一件事：调用方不需要知道
// contenttemplate 的 DTO、版本表与类型校验。**返回错误、返回空文档、解析不出
// root 节点，三者一律等同「这套模板不可用」**。
//
// 不可用之后怎么处置由调用方模式决定（见 StructureSlotInput.Mode）：
// 预览回退到该槽位的块绑定（否则一次配错的模板会让存量站的页眉页脚整片消失），
// 发布直接失败（否则缺一截的页面会被静默推上线）。
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
	// failed 拿不到的结构模板虚拟引用 → 稳定归因码（仅预览模式会写入）。
	//
	// 为什么需要这份映射：没有它时引用会落到 inner（块解析器）上，报出来是
	// 「全局块 __structure_template__xxx 不可用」—— 归因指向一个不存在的块，
	// 恰好把「这套模板没配好」这条最有用的线索盖掉了。
	failed map[string]string
}

// ResolveBlockRoot 实现 core.BlockResolver：虚拟引用优先，其余交给既有解析器。
func (r *structureSlotResolver) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	if nodes, ok := r.refs[blockID]; ok {
		return nodes, nil
	}
	if reason, ok := r.failed[blockID]; ok {
		return nil, &core.RefDegradeError{Reason: reason, Err: fmt.Errorf("结构模板引用 %s 未展开", blockID)}
	}
	if r.inner == nil {
		return nil, &core.RefDegradeError{Reason: core.RefReasonResolverMissing, Err: errors.New("块解析器未注入")}
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
	// Nodes 模板展开出的 root 节点（仅 TemplateID 非空且解析成功时有效）。
	Nodes []*core.Node
	// FailedReason 非空 = 模板显式绑定但拿不到，且没有块可回退（仅预览模式）：
	// 仍产出槽位节点，让画布里的占位能归因到「哪套模板没展开」。
	FailedReason string
}

// deps 该槽位实际消费的依赖键。
//
// 关键在「实际消费」四个字：回退到块绑定、或模板压根没展开时**不登记**
// content_template:{id}。否则会留下一条永远命不中的依赖行，读者会以为
// 「改了那套模板就会重建本页」，而本页根本没在用那套模板。
func (p structureSlotPlan) deps() []Dependency {
	if p.FailedReason != "" {
		// 模板没展开 = 这次产物与它无关。依赖表是「产物消费了什么」的查询投影，
		// 不是「我希望它消费什么」的清单；两者混用会让失效反查指向没被消费的源头。
		return nil
	}
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

// StructureSlotInput 结构槽位解析的输入（端口 / 作用域 / 绑定 / 模式 / 诊断收集器）。
//
// 用结构体而不是继续加位置参数：Mode 与 Diagnostics 都属于「调用方策略」，
// 其中 Mode 的零值（空串）有明确含义 —— 按预览处理（容忍降级），
// 于是漏传不会把预览悄悄变成发布、也不会把发布悄悄变成预览。
type StructureSlotInput struct {
	// Port 结构模板文档来源；nil = 未装配（等同「所有模板都拿不到」）。
	Port StructureTemplatePort
	// ProjectID 工程作用域：模板与块的解析都按它做越权防护（跨工程 = 拿不到）。
	ProjectID string
	// Structure 文档 / 主题快照里的结构绑定。
	Structure builder.StructureBindings
	// Inner 既有块解析器：模板槽位之外的引用交给它；出现模板槽位时它会被叠加包装。
	Inner core.BlockResolver
	// Mode 本次编译的用途（见 builder.CompileMode）。
	Mode builder.CompileMode
	// Diagnostics 归因收集器（可选；nil 接收者安全，未注入即不记录）。
	Diagnostics *builder.DegradeCollector
}

// StructureSlotResult 结构槽位解析结果。
type StructureSlotResult struct {
	// Slots 交给 builder.WithStructureSlots。
	Slots []builder.StructureSlot
	// Resolver 在出现模板槽位时是叠加了解析器的包装（**调用方必须用它替换原来的
	// WithBlockResolver**，否则模板虚拟引用无人解析）；没有模板槽位时原样返回 inner。
	Resolver core.BlockResolver
	// Deps 该绑定实际消费的依赖键（模板 / 模板内引用块 / 回退块）。
	Deps []Dependency
}

// planStructureSlots 把结构绑定解成逐槽位的编译决策（模板优先 → 回退块）。
//
// 优先级与回退：
//   - 同一槽位同时绑定了模板与块时**模板优先**；
//   - 模板绑定且可用 → 用模板；
//   - 模板绑定但拿不到（不存在 / 跨工程 / 文档非法）→ **发布失败**；预览回退到块绑定
//     （没有块可回退时留一个带归因的槽位占位）；
//   - 模板绑定且能拿到、但没有任何内容 → 不算「拿不到」：按显式设计回退块绑定并记诊断；
//   - 两者都没有（或都不可用）→ 该槽位不产出绑定（页眉页脚整块不渲染，与改造前一致）。
func planStructureSlots(ctx context.Context, in StructureSlotInput) ([]structureSlotPlan, error) {
	templateBindings := in.Structure.TemplateBindings()
	blockBindings := in.Structure.SlotBindings()
	if len(templateBindings) == 0 && len(blockBindings) == 0 {
		return nil, nil
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
			nodes, terr := structureTemplateNodes(ctx, in.Port, in.ProjectID, templateID)
			switch {
			case terr != nil:
				// 显式绑定但拿不到 —— 本次整改的核心分界。
				if in.Mode == builder.CompileModePublish {
					return nil, fmt.Errorf("结构槽位 %s 绑定的结构模板 %s 不可用（%s）：发布被拒绝，避免上线缺结构的不完整页面",
						slot, templateID, structureTemplateErrText(terr))
				}
				in.Diagnostics.Record(builder.Degrade{
					Slot: slot, Kind: builder.DegradeKindTemplate, RefID: templateID,
					Reason: core.RefReasonTemplateUnavailable,
				})
				if blockID := strings.TrimSpace(blockBindings[slot]); blockID != "" {
					logger.Scene("build").With("slot", slot).With("template_id", templateID).
						With("err", structureTemplateErrText(terr)).
						Warn("结构模板不可用，预览回退到该槽位的块绑定")
					out = append(out, structureSlotPlan{Slot: slot, BlockID: blockID})
					continue
				}
				logger.Scene("build").With("slot", slot).With("template_id", templateID).
					With("err", structureTemplateErrText(terr)).
					Warn("结构模板不可用且无块可回退，预览输出带归因的槽位占位")
				out = append(out, structureSlotPlan{
					Slot: slot, TemplateID: templateID,
					BlockID:      builder.StructureTemplateRef(templateID),
					FailedReason: core.RefReasonTemplateUnavailable,
				})
				continue
			case len(nodes) == 0:
				// 模板存在、能拿到，只是没有任何内容：不是配置错误，按显式设计回退块绑定。
				in.Diagnostics.Record(builder.Degrade{
					Slot: slot, Kind: builder.DegradeKindTemplate, RefID: templateID,
					Reason: core.RefReasonTemplateEmpty,
				})
				logger.Scene("build").With("slot", slot).With("template_id", templateID).
					Warn("结构模板没有任何内容，回退到该槽位的块绑定")
			default:
				out = append(out, structureSlotPlan{
					Slot: slot, TemplateID: templateID,
					BlockID: builder.StructureTemplateRef(templateID), Nodes: nodes,
				})
				continue
			}
		}
		// 2) 块绑定（含上面的回退路径）。
		if blockID := strings.TrimSpace(blockBindings[slot]); blockID != "" {
			out = append(out, structureSlotPlan{Slot: slot, BlockID: blockID})
		}
	}
	return out, nil
}

// BuildStructureSlots 把 settings.structure 解析成编译期槽位绑定 + 模板虚拟引用 + 依赖键。
//
// 返回 StructureSlotResult（字段含义见其注释）。Mode 为 builder.CompileModePublish 时，
// 显式绑定的结构模板拿不到会返回错误 —— 调用方必须让它成为本次构建的失败，
// 而不是继续产出一份缺了结构的页面。
func BuildStructureSlots(ctx context.Context, in StructureSlotInput) (StructureSlotResult, error) {
	plans, err := planStructureSlots(ctx, in)
	if err != nil {
		return StructureSlotResult{}, err
	}
	if len(plans) == 0 {
		return StructureSlotResult{Resolver: in.Inner}, nil
	}
	var out StructureSlotResult
	refs := map[string][]*core.Node{}
	failed := map[string]string{}
	for _, p := range plans {
		out.Slots = append(out.Slots, builder.StructureSlot{Slot: p.Slot, BlockID: p.BlockID})
		out.Deps = append(out.Deps, p.deps()...)
		if p.TemplateID == "" {
			continue
		}
		if p.FailedReason != "" {
			failed[p.BlockID] = p.FailedReason
			continue
		}
		refs[p.BlockID] = p.Nodes
	}
	if len(refs) == 0 && len(failed) == 0 {
		out.Resolver = in.Inner
		return out, nil
	}
	out.Resolver = &structureSlotResolver{inner: in.Inner, refs: refs, failed: failed}
	return out, nil
}

// StructureSlotDependencies 只推导结构槽位的依赖键，不做编译。
//
// 给「构建之后才落库」的静态依赖扫描用（page 模块的 page_dependency.go）：
// 它必须与构建期用同一个 planner，否则会出现在产物里生效的绑定没被登记、
// 或一个已经回退掉的模板被登记成依赖 —— 两种都表现为「改了源头，页面不重建」。
//
// 这里按**容忍模式**（零值 = 预览）解析：它在构建成功之后才被调用，不承担把关职责，
// 也不该因为一条诊断改变返回值。真正该失败的绑定在构建期已经拦下了。
func StructureSlotDependencies(ctx context.Context, port StructureTemplatePort, projectID string,
	structure builder.StructureBindings) []Dependency {
	plans, _ := planStructureSlots(ctx, StructureSlotInput{
		Port: port, ProjectID: projectID, Structure: structure,
	})
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
