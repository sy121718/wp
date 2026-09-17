package shell

// errors.go — 后台页面的统一错误出口（审计 CQ-009）。
//
// 背景：dashboard 里曾散落 30 余处 c.String(500, err.Error())。它有两个问题，
// 都不在「不好看」这一层：
//
//  1. **信息泄露**：内部错误会带出表名、SQL 片段、文件路径、依赖服务地址。
//     后台不是可信边界（运营、外包、被钓鱼的账号都在里面）。
//  2. **反馈不可行动**：把 Go 的错误字符串直接铺在页面上，运营既看不懂，
//     也无从判断该重试还是找人 —— 而真正的排查信息应该进日志（带请求上下文）。
//
// 所以出口只做一件事：详情进日志，对外给通用提示。
//
// 业务错误（可直接展示给用户的那些）不走这里 —— 它们由 service 层经各自模块的 enums
// 返回，由调用方显式选择展示方式。**不在这里做「是不是业务错误」的猜测**：
// 猜错的代价是「有时泄露有时不泄露」，而那种不规律恰恰最难被发现。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// MsgInternalError 是页面级内部错误提示的 i18n key。
//
// i18n key 是字符串协议：模板与词条表都按字面量取值，各页面模块自带同名常量即可，
// 不必让模块 enums 依赖 web 层（dashboard 模块已随页面回迁删除，壳包是唯一实现方）。
const MsgInternalError = "MsgInternalError"

// PageError 内部错误出口：记日志 + 对外通用提示（HTTP 500）。
func PageError(c *gin.Context, scene string, err error) {
	if err != nil {
		logger.Scene(scene).With("path", c.Request.URL.Path).Error(err, "后台页面处理失败")
	}
	response.ErrorWithMessage(c, http.StatusInternalServerError, MsgInternalError)
}

// PageErrorBadRequest 参数错误出口：同样不直出原文（参数错误也可能带出内部结构），
// 但状态码用 400，前端脚本能据此区分「你填错了」与「系统出问题了」。
func PageErrorBadRequest(c *gin.Context, scene string, err error) {
	if err != nil {
		logger.Scene(scene).With("path", c.Request.URL.Path).Error(err, "后台页面参数校验失败")
	}
	response.ErrorWithMessage(c, http.StatusBadRequest, MsgInternalError)
}

// AdminWriteFailed 管理页写操作失败的统一响应：详情进日志，对外给通用提示。
func AdminWriteFailed(c *gin.Context, err error) {
	if err == nil {
		return
	}
	logger.Scene("admin-page").With("path", c.Request.URL.Path).Error(err, "管理页写操作失败")
	PageErrorBadRequest(c, "admin", err)
}
