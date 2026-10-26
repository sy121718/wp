package binding

// context.go — 请求上下文里与「参数」同源的两样东西：列表页的筛选上下文（list query）
// 与表单隐藏域要的 CSRF token。
//
// 为什么归本包：它们都是「控制器读请求」的一部分，跟渲染无关。放在 adminui 里时，
// 一个只想读筛选串的控制器就得 import 整个后台壳（adminui 连带导航 / 权限码 / 页壳）。
//
// 一处实现两处用：列表页渲染表单 action 时读一次，片段提交回来时按同一份键表读回 ——
// 各写一遍的分叉症状是「改完筛选再操作，回来静默丢了筛选」，页面不报错、日志也干净。

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
)

// ListQuery 本次请求 query 里**白名单内**的页面上下文（空值丢弃、键有序）。
//
// keys 是那一页的筛选键表：渲染时把它拼进表单 action，提交回来时按同一份表读回。
// 不在表里的键一律不透传 —— 页面表单不该把无关参数（例如 source=xxx）带回服务端。
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

// CSRFToken 当前请求的 CSRF token；取不到时返回空串（提交会被中间件拒，与其它后台页一致）。
//
// 给「只需要一个 token」的片段数据（抽屉表单的隐藏域）用：那些地方不该为了一个 token
// 去装配整份页壳。
func CSRFToken(c *gin.Context) string {
	if tok, err := builtin.GetCSRFToken(c); err == nil {
		return tok
	}
	return ""
}
