// Package blockcontract 定义 block 模块对外契约。
package blockcontract

// block_usage.go — 全局块「有效源码引用」的只读形状（审计 ARCH-02）。
//
// 为什么形状定义在 block 契约而不是各模块各写一份：删除保护要在装配层把
// Page / Presentation / ContentTemplate / Block 四条只读面合并成**一份**判据，
// 若每个模块返回自己的结构，装配层就要写四段适配 —— 适配一多，新增一类引用
// 就只会在漏改的那一段上静默放行（ARCH-02 的成因正是「判据只认识页面与主题」）。
//
// 为什么只描述「源码引用」：历史 artifact 是不可变的编译产物，它的保留由 GC 策略
// 决定，不该永久阻断源码删除（任务验收口径）。本形状因此**不包含**产物行 ——
// 块删除只看现存的、未删除的可编辑源码。
//
// 依赖方向（AGENTS.md「命名约束」）：本文件只被其他模块的 contract 引用，
// 不反向 import 任何模块的 service / model；page 与 presentation 的 service
// 本来就已 import 本包，因此这个形状不引入新的模块依赖。

import (
	"errors"
	"fmt"
	"strings"
)

// BlockUsageKind 引用的归属类别。
type BlockUsageKind string

const (
	// UsageKindPageDocument 页面草稿文档树内的 core.globalref（props.blockId，任意深度）。
	UsageKindPageDocument BlockUsageKind = "page_document"
	// UsageKindPageStructure 页面 settings.structure 的页眉/页脚自选绑定与其余槽位（slots）。
	UsageKindPageStructure BlockUsageKind = "page_structure"
	// UsageKindThemeSlot 主题 settings.structure 的页眉/页脚槽位绑定（工程级默认）。
	UsageKindThemeSlot BlockUsageKind = "theme_slot"
	// UsageKindBlockDocument 其它全局块文档树内的嵌套引用（块引用块）。
	UsageKindBlockDocument BlockUsageKind = "block_document"
	// UsageKindPageRevision 页面历史修订（page_revisions.draft_document）里的引用。
	//
	// 与 UsageKindPageDocument 分开而不是合并进它：当前草稿与历史修订的**处置方式不同**
	//（改页面 vs 回滚修订），而且修订有保留期兜底（90 天 / 每页最近 20 个），
	// 提示里说清是哪一版，操作者才知道该不该为它让路。
	UsageKindPageRevision BlockUsageKind = "page_revision"
	// UsageKindContentTemplate 内容模板的草稿文档或任一历史版本文档。
	UsageKindContentTemplate BlockUsageKind = "content_template"
	// UsageKindPresentationInstance 自动发布实例的覆盖文档或任一文档快照。
	UsageKindPresentationInstance BlockUsageKind = "presentation_instance"
)

// BlockUsage 一条有效源码引用（可定位到具体实体）。
//
// ProjectID 是**引用方**的工程归属（不是被引用块的工程）：删除保护的提示要能告诉
// 操作者「去哪个站点、哪个页面解除引用」，跨工程引用（同一块不会被两个工程共用，
// 但块的 id 是全局 uuid）时这条信息是唯一线索。
type BlockUsage struct {
	// Kind 引用类别（决定页面提示用哪条词条，也决定修复路径）。
	Kind BlockUsageKind
	// ProjectID 引用方所属工程 id。
	ProjectID string
	// EntityID 引用方实体 id（页面 / 主题 / 块 / 模板 / 实例）。
	EntityID string
	// Label 人可读的定位（页面路径、模板名、实例 URL 路径、块名…）。
	Label string
	// Detail 补充定位（命中的槽位名、模板版本号等），可为空。
	Detail string
}

// String 单行定位表示：类别 + 实体定位（结构化日志与错误原文用）。
//
// 页面文案由 inbound 层按当前语言渲染（那里才拿得到取词器）；这里只保证
// 「日志与 JSON 接口能看出是哪一类、哪一个实体」，不掺任何需要翻译的措辞。
func (u BlockUsage) String() string {
	var b strings.Builder
	b.WriteString(string(u.Kind))
	if label := strings.TrimSpace(u.Label); label != "" {
		b.WriteString(" ")
		b.WriteString(label)
	} else if id := strings.TrimSpace(u.EntityID); id != "" {
		b.WriteString(" ")
		b.WriteString(id)
	}
	if detail := strings.TrimSpace(u.Detail); detail != "" {
		b.WriteString("(")
		b.WriteString(detail)
		b.WriteString(")")
	}
	return b.String()
}

// BlockInUseError 「块仍被源码引用」的拒绝错误：携带全部命中明细。
//
// 为什么不是裸 sentinel：拒绝必须可定位（哪一类引用、哪些实体），否则操作者只能
// 从「失败了」出发自己猜是哪个页面 —— 而站点的块引用散在文档 JSONB 里的任意深度，
// 人眼根本找不到。明细同时进日志与页面提示。
//
// 兼容性：Unwrap 回 ErrBlockInUse，因此既有的 errors.Is(err, ErrBlockInUse)
// 判定（HTTP 状态码 409、页面文案白名单）全部照旧成立。
type BlockInUseError struct {
	Usages []BlockUsage
}

// Error 实现 error：sentinel 文案 + 逐条定位（不带翻译，日志与 JSON 接口共用）。
func (e *BlockInUseError) Error() string {
	if e == nil || len(e.Usages) == 0 {
		return ErrBlockInUse.Error()
	}
	parts := make([]string, 0, len(e.Usages))
	for _, u := range e.Usages {
		parts = append(parts, u.String())
	}
	return fmt.Sprintf("%s: %s", ErrBlockInUse.Error(), strings.Join(parts, "; "))
}

// Unwrap 让 errors.Is(err, ErrBlockInUse) 继续成立（HTTP 状态映射与文案白名单依赖它）。
func (e *BlockInUseError) Unwrap() error { return ErrBlockInUse }

// NewBlockInUseError 构造拒绝错误：无明细时退化为裸 sentinel
// （调用方拿不到引用清单时仍要拒绝，只是提示退回到「不能删」）。
func NewBlockInUseError(usages []BlockUsage) error {
	if len(usages) == 0 {
		return ErrBlockInUse
	}
	return &BlockInUseError{Usages: usages}
}

// BlockUsages 从错误里取回引用明细（非本类型或没有明细时返回 nil）。
//
// 走标准库 errors.As：包装链的展开规则只有那一处实现，手写一层遍历迟早与它分叉
// （例如忘了 fmt.Errorf 的 %w 语义）。
func BlockUsages(err error) []BlockUsage {
	var inUse *BlockInUseError
	if !errors.As(err, &inUse) {
		return nil
	}
	return inUse.Usages
}
