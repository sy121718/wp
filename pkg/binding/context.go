package binding

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func ListQuery(c *gin.Context, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	in := c.Request.URL.Query()
	out := url.Values{}
	for _, k := range keys {
		if v := strings.TrimSpace(in.Get(k)); v != "" {
			out.Set(k, v)
		}
	}
	return out.Encode()
}

func WithListQuery(action, listQuery string) string {
	if strings.TrimSpace(listQuery) == "" {
		return action
	}
	if strings.Contains(action, "?") {
		return action + "&" + listQuery
	}
	return action + "?" + listQuery
}
