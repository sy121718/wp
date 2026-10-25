package inventorycontract

// attachment_resolver.go — 附件读口（调拨凭证的唯一跨模块依赖，迁移 630，W3）。
//
// 为什么需要一个窄接口而不是直接 import media 的 service / model：
// AGENTS.md 的模块边界判据是「跨模块只走 contract」—— 库存域需要回答两个问题
//（「这个附件 id 存不存在」「它长什么样」），而 media 的 MediaService 有二十来个方法
//（上传 / 列表 / 删除 / 换图 / 分类树 / 引用同步…），库存一条都用不上。
// 依赖面越大越容易在不经意间用上不该用的能力；测试造替身时二十个方法的空实现
// 也会把测试意图淹掉（与 order 侧 StockOperator 的收窄理由同源）。
//
// 实现放在库存侧的出站包（internal/module/inventory/outbound/mediaatt），
// 依赖方向是「实现方依赖调用方契约」—— 与 orderstock 同一手法。

import "context"

// AttachmentResolver 附件存在性校验 + 展示信息（唯一读口）。
type AttachmentResolver interface {
	// ResolveAttachment 不存在时返回 ErrAttachmentNotFound。
	ResolveAttachment(ctx context.Context, projectID string, id int64) (*AttachmentBrief, error)
	// ResolveAttachments 批量；顺序与入参一致，缺项返回 nil 占位（调用方决定打回还是跳过）。
	//
	// 批量而不是逐个：一张调拨单的凭证可能有五六张，逐个查就是五六次往返，
	// 而「哪一张不存在」必须报得出来（缺项占位，不是静默丢弃）。
	ResolveAttachments(ctx context.Context, projectID string, ids []int64) ([]*AttachmentBrief, error)
}

// AttachmentBrief 附件的展示信息（只读、不可变）。
//
// 只给「人看得出来是什么」的四项 + URL：库存域不关心附件在哪个分类、有没有变体、
// 谁传的 —— 那些在 media 页面上看。
type AttachmentBrief struct {
	ID       int64
	FileName string
	FileSize int64
	MimeType string
	URL      string
}

// ErrAttachmentNotFound 附件不存在（或不属于本工程）。
//
// 导出成常量而不是让调用方比对错误文本：库存侧的 service 要在 store 之前校验
// 「凭证真的存在」，而「不存在」是一个可预期的业务结论（404），不是基础设施故障。
const ErrAttachmentNotFound = "ErrAttachmentNotFound"
