// Package analyticshttp analytics 模块 HTTP 接入层。
//
// 两个出口，鉴权模型截然不同：
//   - POST /analytics/collect 是**公开路由**（访问面）：访客的浏览器直接打点，
//     没有会话也没有 CSRF token。越权防护靠**接口形状** —— 它只能「写一条浏览记录」，
//     拿不到任何查询 / 删除能力（契约里也只有打点与只读聚合两组）。
//   - GET /api/analytics/summary 是**后台只读接口**，挂 /api 三层链
//     （Session + CSRF + Casbin），权限点 analytics:view。
package analyticshttp

import (
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticsservice "go_wp/internal/module/analytics/service"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// maxCollectBody 打点报文大小上限。
//
// 打点是**未认证输入**：不限长就等于把内存交给任何能访问这个路径的人。
// 取值与 cart 的支付回调同口径（64 KiB）—— 一次页面浏览只需要几百字节。
const maxCollectBody = 64 << 10

// Handle analytics 模块 HTTP 处理器。
type Handle struct {
	svc analyticscontract.AnalyticsService
}

// NewHandle 创建处理器。
func NewHandle(svc analyticscontract.AnalyticsService) *Handle {
	return &Handle{svc: svc}
}

// Collect 页面浏览打点（POST /analytics/collect，公开路由）。
//
// 一律回 204 空响应 —— 这是**静默失败**的对外形态：
//   - 坏请求回 204：返回差异会向外部暴露「这个工程存在吗 / 这个形状会被接受吗」，
//     而访客本来也不该因为打点看到任何东西；
//   - 写库失败也只记日志：统计是增强能力，压不进访客的页面加载路径。
func (h *Handle) Collect(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCollectBody))
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	values, perr := url.ParseQuery(string(raw))
	if perr != nil {
		c.Status(http.StatusNoContent)
		return
	}
	// IP 与 UA 由服务端从连接与请求头取，客户端无法伪造这两项
	//（上报体里只有它自己知道的匿名标识与路径）。
	_ = h.svc.Collect(c.Request.Context(), &analyticsdto.CollectReq{
		ProjectID: values.Get("projectId"),
		Path:      values.Get("path"),
		Lang:      values.Get("lang"),
		Referrer:  values.Get("referrer"),
		Session:   values.Get("session"),
		Visitor:   values.Get("visitor"),
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
}

// Summary 访问统计聚合查询（GET /api/analytics/summary，后台只读）。
func (h *Handle) Summary(c *gin.Context) {
	var req analyticsdto.SummaryReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, analyticsenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Summary(c.Request.Context(), &req)
	if err != nil {
		analyticsError(c, err)
		return
	}
	response.Success(c, res)
}

// analyticsError 把统计业务错误映射为响应状态码与文案。
//
// 与 project 模块同口径：业务哨兵给具体原因，其余（基础设施故障）给兜底文案
// 并把原文写进日志 —— 不向客户端泄漏内部错误细节。
func analyticsError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, analyticsenums.ErrAnalyticsInternal
	switch {
	case errors.Is(err, analyticsservice.ErrInvalidParam), errors.Is(err, analyticsservice.ErrInvalidRange):
		status, message = http.StatusBadRequest, err.Error()
	default:
		logger.Scene("analytics").Error(err, "访问统计查询失败")
	}
	response.ErrorWithMessage(c, status, message)
}
