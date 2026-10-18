package adminui

// listquery.go — 页面上下文（筛选条件）在「渲染时拼进表单 action」与「提交回来时读回」之间
// 共用的一份实现。
//
// 两处若各写一遍，分叉的症状是「改完筛选再操作，回来静默丢了筛选」—— 页面不报错、日志也干净。
// 所以键表由各页自己声明（每页一份），取读实现只有这一份。

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// ListQuery 本次请求 query 里**白名单内**的页面上下文（空值丢弃、键有序）。
//
// keys 是那一页的筛选键表：渲染时把它拼进表单 action，提交回来时按同一份表读回。
// 不在表里的键一律不透传 —— 页面表单不该把无关参数（例如 source=bundle）带回服务端。
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

// WithListQuery 把页面上下文拼到地址后面（空上下文原样返回，不留悬空的 ?）。
func WithListQuery(action, listQuery string) string {
	if strings.TrimSpace(listQuery) == "" {
		return action
	}
	if strings.Contains(action, "?") {
		return action + "&" + listQuery
	}
	return action + "?" + listQuery
}
