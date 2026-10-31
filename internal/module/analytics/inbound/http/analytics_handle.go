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
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

const maxCollectBody = 64 << 10

type Handle struct {
	svc analyticscontract.AnalyticsService
}

func NewHandle(svc analyticscontract.AnalyticsService) *Handle {
	return &Handle{svc: svc}
}

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

func (h *Handle) Summary(c *gin.Context) {
	req, ok := binding.Query[analyticsdto.SummaryReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Summary(c.Request.Context(), req)
	if err != nil {
		analyticsError(c, err)
		return
	}
	response.Success(c, res)
}

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
