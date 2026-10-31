package pubhttp

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// SeoAuditHandle 产物体检的控制器承载者。
//
// 为什么是个类型而不是包级闭包：写端点契约（cmd/write-contracts）用 go/ast 扫
// `func (x *T) Name(...)` 的方法体取 `binding.JSON[T]` —— 自由函数 `fn.Recv == nil`
// 会被跳过去，端点就落进"没匹配到 handler"的警告里，`/api/publication/seo-audit`
// 因此没有字段契约，表单字段写错时 check-write-fields 看不见。
//
// 方法名必须是 `SeoAudit`：路径 /api/publication/seo-audit 的资源段是 `seo-audit`，
// 不含动词，契约生成器走"资源名即处理器名"那条精确匹配（camelWord → SeoAudit）。
type SeoAuditHandle struct {
	run func(context.Context, string) (pubservice.AuditResult, error)
}

// NewSeoAuditHandle 注入体检用例。run 的签名与 service.SeoAuditRun 一致，
// 控制器只负责协议，不复制业务。
func NewSeoAuditHandle(run func(context.Context, string) (pubservice.AuditResult, error)) *SeoAuditHandle {
	return &SeoAuditHandle{run: run}
}

// SeoAudit 产物 SEO 体检：绑定 → 调用例 → JSON 出口。
//
// 三种传输（JSON / urlencoded / multipart）都走 binding.JSON：这个端点的两个真实客户端
// 本来就不同 —— 页面表单发 formdata，MCP / AI / fetch 发 JSON。绑定入口只有一处，
// 「这次到底按什么解析」由 Content-Type 决定，控制器不分支。
//
// 工程取值也收在 DTO 里（SeoAuditReq.ResolveProject）：body 的 project 去空白后非空则优先，
// 否则回落到 binding.Query 读出的 query。控制器既不 trim 也不读 c.Query ——
// 「两种来源哪个赢」是请求形状，属于 DTO；写在控制器里，第二个入口就会抄第二份。
//
// 出口只有一种形态：{code,message,data}。此前 htmx 分支渲染 HTML 片段、非 htmx 分支回 JSON，
// 同一个端点两种形状意味着「HX-Request 头一换，客户端契约就变了」，且片段的失败态是
// 200 + 提示文案 —— 前端无法区分「跑失败了」与「跑通了没问题」。展示归模板与专用 JS。
func (h *SeoAuditHandle) SeoAudit(c *gin.Context) {
	req, ok := binding.JSON[pubdto.SeoAuditReq](c)
	if !ok {
		return // binding 已写 400 JSON；畸形 JSON 在这里就结束，不回落成一次成功调用
	}
	query, ok := binding.Query[pubdto.SeoAuditReq](c)
	if !ok {
		return
	}
	result, err := h.run(c.Request.Context(), req.ResolveProject(query))
	if err != nil {
		// 状态码与文案都在声明处（enums 的 facing.WithStatus / 兜底），出口只调一次。
		response.ErrorStatusAuto(c, "publication", err)
		return
	}
	response.Success(c, result)
}
