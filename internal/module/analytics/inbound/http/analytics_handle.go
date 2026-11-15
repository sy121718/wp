package analyticshttp

import (
	"io"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

const maxCollectBody = 64 << 10

const analyticsErrScene = "analytics"

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
	req, err := binding.ParseQuery[analyticsdto.SummaryReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Summary(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, analyticsErrScene, err)
		return
	}
	response.Success(c, res)
}
