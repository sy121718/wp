package mediaenums

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
)

const (
	MsgSuccess    = "MsgSuccess"    // 操作成功
	MsgBadRequest = "MsgBadRequest" // 请求参数错误
)
