package mediaenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	MediaCategoryCreate = "media:category_create"
	MediaCategoryDelete = "media:category_delete"
	MediaCategoryTree = "media:category_tree"
	MediaCategoryUpdate = "media:category_update"
	MediaDelete = "media:delete"
	MediaDetail = "media:detail"
	MediaDownload = "media:download"
	MediaDownloadBatch = "media:download_batch"
	MediaList = "media:list"
	MediaReferences = "media:references"
	MediaReplace = "media:replace"
	MediaUpdate = "media:update"
	MediaUpload = "media:upload"
	MediaVariantsGenerate = "media:variants_generate"
)
