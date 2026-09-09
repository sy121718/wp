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
)

const (
	MsgSuccess    = "MsgSuccess"    // 操作成功
	MsgBadRequest = "MsgBadRequest" // 请求参数错误
)
