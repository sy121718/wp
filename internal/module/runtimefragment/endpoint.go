package runtimefragment

// endpoint.go — 片段端点（0-D，docs/04 §1.2）。
// GET /_fragments/{type}：白名单校验 → 认证策略 → 参数限制 → 处理器 → HTML 片段。
// 安全边界：不读 Page Document、不执行 Jet、不接受任意 endpoint（type 必须在
// Registry 内）；参数长度/枚举受限；handler 输出经统一 escape 边界。

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/auth"
)

// maxParamLen 查询参数值长度上限（防超长注入）。
const maxParamLen = 200

// maxParamCount 查询参数数量上限。
const maxParamCount = 10

// FragmentEndpoint 片段端点 handler。
func FragmentEndpoint(c *gin.Context) {
	typeName := c.Param("type")
	spec, ok := Lookup(typeName)
	if !ok {
		// 未知能力：白名单拒绝（不接受任意 endpoint）。
		c.String(http.StatusNotFound, "片段能力不存在")
		return
	}
	// 认证策略。
	userID := ""
	if spec.Auth == AuthSession {
		cs, err := auth.GetCookieSession(c)
		if err != nil || cs == nil {
			c.String(http.StatusUnauthorized, "需要登录")
			return
		}
		userID = strconv.FormatUint(cs.UserID, 10)
	}
	// 参数白名单限制（长度/数量/上下文枚举）。
	params := map[string]string{}
	query := c.Request.URL.Query()
	if len(query) > maxParamCount {
		c.String(http.StatusBadRequest, "参数过多")
		return
	}
	for k, vs := range query {
		if len(vs) == 0 {
			continue
		}
		if len(k) > 64 || len(vs[0]) > maxParamLen {
			c.String(http.StatusBadRequest, "参数非法")
			return
		}
		params[k] = vs[0]
	}
	req := &Request{
		Type:    typeName,
		Context: params["context"],
		Params:  params,
		UserID:  userID,
	}
	if err := validateContext(req.Context); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	// 处理器：返回 HTML 片段（handler 内部对用户数据 escape）。
	htmlFragment, err := spec.Render(c.Request.Context(), req)
	if err != nil {
		c.String(http.StatusInternalServerError, "片段渲染失败")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(strings.TrimSpace(htmlFragment)))
}
