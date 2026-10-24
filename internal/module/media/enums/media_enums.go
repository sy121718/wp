package mediaenums

import "go_wp/pkg/facing"

const (
	ErrAttachmentNotFound = "ErrAttachmentNotFound" // 附件不存在
	ErrUploadFailed       = "ErrUploadFailed"       // 文件上传失败
	ErrUploadEmpty        = "ErrUploadEmpty"        // 上传文件不能为空
	// 变体与打包下载（048 媒体变体改造）。
	ErrAttachmentNotImage      = "ErrAttachmentNotImage"      // 该附件不支持生成变体
	ErrVariantStorageNotLocal  = "ErrVariantStorageNotLocal"  // 仅本地存储支持该操作
	ErrDownloadFailed          = "ErrDownloadFailed"          // 打包下载失败
	ErrDownloadEmpty           = "ErrDownloadEmpty"           // 请选择要下载的附件
	ErrDownloadStorageNotLocal = "ErrDownloadStorageNotLocal" // 仅本地存储支持打包下载
	// 媒体中心（02-B，迁移 067）：稳定引用 / 换图 / 引用保护。
	ErrReplaceFailed        = "ErrReplaceFailed"        // 换图失败
	ErrReplaceExtMismatch   = "ErrReplaceExtMismatch"   // 换图需保持扩展名一致（URL 稳定引用的前提）
	ErrAttachmentReferenced = "ErrAttachmentReferenced" // 附件被页面引用，删除被拒绝
	// ErrReplaceStaleNotifyFailed 换图内容已替换，但引用方（页面 / 实例）没能标记为待重建。
	//
	// 与 ErrReplaceFailed 分开单列：前者说的是「内容没换上去」，本项说的是「换上了，
	// 但引用它的页面不会更新」—— 处置动作不同（前者重传是为了落盘，后者重传是为了
	// 重发失效通知，内容替换本身已经幂等），压成同一句话会让操作者不知该做什么。
	ErrReplaceStaleNotifyFailed = "ErrReplaceStaleNotifyFailed"
)

const (
	MsgSuccess    = "MsgSuccess"    // 操作成功
	MsgBadRequest = "MsgBadRequest" // 请求参数错误
)

// —— 点分 key 常量（新式）——
//
// 值是 sys_i18n 的 item_key（文案真源在迁移 451），命名按「去掉模块子域前缀
// （`admin.media.`）后的语义路径」：包名 mediaenums 已给出模块上下文。
//
// 与上面那批 `MsgXxx = "MsgXxx"` / `ErrXxx = "ErrXxx"` 分开成组：老式形态的值就是
// 常量名本身（`sys_i18n` 里存同名 key），两者混在同一前缀下会让人以为值也是常量名。
// 中文兜底留在调用点（词条缺失时的回落），不在这里。
const (
	// 媒体资源包（zip）的下载计划文案：README 会随包落到用户机器上，是导出产物的一部分。
	PackageTitle              = "admin.media.package.title"              // go_wp 媒体资源包
	PackageDir                = "admin.media.package.dir"                // 目录：{name}
	PackageVariantType        = "admin.media.package.variantType"        // 内容：{name} 变体
	PackageStatus             = "admin.media.package.status"             // 状态：{name}
	PackageVariantUnavailable = "admin.media.package.variantUnavailable" // 说明：该变体文件当前不可用，原图见 original/ 目录。
	PackageRegenHint          = "admin.media.package.regenHint"          // 可在媒体库详情页点「重新生成变体」，生成完成后重新下载。
	// PackageOriginalPathInvalid / PackageOriginalMissing 降级为 README 说明时的正文。
	PackageOriginalPathInvalid = "admin.media.package.originalPathInvalid" // 原图存储路径非法，无法打包。
	PackageOriginalMissing     = "admin.media.package.originalMissing"     // 原图物理文件已缺失，仅剩元数据。
	// PackageReadFailed zip 生成过程中读文件失败（流式响应里以文本条目回执）。
	PackageReadFailed = "admin.media.package.readFailed" // 文件读取失败： {name}

	// VariantStatusMissing 变体状态文案的兜底（状态值不在已知集合里）。
	VariantStatusMissing = "admin.media.variantStatus.missing" // 无变体记录（missing）

	// Heading 媒体库页标题（页头与回跳链接文字共用同一条词条）。
	Heading = "admin.media.heading" // 媒体库
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrAttachmentNotFound` 这样的裸 key
// （这批常量的值就是常量名本身，旧式形态）。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrAttachmentNotFound:       "附件不存在",
	ErrUploadFailed:             "文件上传失败",
	ErrUploadEmpty:              "上传文件不能为空",
	ErrAttachmentNotImage:       "该附件不支持生成变体",
	ErrVariantStorageNotLocal:   "仅本地存储支持该操作",
	ErrDownloadFailed:           "打包下载失败",
	ErrDownloadEmpty:            "请选择要下载的附件",
	ErrDownloadStorageNotLocal:  "仅本地存储支持打包下载",
	ErrReplaceFailed:            "换图失败，请稍后重试",
	ErrReplaceExtMismatch:       "换图时扩展名必须与原文件一致，否则已引用该地址的页面会失效",
	ErrAttachmentReferenced:     "该附件仍被页面引用，不能删除；请先解除引用",
	ErrReplaceStaleNotifyFailed: "附件已替换，但通知页面重建失败：请手动重建引用它的页面",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
