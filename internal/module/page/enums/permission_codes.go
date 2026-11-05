package pageenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	PageArtifactGc = "page:artifact_gc"
	PageArtifactRebuild = "page:artifact_rebuild"
	PageBuild = "page:build"
	PageCreate = "page:create"
	PageDelete = "page:delete"
	PageDetail = "page:detail"
	PageDraftSave = "page:draft_save"
	PageList = "page:list"
	PagePublicationAudit = "page:publication_audit"
	PagePublish = "page:publish"
	PageRedirectCreate = "page:redirect_create"
	PageRedirectDelete = "page:redirect_delete"
	PageRedirectMerge = "page:redirect_merge"
	PageRedirectView = "page:redirect_view"
	PageRevisionList = "page:revision_list"
	PageRollback = "page:rollback"
	PageSEOPatrol = "page:seo_patrol"
	PageTranslationsSave = "page:translations_save"
	PageURLUpdate = "page:url_update"
)
