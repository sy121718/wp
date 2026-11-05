package blueprintenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	BlueprintCreate = "blueprint:create"
	BlueprintDelete = "blueprint:delete"
	BlueprintGet = "blueprint:get"
	BlueprintInit = "blueprint:init"
	BlueprintList = "blueprint:list"
	BlueprintPublish = "blueprint:publish"
	BlueprintUpdate = "blueprint:update"
)
