# publication SEO 审计 MVC 实施计划

> 执行技能：subagent-driven-development。固定成员分工；全部实现与测试源码完成后，单一负责人集中验收。

**目标：** 审计业务端点统一返回事实 JSON，保留工程隔离与双权限。
**架构：** service 产出事实，handler 绑定/调用/响应；Jet 预置译文和结构，专用 JS 填参数；提交复用 write-base，不再用 json_swap。
**技术栈：** Go、Gin、Jet、原生 JS。
**依据：** AGENTS.md、public/docs/其他模块MVC职责分离规范任务书.md、public/docs/商品模块MVC改造执行记录.md；分层以 AGENTS.md 为准。

## 全局约束与工作树

- 工作树 `/home/sky/project/go/wp/.worktrees/mvc-publication-seo`，分支 `refactor/mvc-publication-seo`，起点 `87902f126208c6fe11786becaff5caa102e1cb52`。
- 原工作区四项未跟踪资料不纳入、不移动、不删除。
- 只改 publication 审计及 analytics SEO 直接消费者。其他评分算法、scoreview、数据库结构、权限种子和全局鉴权不改。
- 实现期只读源码、写实现与测试源码、gofmt、差异自检；不运行 build/test/vet/门禁。集中验收规则覆盖技能默认基线测试与逐步运行测试要求。
- 不自行提交、合并、推送；文档与代码分开组织。
- 执行者先读技能目录及适用规则并加载匹配技能。结构检索优先 code-review-graph，文本检索补充；搜索未命中不等于没有外部客户端。

## 冻结接口

保留 `POST /api/publication/seo-audit`。任意 HX/Accept 组合均返回 `{code,message,data}` JSON。保留 PublicationSEOAudit、CasbinMiddlewareForPath("/api/seo/audit") 和原位虚拟 permission.Declare。

请求 SeoAuditReq.Project 使用一致 json/form 的 project tag，不新增 projectId。入口 binding.JSON 支持 JSON/urlencoded/multipart。保留既有 query 回落：body 工程去空白后非空则优先，否则 binding.Query 读取 project；请求 DTO 方法 `ResolveProject(query *SeoAuditReq) string` 负责去空白与选择，handler 不手读 c.Query。非法 JSON 立即 400，不回落成功。service 校验最终空工程并通过 enums 返回 400。

保留 query 是避免本次职责迁移附加输入行为变更，不以旧注释当作真实流量证据。

service 签名：`RunSEOAudit(ctx context.Context, projectID string) (AuditResult, error)`。
类型定义于 `internal/module/publication/service/seo_audit_result.go`，不新增响应展示 DTO：

```go
type AuditResult struct {
    Scanned int          `json:"scanned"`
    Count   int          `json:"count"`
    Issues  []AuditIssue `json:"issues"`
}
type AuditIssue struct {
    Path   string           `json:"path"`
    Item   string           `json:"item"`
    Level  string           `json:"level"`
    Params AuditIssueParams `json:"params"`
}
type AuditIssueParams struct {
    Title string   `json:"title,omitempty"`
    Src   string   `json:"src,omitempty"`
    Href  string   `json:"href,omitempty"`
    Paths []string `json:"paths,omitempty"`
}
```

Count 在 service 等于 len(Issues)，空 Issues 编码为 []。单页 Path 为真实路径；重复标题 Path 为空，Title 为原值，Paths 为全部命中路径排序数组，禁止挑首路径伪装归属。图片与死链分别携带 Src/Href 原值，其他四类无参数。七个 Item 和三个 Level 维持现有语义。删除数据内 Message；envelope message 仍由 response 出口负责。

scanned=0 为无产物，scanned>0 且 count=0 为未发现问题，均 200。保持工程激活路径过滤；依赖读取失败传播 500，不吞为空态。发现解析/算法本身缺陷时报告证据，禁止悄悄扩大算法改造。

## 前端冻结方案

GET 工程选择保持；审计表单使用 data-write、data-write-url、data-write-handler="publication.seo-audit"，移除 hx 提交，保留 project/CSRF 快照。新建 `internal/templates/static/js/ui/seo-audit.js`，在 `internal/templates/partials/ui_scripts.html` 的 index.js 前加载。使用 WBUI.writeBase.registerResultHandler，返回 true，无导航/刷新/关抽屉。

Jet 预置七类问题、未知类型、级别与空态模板；JS 克隆节点并 textContent 填值，不使用 innerHTML，不建第二份词表。Paths 完整可见或通过原生 details/summary 键盘展开，不藏在 title 属性中。未知 Item 显示已取词通用提示及原标识纯文本。保留 aria-live、无权限分支，失败保留输入和已有结果。

明确新增最小共享能力：write-base 支持仅此表单开启的 `data-write-latest`。每次提交递增 form 请求代号；成功、失败、catch、解锁均检查代号及 form.isConnected。旧响应不渲染、不提示、不解锁新请求；未启用表单保持原行为。此能力尚未实现，不能假定现有基座已支持。

## 文件所有权、任务与验收断言

### A：后端事实和请求

拥有 `internal/module/publication/service/publication_feed.go`、新 `service/seo_audit_result.go`、`service/seo_audit_test.go` 及必要同包审计测试；`dto/publication_req.go`、`dto/publication_seo_audit_resp.go`、`enums/publication_enums.go`（后三路径同属 publication）。
- [ ] 写 TestAuditIssueFacts：七类事实、完整重复路径、无 Message 拼句；TestAuditResultEmpty：Issues=[] 且 Count=len(Issues)。
- [ ] 写请求 ResolveProject 表格测试：body 优先、空白回落、双空；缺工程用声明处 400。
- [ ] 实现冻结类型/签名；保留算法与工程范围，删除旧视图及孤立展示辅助函数。
- [ ] gofmt、自检；报告文件、签名、未决消费者，不跑测试。

### B：HTTP 与既有台账

拥有 `internal/module/publication/inbound/http/seo_audit_result.go`（改名 seo_audit_handle.go）、对应 HTTP 测试、publication_router.go；`internal/module/analytics/inbound/http/seo_page_handle_test.go`；`internal/architecture/controller_shape_test.go` 和 text_domain_test.go 的本域既有条目。
- [ ] handler 使用 A 的签名，绑定→调用→Success/ErrorStatusAuto；删除 BareSlot、旧模板常量、pubAuditFailedMsg、toAnyIssues、pageview/adminui 依赖。
- [ ] TestSEOAuditJSONContract 覆盖三传输、query 回落、非法 JSON、HX/非 HX、400/500、原文不泄露、无跳转。
- [ ] 迁移 analytics 页面旧 hx 断言；按最终代码减台账，不扩大豁免。
- [ ] 定向核对路径/旧符号/MCP 调用者，记录查证范围；双权限原位不改。

### C：模板、JS 与共享前端

拥有 `internal/templates/admin/analytics/seo.html`、seo_audit_result.html（删）；`internal/templates/partials/ui_scripts.html`；`internal/templates/static/js/ui/seo-audit.js`、write-base.js 及对应 JS 测试。测试具体位置沿现有仓库布局，开写前在任务板登记。
- [ ] 实现前端冻结方案，删除旧片段，不改 Go 测试。
- [ ] 测七类/未知类型、两种空态、恶意文本、完整路径、无导航、一次表单快照。
- [ ] 测旧成功/失败晚于新请求、断开节点、重复初始化、未启用 data-write-latest 的共享回归。

### D：feature 与新增断言

拥有 `public/test/publication/feature/seo_audit_json_test.go`、必要 analytics 专属 feature；新 `internal/architecture/publication_seo_audit_boundary_test.go`。禁止编辑 B 的台账文件。
- [ ] 从真实装配链验证会话、CSRF、缺任一权限时真实状态与 JSON，无重定向。
- [ ] 真实 PG 由生产迁移建表，验证两工程隔离；不可用的跳过独立记录，最终 GOWP_REQUIRE_PG=1。
- [ ] 旧 HTML 出口/展示类型清退断言和行为测试相配，不用计数冒充正确性。

### E：集中验收与交付

所有写手停止、主负责人静态复审通过后才开跑。E 不自行改生产代码或放宽测试。
- [ ] 记录命令、日志、退出码、跳过项。仅有实际生成物变更时运行对应生成。
- [ ] `go build ./...` 和生产包 `go build -o /tmp/wp-seoaudit-acceptance ./cmd`。
- [ ] publication、analytics 受影响包/feature，新增架构断言和 JS；对照 check-all.sh 去重，默认不加 -race，不全量 go test ./...。
- [ ] `bash scripts/check-all.sh`；浏览器桌面/平板/手机核查空态、错误、权限、竞态、键盘完整路径。
- [ ] 首轮汇总失败后交原负责人集中修复，随后仅定向复验；main 基线已有失败单列，不顺手修。
- [ ] 更新工作树 code-review-graph，交差异、旧符号清理、门禁减条和未覆盖限制。任何通过结论附真实证据。

## 主审重点

body/query 冲突与畸形 JSON（A/B）；站点共目录下两工程隔离（A/D）；标题/路径恶意文本（C）；旧失败迟到及节点卸载（C）；缺权限/会话/CSRF 的真实链（D）。上述每项必须有对应断言，门禁绿不能替代语义验收。

## 当前状态

计划已定稿，业务代码未改，未运行构建/测试/门禁。用户确认本方案后，按既定 A/B/C/D 并行执行、E 集中验收；不再启动候选方案讨论或全仓盘点。
