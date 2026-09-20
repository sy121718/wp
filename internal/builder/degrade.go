package builder

// degrade.go — 「引用块 / 结构模板不可用」的发布策略与归因诊断（审计 ARCH-05）。
//
// 背景：core.globalref 与 core.layoutSlot（结构槽位）在引用拿不到时都会降级成占位 div。
// 这对预览友好（编辑期配置不完整是常态），却让**发布**也照常成功 —— 一次配错的模板绑定
// 会把缺了页眉的页面推上线，而构建接口返回的是成功，运营只能靠肉眼看出来。
//
// 本文件把那条分界显式化（唯一的判据）：
//   - 没绑定：该槽位本来就不产出内容（既有设计，不算降级）；
//   - 绑定了但拿不到 / 非法：**发布失败**，预览降级为带归因的占位；
//   - 绑定了、拿得到、只是内容为空（ref_empty）：不属于「拿不到」—— 发布照常，
//     但记一条诊断进 Manifest（操作者据此知道这份产物少了什么）。
//
// 为什么诊断要进 Manifest：发布失败时根本不存在 Manifest，所以**能进 Manifest 的
// 正是被容忍的那些降级**。归因码是稳定枚举（core.RefReason*），错误原文只进日志 ——
// Manifest 参与产物 hash，写进会变的文本就等于放弃确定性构建。

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileMode 本次编译的用途：决定「显式绑定但拿不到」是降级还是失败。
//
// 零值（""）按预览处理：既有调用方（工作台画布、片段渲染、单测直连组件）不传模式，
// 行为必须与改造前一致 —— 只有发布链路显式声明 CompileModePublish。
type CompileMode string

const (
	// CompileModePreview 预览 / 编辑器画布：容忍配置缺失，降级为带归因的占位。
	CompileModePreview CompileMode = "preview"
	// CompileModePublish 发布（构建 / 激活 / 灾难恢复重建）：
	// 显式绑定但拿不到的结构依赖直接失败，绝不产出缺一截的页面。
	CompileModePublish CompileMode = "publish"
)

// DegradeKind 降级来源的种类。
type DegradeKind string

const (
	// DegradeKindBlock 全局块引用（结构槽位的块绑定 / 文档内 core.globalref）。
	DegradeKindBlock DegradeKind = "block"
	// DegradeKindTemplate 结构槽位绑定的结构模板（headerTemplateId 等）。
	DegradeKindTemplate DegradeKind = "template"
)

// Degrade 一次「引用未能展开」的归因记录。
//
// 字段全部来自输入本身（槽位名 / 节点 ID / 绑定 ID）与稳定枚举，因此可确定性地
// 序列化进 Manifest；不含错误原文、不含时间。
type Degrade struct {
	// Slot 结构槽位名（文档内 globalref 为空）。
	Slot string `json:"slot,omitempty"`
	// NodeID 引用节点 ID（文档内节点为文档里写的 ID）。
	NodeID string `json:"nodeId,omitempty"`
	// NodeType 引用节点类型（core.layoutSlot / core.globalref）。
	NodeType string `json:"nodeType,omitempty"`
	// Kind 来源种类：block / template。
	Kind DegradeKind `json:"kind"`
	// RefID 被引用的来源 ID（块 ID / 结构模板 ID）。
	RefID string `json:"refId"`
	// Reason 稳定归因码（core.RefReason* 常量）。
	Reason string `json:"reason"`
}

// dedupeKey 去重键：同一个块被引用多次时，诊断里出现 N 条相同记录只会稀释信息。
func (d Degrade) dedupeKey() string {
	return strings.Join([]string{
		d.Slot, d.NodeID, d.NodeType, string(d.Kind), d.RefID, d.Reason,
	}, "\x00")
}

// DegradeCollector 引用降级归因收集器（去重 + 确定性排序）。
//
// 指针类型且所有方法对 nil 接收者安全：调用方「没有收集器就传 nil」即可，
// 不必在每处判断（与 core.UsageRecorder 同一条路子）。
type DegradeCollector struct {
	items []Degrade
	seen  map[string]bool
}

// NewDegradeCollector 构造收集器。
func NewDegradeCollector() *DegradeCollector {
	return &DegradeCollector{seen: map[string]bool{}}
}

// Record 记录一条诊断（nil 接收者是空操作）。
func (c *DegradeCollector) Record(d Degrade) {
	if c == nil {
		return
	}
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	key := d.dedupeKey()
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.items = append(c.items, d)
}

// RecordRefFailure 把一次渲染期的引用失败记成块诊断。
func (c *DegradeCollector) RecordRefFailure(f core.RefFailure) {
	c.Record(Degrade{
		Slot: f.Slot, NodeID: f.NodeID, NodeType: f.NodeType,
		Kind: DegradeKindBlock, RefID: f.BlockID, Reason: f.Reason,
	})
}

// Items 返回确定性排序后的诊断副本（空集返回 nil，让 Manifest 字段被 omitempty 省略）。
//
// 排序是确定性构建的一部分：Manifest 参与产物 hash，map 遍历或追加顺序的抖动
// 会让同一份输入产出不同字节。
func (c *DegradeCollector) Items() []Degrade {
	if c == nil || len(c.items) == 0 {
		return nil
	}
	out := slices.Clone(c.items)
	slices.SortStableFunc(out, func(a, b Degrade) int {
		return cmp.Or(
			cmp.Compare(a.Slot, b.Slot),
			cmp.Compare(a.NodeID, b.NodeID),
			cmp.Compare(a.NodeType, b.NodeType),
			cmp.Compare(string(a.Kind), string(b.Kind)),
			cmp.Compare(a.RefID, b.RefID),
			cmp.Compare(a.Reason, b.Reason),
		)
	})
	return out
}

// refFailureHandler 把「引用未能展开」的策略收敛到一处。
//
// 判据（本任务唯一的分界）：显式绑定但拿不到 = 失败；拿到了但是空的 = 降级 + 诊断。
// 前者的典型现场是绑定的块被删、块属于别的工程、绑定的结构模板被删 / 非法 ——
// 它们在旧行为下都会静默产出一个占位 div，页面照样发布成功。
func refFailureHandler(cfg *compileConfig) func(core.RefFailure) error {
	return func(f core.RefFailure) error {
		// 无论降级还是失败都先留痕：失败路径的收集结果虽然到不了 Manifest，
		// 但同一次编译里的容忍降级仍要能进 Manifest。
		cfg.degrade.RecordRefFailure(f)
		if cfg.mode != CompileModePublish || f.Reason == core.RefReasonEmpty {
			return nil
		}
		if f.Slot != "" {
			return fmt.Errorf("结构槽位 %s 绑定的全局块 %s 不可用（%s）：发布被拒绝，避免上线缺结构的不完整页面",
				f.Slot, f.BlockID, f.Reason)
		}
		return fmt.Errorf("节点 %s 引用的全局块 %s 不可用（%s）：发布被拒绝，避免上线缺内容的不完整页面",
			f.NodeID, f.BlockID, f.Reason)
	}
}
