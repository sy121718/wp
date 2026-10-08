package shell

// helpers.go — 后台页面的零散公共工具：JSON 安全串、表单取值、分页参数解析、
// 对外文案出口、详情页路径派生。
//
// 同主题的专门文件：shell.go（外壳装配与权限中间件）、nav.go（侧栏）、
// pagination.go（分页组件）、errors.go（错误出口）。

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/siteurl"
)

// JsonSafe 转义 JSON 字符串里的 script 闭合序列，防止用户可编辑的内容注入
// </script> 提前闭合 <script type="application/json"> 数据岛造成存储型 XSS。
//
// JSON 允许斜杠用反斜线转义（JSON.parse 会还原，不影响前端解析），因此只需处理
// 斜杠形式的闭合点，以及 HTML 注释起始序列：其余 < 在 JSON 字符串内合法，不会闭合 script。
func JsonSafe(s string) string {
	s = strings.ReplaceAll(s, "</", "<\\/")
	s = strings.ReplaceAll(s, "<!--", "<\\!--")
	return s
}

// FieldValue 取 PostForm 值并去掉首尾空白；无值返回空串。
func FieldValue(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

// ParseUint 解析非负整数；空串或非法返回 0。
func ParseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

// ParseStatus 解析状态；空串返回 0（多数域 0=禁用，service 默认启用由各 create 处理）。
func ParseStatus(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

// ParseStatusPtr 解析状态为 *int（未传用 nil 表示，如 RoleCreate 的「未传即启用」）。
func ParseStatusPtr(s string) *int {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	v, _ := strconv.Atoi(t)
	return &v
}

// FilterBaseURL 拼出「路径 + 非空筛选参数」作为分页链接前缀，翻页时保留筛选条件。
func FilterBaseURL(path string, filters map[string]string) string {
	q := url.Values{}
	for k, v := range filters {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// PageParams 读取分页查询参数（?page=&limit=），缺省第 1 页、每页 20 条。
// limit 上限 100（与各模块 GetLimit() 的上限一致）。
func PageParams(c *gin.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.Query("page"))
	limit, _ = strconv.Atoi(c.Query("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

// PageInternalText 统一内部错误文案（走当前语言的译文，缺词条回退中文原文）。
func PageInternalText(c *gin.Context) string {
	return TranslateFor(c)(MsgInternalError, "系统内部错误，请稍后重试")
}

// FacingQueryText 是列表页 ?err= / ?ok= 回显参数的共用骨架：raw 先过本域白名单 allow，
// 未命中时用 fallback（错误提示落统一文案、成功提示落空串）——
// 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
func FacingQueryText(raw, fallback string, allow func(string) string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := allow(raw); msg != "" {
		return msg
	}
	return fallback
}

// SiteURLPatternsOf 读某工程的 URL 模式配置。
//
// 读不到（工程不存在 / settings 为空 / 字段缺失）一律返回 nil，由 siteurl 回落到默认模式 ——
// 派生路径是「帮用户把表单填好」，任何一步失败都不该让页面报错。
func SiteURLPatternsOf(ctx context.Context, projects projectcontract.ProjectService,
	projectID string) map[string]string {
	if projects == nil || projectID == "" {
		return nil
	}
	project, err := projects.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return nil
	}
	return projectcontract.ParseSiteSettings(project.Settings).URLPatterns
}

// SiteDetailPath 派生某实体在某工程下的详情页路径（无模式可依时返回空串）。
//
// 派生规则在 internal/siteurl（纯函数），站点配置在 SiteSettings.urlPatterns，
// 两处硬编码都收口到这一个入口 —— 各页面不再各写一份 /products/ 或 /blog/ 前缀。
func SiteDetailPath(ctx context.Context, projects projectcontract.ProjectService,
	projectID, kind, slug, id string) string {
	return siteurl.DetailPath(kind, slug, id, SiteURLPatternsOf(ctx, projects, projectID))
}
