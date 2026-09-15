package dashboardhttp

// page_error.go — 后台页面的统一错误出口（审计 CQ-009）。
//
// 背景：dashboard 里曾散落 30 余处 `c.String(500, err.Error())`。它有两个问题，
// 都不在「不好看」这一层：
//
//  1. **信息泄露**：内部错误会带出表名、SQL 片段、文件路径、依赖服务地址。
//     后台不是可信边界（运营、外包、被钓鱼的账号都在里面）。
//  2. **反馈不可行动**：把 Go 的错误字符串直接铺在页面上，运营既看不懂，
//     也无从判断该重试还是找人 —— 而真正的排查信息应该进日志（带请求上下文）。
//
// 所以出口只做一件事：详情进日志，对外给 dashboardenums 里的通用提示。
//
// 业务错误（可直接展示给用户的那些）不走这里 —— 它们由 service 层经 enums 返回，
// 由调用方显式选择展示方式。**不在这里做「是不是业务错误」的猜测**：
// 猜错的代价是「有时泄露有时不泄露」，而那种不规律恰恰最难被发现。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// pageError 内部错误出口：记日志 + 对外通用提示（HTTP 500）。
func pageError(c *gin.Context, scene string, err error) {
	if err != nil {
		logger.Scene(scene).With("path", c.Request.URL.Path).Error(err, "后台页面处理失败")
	}
	response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
}

// pageErrorBadRequest 参数错误出口：同样不直出原文（参数错误也可能带出内部结构），
// 但状态码用 400，前端脚本能据此区分「你填错了」与「系统出问题了」。
func pageErrorBadRequest(c *gin.Context, scene string, err error) {
	if err != nil {
		logger.Scene(scene).With("path", c.Request.URL.Path).Error(err, "后台页面参数校验失败")
	}
	response.ErrorWithMessage(c, http.StatusBadRequest, dashboardenums.MsgInternalError)
}
