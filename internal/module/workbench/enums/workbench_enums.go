// Package workbenchenums 工作台模块响应消息（哨兵串经后台 i18n 词表翻译）。
package workbenchenums

// MsgDashboardTitle 仪表盘页面标题。
const MsgDashboardTitle = "MsgDashboardTitle" // 仪表盘

// MsgInternalError 页面 handler 内部错误统一提示（禁止直出 err.Error() 泄露内部细节）。
const MsgInternalError = "MsgInternalError" // 系统内部错误，请稍后重试

// MsgCompileFailed 预览编译失败统一提示（编译器内部错误不外泄，仅提示用户检查配置）。
const MsgCompileFailed = "MsgCompileFailed" // 预览编译失败
