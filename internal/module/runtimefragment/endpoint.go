package runtimefragment

// endpoint.go — 片段端点（0-D，docs/04 §1.2）。
// GET/POST /_fragments/{type}：白名单校验 → 认证策略 → 参数限制 → 处理器 → HTML 片段。
// 安全边界：不读 Page Document、不执行 Jet、不接受任意 endpoint（type 必须在
// Registry 内）；参数长度/枚举受限；handler 输出经统一 escape 边界。
//
// issue #20 起支持 POST：结构化入参（表单里的并行数组）用 GET 的 query 传既受长度
// 限制也不合语义。POST 片段仍然是**无副作用**的（bundleConfiguratorCheck 是纯计算校验，
// 不写库、不预占库存），因此 anonymous 策略下不要求 CSRF；一旦某个 POST 片段真的产生
// 状态变更（购物车 / 下单），它必须改用 session 策略并带 CSRF —— 见 Spec.Method 的注释。

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/auth"
)

// maxParamLen 查询参数值长度上限（防超长注入）。
const maxParamLen = 200

// maxParamCount GET 的参数名数量上限（query 是扁平结构，几个键就够）。
const maxParamCount = 10

// maxFormParamCount POST 的参数名数量上限：表单用「并行数组」表达结构化入参
// （variantId 出现 N 次 + qty 出现 N 次），捆绑选项上限 20 时约 42 个键。
const maxFormParamCount = 64

// FragmentEndpoint 片段端点 handler。
func FragmentEndpoint(c *gin.Context) {
	typeName := c.Param("type")
	spec, ok := Lookup(typeName)
	if !ok {
		// 未知能力：白名单拒绝（不接受任意 endpoint）。
		c.String(http.StatusNotFound, "片段能力不存在")
		return
	}
	// 方法与能力声明必须一致：GET 能力不接受 POST 调用（反之亦然）。
	if spec.Method != "" && !strings.EqualFold(spec.Method, c.Request.Method) {
		c.String(http.StatusMethodNotAllowed, "片段能力不支持该请求方法")
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
	params, values, perr := collectFragmentParams(c)
	if perr != nil {
		c.String(http.StatusBadRequest, perr.Error())
		return
	}
	req := &Request{
		Type:    typeName,
		Context: params["context"],
		Params:  params,
		Values:  values,
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

// collectFragmentParams 收集并校验入参：GET 走 query，POST 走表单。
//
// 同时给出「首值」与「全部值」两份视图：前者服务单值参数（productId / context），
// 后者服务并行数组（variantId / qty）—— 只取首值会把多选项静默截成一选项，
// 那正好是最难发现的错误（前台看起来「只算了一件」）。
func collectFragmentParams(c *gin.Context) (params map[string]string, values map[string][]string, err error) {
	params = map[string]string{}
	values = map[string][]string{}
	var source url.Values
	if c.Request.Method == http.MethodPost {
		if perr := c.Request.ParseForm(); perr != nil {
			return nil, nil, errors.New("表单解析失败")
		}
		source = c.Request.PostForm
		if len(source) > maxFormParamCount {
			return nil, nil, errors.New("参数过多")
		}
	} else {
		source = c.Request.URL.Query()
		if len(source) > maxParamCount {
			return nil, nil, errors.New("参数过多")
		}
	}
	for k, vs := range source {
		if len(vs) == 0 {
			continue
		}
		if len(k) > 64 {
			return nil, nil, errors.New("参数非法")
		}
		for _, v := range vs {
			if len(v) > maxParamLen {
				return nil, nil, errors.New("参数非法")
			}
		}
		values[k] = vs
		params[k] = vs[0]
	}
	return params, values, nil
}
