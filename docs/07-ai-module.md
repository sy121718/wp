# 07 · AI 模块（设计储备 · 当前不实施）

> **状态：设计储备，不进入施工队列。** 本文固定「AI 模块」的边界、可复用机制与 Go 侧落点，
> 等待 DeepSeek Harness（`dsh`）内核稳定后复评。本文**不改变任何现状**，未新增代码、未新增依赖、
> 未改动既有文档口径。
>
> **复评触发条件见 §7。**

| 项 | 值 |
|---|---|
| 调研时间 | 2026-09-11 |
| 调研对象 | DeepSeek Harness `0.1.5-rc.2`（2026-09-10 15:09 发布，MIT，developer preview） |
| 结论 | **当前不引入 Node 运行时**；AI 能力一旦实施，走 Go 静态单二进制路径 |
| 关联文档 | `05-implementation-plan.md`（阶段计划）、`06-plugin-system.md`（插件体系，与本模块无关） |

---

## 1. 决策：当前不做

| # | 理由 | 证据 |
|---|---|---|
| 1 | **与「启动不依靠任何环境的二进制」直接冲突** | `dsh` 运行时必然是 Node 应用；官方引擎要求 `^22.19.0 || >=24.0.0`。Node SEA 单文件路线结构性走不通：Cordis 的加载器在**运行期**按 profile 动态解析并 import npm 包名，且带 `node-addon-landlock-run` 等原生 addon，静态打包与插件动态加载天生对立 |
| 2 | **上游仍在高频改内核接口** | 2026-08-19 至 09-10 共 15 个 release，9-01 起几乎每天一版、9-10 一天两版；最近 5 天内落地两项内核级变更：会话格式升到 **V3**（09-06）、**废弃同步事件读取 API**（09-09） |
| 3 | **业务侧尚无必须由 Agent 才能解的问题** | 现有路线（§4-B 三路径：静态绑定 / Fragment / Client Enhancement）已覆盖动态能力的既定需求；引入 AI 属于能力扩张而非补缺 |

**判断口径**：不是「AI 无价值」，而是**现在引入的持有成本（Node 运行时 + 平台矩阵 + 高频接口漂移）高于此刻的收益**。

---

## 2. 定位与边界（若将来实施）

| 维度 | 约束 |
|---|---|
| 所属面 | **控制面**（后台运营、构建辅助、内容治理）。**绝不进入访问面**——访客请求不执行模板、不查库，本条受架构不变量 1 保护 |
| 组件语义 | **Optional**，与 `auth`（`RequireSessionStorage` fail-fast）相反：AI 能力不可用时只降级、不阻断启动 |
| 写操作 | 一律经由模块 `service` 具名方法，复用 Casbin 权限点与审计；禁止直连 `model` 裸句柄 |
| 主体 | Agent 使用**独立可审计账号主体**，不得复用超管 token |
| 数据 | 会话与产物落 go_wp 自有表；不引入第二份业务真源 |

---

## 3. 可复用机制清单（从 DSH 抄设计，不抄运行时）

> 这些是 DSH 内核里经过实战打磨、且**官方已写成规格**的部分。抄的是数据结构与顺序纪律，与 Node 无关。

| # | 机制 | DSH 中的名称 | 规格要点 | 落到 go_wp |
|---|---|---|---|---|
| M1 | **事件溯源会话** | `dsh-session` | 会话是**仅追加**的类型化事件日志，是交互历史的唯一真源；**消息历史从日志派生**，从不单独存储；序号连续；可重放 | 新表 `ai_event`，历史由 `DeriveMessages()` 派生 |
| M2 | **表层置换** | `surfaceOp`（V3 规范） | 恰好两种形态：`'append'` 或 `{ op:'replace', startSeq, endSeq }`；端点标识**当前 surface 顺序的包含性跨度**，不是数值 seq 区间（先前的 replace 可使 `start > end`）；不允许别名或多余键 | 压缩后只改"模型可见视图"，原始日志一条不删 |
| M3 | **系统提示词归位** | `system/message` | V3 明确拒绝 `request/header.header.system`——"系统提示词属于 `system/message`"；首个步骤作为 surface 第 0 号节点，**渲染变化时原位替换**，仅在路由显式支持时追加到已缓存历史之后 | 提示词不进请求字段，作为历史节点管理 |
| M4 | **前缀稳定纪律** | 各包 README 的 `Token effect` / `KV Cache effect` 必填节 | 段落按 `order` 升序、同级按名称序拼接；动态上下文做成"**变化才追加尾部**"的持久快照；任何改动都要评估对缓存命中的影响 | 写进组件/模块规范；稳定前缀 = persona + 工具 schema + 固定指令，易变内容一律排在之后 |
| M5 | **压缩：剪枝优先** | `compaction-basic` + `tool-result-pruner` | 触发点 `agent/pre-step`（先于请求派生）；顺序 **先剪枝（零 LLM 调用）→ 用 tokenMeter 重新测量 → 仍超限才摘要**；范围边界必须保持**工具调用/结果配对**（`toolPairingBalancedBefore/After`），但不要求保持整个轮次 | 压缩策略模块，先做剪枝与 spill，摘要最后上 |
| M6 | **压缩锁与可检测崩溃** | `compaction/start` → `summary` → `end` | 先写 start、最后写 end；中途崩溃留下**孤儿 start**，是崩溃证据而非"假装完成的 end"；锁是时间点标记，不是排他容器 | 压缩事务的事件写入顺序 |
| M7 | **投影 + 增量 + 分页** | 09-09 决策 `deprecate-synchronous-session-event-reads` | 废弃 `eventAt()`/`snapshotEvents()`/`ownEvents()`；存储方向是**不再在内存保留完整事件序列**；改为 resume 时重建投影、之后**从新提交事件增量维护**、历史内容走**显式异步分页与渐进加载** | 长会话内存与并发的正解：**不驻留全量历史** |
| M8 | **失败尝试不入历史** | `assistant/attempt` | 空内容 / max-tokens 截断 / 失败调用**记录日志但不进入派生历史** | 派生时按 settlement 过滤 |
| M9 | **大输出外置** | `spill` 子系统 | 工具/命令大输出落盘，上下文只留引用 | 复用 `/storage` 与 artifact 存储 |
| M10 | **计量单一真源** | `ctx.tokenMeter` | 估算与回放归一，压缩前**重新测量**；压缩 seam 不拥有计价 API | 单例计量服务，按 session→user 归属累加 |
| M11 | **沙箱只管文件效果** | `SandboxMode` | 官方原文：模式仅管控文件系统效果（`read-only` / `workspace-write` / `danger-full-access`），**网络与进程可见性不在定义范围内**；强制执行完整性分 `full`/`partial` | 若将来给 Agent 工具：先限工作区、默认 read-only，且**必须自建网络出口控制** |
| M12 | **外部接入协议面** | `dsh-sdk-protocol` | 换行分隔 JSON-RPC 2.0；**客户端→服务端 3 个请求**（`initialize` / `session/prompt` / `shutdown`）+ **服务端→客户端 4 个通知**（`session.event` / `session.status` / `subagent.started` / `subagent.finished`）；`session.event` 全量推送、不按会话过滤 | 若将来需要"交给外部 Agent 执行"，协议面只有这些；**鉴权与归属校验必须在 go_wp 侧** |

---

## 4. Go 侧落点（储备骨架，未实施）

### 4.1 模块位置

`internal/module/ai`（重轨 Go 模块，遵循 `internal/module/CLAUDE.md`：contract / model / service / dto / enums + 自装配）。

```go
// internal/module/ai/contract/ai.go —— 跨模块只暴露 contract
type Kind string // "seo-review" | "summary" | "translate" | "qa"

type GenerateRequest struct {
    Kind      Kind
    Input     string
    Context   []string // 由调用方在 Go 侧检索好后传入，不让 Agent 自行取数
    SessionID string   // go_wp 持有的会话标识
}

type GenerateResult struct {
    Text               string
    TokensIn, TokensOut int
    Model              string
}

type AiService interface {
    Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error)
    Available() bool // 能力未启用/未就绪时为 false，调用方按降级处理
}
```

### 4.2 表结构草案

```sql
-- 会话归属（唯一真源在 go_wp，外部运行时只是执行器）
CREATE TABLE ai_session (
  id             BIGSERIAL PRIMARY KEY,
  external_id    TEXT UNIQUE NOT NULL,   -- ULID/UUIDv7，不可枚举
  user_id        BIGINT NOT NULL,
  kind           TEXT NOT NULL,          -- 用途分流，见 §6
  title          TEXT,
  last_active_at TIMESTAMPTZ,
  create_time     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- append-only 事件日志：唯一真源；消息历史由它派生
CREATE TABLE ai_event (
  session_id BIGINT NOT NULL REFERENCES ai_session(id),
  seq        INT NOT NULL,
  kind       TEXT NOT NULL,      -- user/message | assistant/message | compaction/* | ...
  surface_op JSONB,              -- 'append' 或 {"op":"replace","startSeq":n,"endSeq":m}
  payload    JSONB NOT NULL,     -- 原始内容，未知字段一律保留（宽容解析）
  PRIMARY KEY (session_id, seq)
);
```

### 4.3 依赖选型（复评时再定，三选一）

| 方案 | 静态单二进制 | go.mod 新增 | 机制完备度 | 说明 |
|---|---|---|---|---|
| **纯自写** | ✅ | **0** | 自己决定 | 只需标准库；M1–M7 按 §3 规格实现；工程量最大 |
| **Eino / ADK**（CloudWeGo） | ✅ | 数十个间接依赖 | 高 | 官方 deepseek 与 agenticdeepseek 适配；ADK 覆盖 TurnLoop、Summarization Reduction、HITL、Skill、AgentsMD、沙箱抽象 |
| **tRPC-Agent-Go**（腾讯） | ✅ | 数十个间接依赖 | 高 | graph workflow、memory、A2A/AG-UI、MCP、evaluation、observability |
| ~~DSH sidecar~~ | ❌ | 0（Go 侧） | 高 | **排除**：必然携带 Node 运行时 |

辅助库（复评时核对版本）：官方 MCP Go SDK（`modelcontextprotocol/go-sdk`）、`pkoukk/tiktoken-go`（token 估算）、`pgvector/pgvector-go`（知识库）。

### 4.4 静态构建约束（硬要求）

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o gowp ./cmd
go list -deps -f '{{if .CgoFiles}}{{.ImportPath}}{{end}}' ./... | grep -v '^$'   # 须无输出
```

- 时区：主包 `import _ "time/tzdata"`（容器内无系统 tzdata 时的兜底）
- 根证书：`scratch`/`distroless` 镜像需挂载 `ca-certificates`，或自行 embed + `x509.CertPool`
- 依赖准入：新增任何间接依赖前先核对是否会引入 cgo

---

## 5. 明确排除的选型

| 选型 | 排除理由 |
|---|---|
| 把 DSH 运行时嵌入 go_wp 进程 | Go 无法加载 JS 运行时；DSH 是 Node 应用 |
| Node SEA / `bun build --compile` 单文件 | 加载器运行期动态解析包名 + 原生 addon，与静态打包结构性冲突 |
| 用 DSH 插件机制实现 go_wp 插件 | 两者哲学不同：go_wp 插件是**编译期数据包**（zip：Jet 模板 + schema + 迁移 + 内容源），运行时零插件代码；DSH 插件是**运行时代码**。混用会破坏"插件不触碰运行时"的设计前提 |
| 把 AI 放进访问面 | 违反架构不变量 1（访客请求不执行模板、不查库） |

---

## 6. 知识库归属（若实施）

| 数据 | 归属 | 说明 |
|---|---|---|
| 业务语料与索引 | **go_wp** | `content` / `page_document` / `artifact` / `docs/*.md` 即语料；先 PG 全文检索（`tsvector`），需要语义检索再叠加 pgvector。复用既有 Casbin 权限与多站点隔离，不产生第二份真源 |
| 会话历史与产物 | **go_wp** | 落 §4.2 表；AI 产出物直接进 CMS 草稿（可评审、可回滚） |
| Agent 工作记忆 | 外部运行时（若引入） | 只放"运行时自己的笔记"，与业务数据物理隔离 |
| 检索时机 | **调用前** | 由 Go 侧检索完成后作为 `Context[]` 传入；**不让 Agent 自行取数**（更安全、更快、无需给工具权限） |

> 用途分流（若同时存在对外与对内）：**客服类**只做"检索 + 生成"，不挂任何工具与沙箱；
> **内部自动化类**才考虑工具能力。两类必须**分进程/分配置**，不可共用同一运行时（模型路由与故障域均不同）。

---

## 7. 复评触发条件（继续等待官方更新）

**任一条满足即启动复评**（复评 = 重新评估是否实施，不等于立刻实施）：

| # | 触发条件 | 判据 |
|---|---|---|
| T1 | DSH 内核接口进入稳定期 | 连续 4 周无内核级破坏性变更（会话格式、事件读取 API、组合层配置字段），或官方发布首个非 preview 版本 |
| T2 | 官方给出**可验证的零外部运行时**方案 | 例如官方单文件发行版覆盖 ESM + 动态插件加载（目前不存在） |
| T3 | 「内建 Node 运行时」成为可选项且你们接受 | 即愿意随包分发`node` + 裁剪后 node_modules（官方 Python SDK 的 runtime wheel 即此形态），换来"用户无需自行安装" |
| T4 | 业务侧出现必须由 Agent 才能解的确定需求 | 例如多步运营自动化、跨内容源的批量治理，且现有三路径无法覆盖 |
| T5 | Go 侧框架（Eino / tRPC-Agent-Go）成熟度达到可直接采用且依赖可接受 | 通过 `go mod graph` 与二进制体积评估 |

### 7.1 复评时要看的材料（命令清单）

```bash
R=deepseek-ai/deepseek-harness

# 发布节奏与变更日志（官方不维护 CHANGELOG，变更写在 release body）
gh release list -R $R --limit 15
gh release view dsh-v0.1.5-rc.1 -R $R --json body -q .body | head -60

# 内核决策是否在动（架构决策记录，带日期）
gh api "repos/$R/git/trees/master?recursive=1" --jq '.tree[].path' \
  | grep '^\.agents/notes/implemented/architecture/' | grep -v '\.zh\.md$' | sort | tail -18

# 包级迭代活跃度（版本数 = 迭代次数）
gh api "repos/$R/commits?per_page=30" \
  --jq '.[] | "\(.commit.author.date[0:10]) \(.commit.message | split("\n")[0])"'
```

> 坑：`gh api .../compare/A...B` 的文件列表**上限 300**，而 `.agents/notes` 按字母序排最前，
> 直接统计会得到"改了 300 个文件、全是 notes"的假象。要统计包级落点须浅克隆后用
> `git log --name-only` 聚合。

### 7.2 当前观察基线（2026-09-11）

| 观察项 | 基线值 |
|---|---|
| 最新版本 | `0.1.5-rc.2`（2026-09-10 15:09） |
| 会话格式 | **V3**（2026-09-06 落地，附 `dsh-session-format-v2-to-v3` 迁移包） |
| 内核框架 | Cordis `4.0.2`（08-30 后未动，相对稳定） |
| 高频变动面 | 接入与策略层：`dsh-llm`(21 版)、`dsh-mcp-client`(21 版)、`dsh-permission-presets`(19 版)、`dsh-schedule`(19 版) |
| 冻结面 | 工具包（`dsh-tool-*`）与 `dsh-session`/`dsh-tools`/`dsh-sandbox` 抽象层：**首发版后未动** |
| 决策记录制度 | `.agents/notes/implemented/` 共 930 条（architecture 346 / feature 226 / process 142 / bug-fix 97 / testing 64 / simplification 55） |

---

## 8. 与其他文档的关系

- 本文**不覆盖** `06-plugin-system.md` 的插件定义：那套是**编译期数据包**体系，与本文的 AI 能力无关；
- 本文**不改变** `05-implementation-plan.md` 的阶段划分：AI 模块不在阶段 0–7 的验收范围内；
- 本文**未登记**进 `10-todo.md`（该文档是 2026-09 的只读盘点产物，为保持其统计口径一致，待复评通过后再补登）。
