package mediaenums

import "go_wp/pkg/facing"

const (
	ErrAttachmentNotFound = "ErrAttachmentNotFound"
	ErrUploadFailed       = "ErrUploadFailed"
	ErrUploadEmpty        = "ErrUploadEmpty"

	ErrAttachmentNotImage      = "ErrAttachmentNotImage"
	ErrVariantStorageNotLocal  = "ErrVariantStorageNotLocal"
	ErrDownloadFailed          = "ErrDownloadFailed"
	ErrDownloadEmpty           = "ErrDownloadEmpty"
	ErrDownloadStorageNotLocal = "ErrDownloadStorageNotLocal"

	ErrReplaceFailed        = "ErrReplaceFailed"
	ErrReplaceExtMismatch   = "ErrReplaceExtMismatch"
	ErrAttachmentReferenced = "ErrAttachmentReferenced"

	ErrReplaceStaleNotifyFailed = "ErrReplaceStaleNotifyFailed"
)

const (
	MsgSuccess    = "MsgSuccess"
	MsgBadRequest = "MsgBadRequest"
)

const (
	PackageTitle              = "admin.media.package.title"
	PackageDir                = "admin.media.package.dir"
	PackageVariantType        = "admin.media.package.variantType"
	PackageStatus             = "admin.media.package.status"
	PackageVariantUnavailable = "admin.media.package.variantUnavailable"
	PackageRegenHint          = "admin.media.package.regenHint"

	PackageOriginalPathInvalid = "admin.media.package.originalPathInvalid"
	PackageOriginalMissing     = "admin.media.package.originalMissing"

	PackageReadFailed = "admin.media.package.readFailed"

	VariantStatusMissing = "admin.media.variantStatus.missing"

	Heading = "admin.media.heading"
)

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

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
