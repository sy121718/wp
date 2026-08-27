# 0-A1 · 数据流转与发布管道规范（归档）

> 用户规范《0-A1 数据流转与发布管道规范》原文归档。实现映射见 §5。

## 1. 核心架构与设计哲学

1. **草稿与发布强解耦**：编辑器的任何修改、保存仅更新「草稿树（Draft AST）」，绝不直接污染线上生产环境。只有显式触发「发布」操作，才会生成不可变的构建快照并进行编译。
2. **构建产物不可变性（Immutable Artifacts）**：每一次发布构建产生的静态 HTML/CSS 文件均携带唯一版本标识与内容哈希独立归档落盘，永不覆盖历史文件。
3. **指针式激活与零秒回滚**：线上路由服务只认当前处于「活跃（Active）」状态的产物指针。切换版本或回滚历史版本仅需原子切换指针，无需重新编译。

## 2. 发布生命周期与状态机流转

整个生命周期分为 **草稿阶段 → 构建阶段 → 产物归档 → 激活服务** 四个闭环环节：

```text
[可视化编辑器 / 后台]
       │
       ▼ (1) 保存草稿 (Save Draft)
【草稿状态 Draft】──────── 写入最新 Draft AST，记录编辑快照
       │
       ▼ (2) 触发发布 (Publish Action)
【构建中 Building】─────── 冻结当前 AST 为只读快照，排队进入 Go 编译器
       │
       ├─────────────────────────────────────────┐
       ▼ (构建成功)                               ▼ (构建失败)
【产物就绪 Ready】                           【构建异常 Failed】
 写入独立静态 HTML/CSS 文件并生成内容哈希       记录错误日志，线上版本保持不变
       │
       ▼ (3) 原子切换活跃指针
【线上发布 Published】
 将当前版本标记为 Active，旧版本自动转为 Superseded
       │
       ▼ (4) 遇险一键回滚 (Rollback)
【历史版本 Superseded】──── (秒级重置指针) ────► 重新激活为 Published
```

状态集：`Draft`（当前草稿）→ `Building`（冻结快照编译中）→ `Ready`（产物就绪）| `Failed`（构建异常，线上不变）→ `Published`（活跃）| `Superseded`（历史存档，可回滚）。

## 3. 四大核心模块业务职责

### 3.1 页面与草稿模块 (Page & Draft)

- **业务职责**：维护页面的全局标识（如访问路径 Slug）、页面级设置（SEO、版心、背景）以及当前工作区的最新组件树（AST）。
- **操作流**：编辑器内自动保存或手动保存时，向草稿模块写入完整 JSON 结构；每次保存自动生成一条修订快照（Revision），供编辑器内的历史记录（Undo/Redo/版本对比）回溯使用。

### 3.2 构建任务调度模块 (Build Job Engine)

- **业务职责**：接收发布请求，作为只读快照与 Go 编译器的桥梁。
- **操作流**：接收发布动作后，立即锁定当前瞬间的 Draft AST，生成不可变的 Build 快照，防止用户在编译期间继续编辑导致数据撕裂；调度 Go Publish Compiler 递归解析组件树、批量向媒体模块解析图片规格与响应式 `srcset`、向 CMS 模块拉取动态绑定的字段数据、提取整页 CSS 样式规则。

### 3.3 静态产物归档模块 (Artifact Storage)

- **业务职责**：物理承载 Go 编译器直出的纯净静态 HTML、CSS 产物。
- **操作流**：编译完成后计算整页产物的内容哈希值（SHA256）；将 HTML/CSS 文件以哈希或版本号命名落盘到静态存储目录（或对象存储中），形成独立版本包；产物一旦生成，禁止任何原地修改。

### 3.4 发布版本与激活控制模块 (Publication Control)

- **业务职责**：管理全站所有页面的对外版本指针与发布历史。
- **操作流**：
  - **原子激活**：新产物就绪后在单一事务中将当前页面的活跃发布版本切换为最新产物，旧版本状态变更为历史存档（Superseded）。
  - **路由直出**：外部访客请求页面时，Web 服务层直接根据活跃版本映射读取对应静态 HTML 文件极速返回（耗时低于 5ms），不经过任何数据库查询或模板拼装。
  - **秒级回滚**：管理员在后台选择任一历史版本点击「回滚」时，系统直接将活跃版本指针指回历史静态产物，无需重新触发编译器，即时生效。

## 4. 编辑器与数据层的交互契约

1. **实时草稿保存**：编辑器在用户操作停顿（防抖）或按下 `Ctrl+S` 时向后端提交最新 AST；后端仅更新草稿字段并追加历史版本，返回保存成功时间戳。
2. **实时预览（Preview）**：编辑器点击「预览」时，后端基于当前草稿 AST 走一次轻量级编译，产出临时隔离的预览静态页面，并返回专用预览访问链接供 Iframe 无缝加载，完全不影响线上正式环境。
3. **正式发布（Publish）**：提交发布后顶栏进入「编译发布中」加载态；后端完成构建、落盘与指针切换后，返回发布成功状态、版本号及最终可访问的公开 URL，编辑器状态同步变更为「已是最新发布版本」。

## 5. 实现映射

> 说明：本规范对应 Phase 0-A1（手工 Page 静态发布主链），流水线层的实现设计已展开为 `03-pipeline.md`，各阶段进度见 `05-implementation-plan.md` 与 `04-runtime-and-delivery.md` §4（验收清单）。

| 规范条目 | 实现 / 计划 |
|---|---|
| 生命周期状态机（Draft/Building/Ready/Failed/Published/Superseded） | `docs/03-pipeline.md` §6（Draft/Build/Stage/Publish/Rollback 流程）、§7（删除、取消发布与 GC）；落地为 Phase 0-A1 `publication` 模块（`05-implementation-plan.md` 阶段 3.3） |
| 不可变产物 + SHA256 + 版本包 | `docs/03-pipeline.md` §4（Page Artifact、§4.2 Artifact Manifest、§4.3 ArtifactStore 契约）；落地为 Phase 0-A1 `artifact` 模块 |
| 指针式激活 / 秒级回滚 | `docs/03-pipeline.md` §5（PublicationStore 符号链接原子激活）、§6.4（Rollback）；落地为 Phase 0-A1 `publication` 模块 |
| 路由直出（<5ms，无 DB/模板） | `docs/03-pipeline.md` §5（URL → 文件映射由 PublicationStore 文件状态决定）；`docs/04-runtime-and-delivery.md` §2.3（访客请求不查库、不执行 Jet） |
| 草稿保存 / 修订快照 Revision | `docs/03-pipeline.md` §6.1（Draft 与乐观锁）、§8.1（Revision 机制）；落地为 Phase 0-A1 `page` 模块 |
| 预览（轻量编译 + 隔离预览页） | `docs/03-pipeline.md` §2（Preview Build）；编译器内核已实现于 `internal/builder` |
| 构建快照防撕裂（冻结 AST） | `docs/03-pipeline.md` §6.2（Build 与 Stage 的版本校验）；落地为 Phase 0-A1 `build` 模块 |
| 生产持久化 | `docs/03-pipeline.md` §9（流水线层持久化投影，pipeline 表 schema 已建）：`projects`/`pages`/`page_documents`/`artifacts`/`page_routes` 等 |

编译器内核（Draft AST → 静态 HTML/CSS）已落地于 `internal/builder`（`builder.go` 入口 + `core/` 内核 + `components/` 组件库），是「Build Job Engine → Go Publish Compiler」的执行者；Page/Build/Artifact/Publication 四个 module 尚未创建，属于 Phase 0-A1 计划范围。