package pluginenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	PluginDetail = "plugin:detail"
	PluginInstall = "plugin:install"
	PluginList = "plugin:list"
	PluginToggle = "plugin:toggle"
	PluginUninstall = "plugin:uninstall"
)
