package aienums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	AIChat = "ai:chat"
	AIProviderDelete = "ai:provider_delete"
	AIProviderGet = "ai:provider_get"
	AIProviderList = "ai:provider_list"
	AIProviderModelsAppend = "ai:provider_models_append"
	AIProviderModelsCandidates = "ai:provider_models_candidates"
	AIProviderModelsFetch = "ai:provider_models_fetch"
	AIProviderModelsList = "ai:provider_models_list"
	AIProviderModelsRestore = "ai:provider_models_restore"
	AIProviderModelsSave = "ai:provider_models_save"
	AIProviderSave = "ai:provider_save"
	AIProviderStatus = "ai:provider_status"
	AISessionAppend = "ai:session_append"
	AISessionArchive = "ai:session_archive"
	AISessionEvents = "ai:session_events"
	AISessionFold = "ai:session_fold"
	AISessionFoldPlan = "ai:session_fold_plan"
	AISessionGet = "ai:session_get"
	AISessionList = "ai:session_list"
	AISessionRename = "ai:session_rename"
	AISessionSend = "ai:session_send"
	AITokenCreate = "ai:token_create"
	AITokenList = "ai:token_list"
	AITokenRevoke = "ai:token_revoke"
	AITokenToggle = "ai:token_toggle"
)
