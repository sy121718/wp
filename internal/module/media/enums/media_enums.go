package mediaenums

const (
	ErrAttachmentNotFound = "附件不存在"
	ErrUploadFailed       = "文件上传失败"
	ErrUploadEmpty        = "上传文件不能为空"
	// 变体与打包下载（048 媒体变体改造）。
	ErrAttachmentNotImage      = "该附件不支持生成变体"
	ErrVariantStorageNotLocal  = "仅本地存储支持该操作"
	ErrDownloadFailed          = "打包下载失败"
	ErrDownloadEmpty           = "请选择要下载的附件"
	ErrDownloadStorageNotLocal = "仅本地存储支持打包下载"
)

const (
	MsgSuccess    = "操作成功"
	MsgBadRequest = "请求参数错误"
)
