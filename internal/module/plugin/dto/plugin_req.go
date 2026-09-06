// Package plugindto 插件模块请求/响应结构。
package plugindto

// ToggleReq 启停插件。
type ToggleReq struct {
	ID      string `form:"id" binding:"required"`
	Enabled bool   `form:"enabled"`
}

// UninstallReq 卸载插件。
type UninstallReq struct {
	ID string `form:"id" binding:"required"`
}

// DetailReq 插件详情。
type DetailReq struct {
	ID string `form:"id" binding:"required"`
}
