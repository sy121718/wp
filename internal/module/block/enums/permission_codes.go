package blockenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	BlockBulkDelete = "block:bulk_delete"
	BlockClone = "block:clone"
	BlockCreate = "block:create"
	BlockDelete = "block:delete"
	BlockDetail = "block:detail"
	BlockList = "block:list"
	BlockUpdate = "block:update"
)
