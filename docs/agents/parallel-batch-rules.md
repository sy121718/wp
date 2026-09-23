# 并行批次代理的工作规则（批次执行者必读）

本文件给**并行执行批次**的代理用。工作区是多人共用的，规则错一步会**静默**失败（编译过、测试绿、但页面坏）。

## 只改清单里列出的文件

- 不要 `git add` / `git commit` / `git checkout` / `git stash` —— 提交由主会话统一做
- 不要顺手改别的文件；发现清单外的问题**只报告**
- 同包私有函数的**签名**也是边界：改签名会波及清单外文件，需要时先报告再决定

## 定位方式

- `docs/02-*` 里的**行号已全部失效**，按语义定位（函数名 / 类名 / 文案 / 元素结构）
- 计数与命名不是判据：`grep -c` 只能当筛查起点，结论必须回读上下文
- 先**核对这条是否已完成**：已完成就跳过并在报告里说明，别重复劳动

## 模板（Jet）

- 模板按后端模块分目录：`internal/templates/admin/<模块>/x.html`；根只留 `layout.html` / `login.html` / `dashboard.html`
- 引用路径**相对当前文件所在目录**：同模块裸名 `x.html`、跨模块 `../partials/x.html`、`../layout.html`、模板根下的 `fragments/` 要 `../../fragments/x.html`
- 可选键用 `{{if isset(.X)}}`（缺键会让整页渲染中断）；csrf 用 `{{ .["csrf_token"] }}`（**禁止** `{{.csrf_token}}`）；Jet 注释是 `{* *}`
- **空态表头约定**：`<table>` 不放进 `{{if}}` 里，表头常驻、空态整行进 `tbody`、`colspan` 精确等于列数 —— 门禁 `bash scripts/check-empty-state-table-head.sh`

## 错误出口

- 后台页面 handler **禁止直出内部错误**，三种形态一起管：① `c.String(500, err.Error())`；② `?err=` 里塞 `err.Error()`；③ 模板数据塞原文
- 走模块的**白名单文案**（`XxxFacingMessages` / `orderFacingText` 这类）+ 归口 key；门禁 `bash scripts/check-no-internal-error-leak.sh`
- 门禁的豁免清单**只增不减**：新增豁免必须在条目里写清理由

## 技能（hook 强制）

编辑任何 `.go` / `.py` / `.ts` / `.php` / `.sql` / `.html`（Jet）之前，先调用 `skill` 工具加载对应技能（`golang-how-to`、`admin-ui-logic`、`frontend-design` 等），否则编辑会被拒。

## 验收（必做，不接受「看起来对」）

- `go build ./...` 通过。**但若失败的符号不在你的文件里，别去改别人的文件** —— 那是邻居正在改的
  半成品（多个代理共用一个工作区，全量 build 随时会被打红）。这时用**自己包的范围**验收
  （`go build ./internal/module/<模块>/...`）并在报告里点出外部红点，由主会话统一判定。
- 相关包测试通过：`go test ./internal/module/<模块>/... -count=1`（**不要跑 `./...` 全量**：慢且需要数据库）
- 上面两个门禁脚本跑绿
- 模板改动要有**真实渲染**证据（渲染断言，或 `curl` 打页面确认响应含完整 `</html>`），不要只看静态文本

## 报告格式

逐条：`状态（已改 / 已符合无需改 / 需产品决策未做）` + 语义位置（函数名 / 类名 / 文案） + 改了什么 + 验证命令与结果。

结尾附：**未做的事与原因** + **清单外发现的问题**。
