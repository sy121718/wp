package sysconfigenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	SysConfigGet = "sysconfig:get"
	SysConfigSave = "sysconfig:save"
	SysConfigSystemSave = "sysconfig:system_save"
)
