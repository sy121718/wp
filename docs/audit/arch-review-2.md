# 架构复审（2026-09 第二轮）

## 总体判断

这套系统最有价值的取舍仍然成立：页面结构、构建期数据和访问面产物分离；Page 与 Presentation 共用确定性编译、内容寻址和原子激活。最脆弱的地方不是编译器，而是发布编排的“接线”：能力实现、装配注册、文件系统副作用和数据库账本没有被一个可恢复状态机统一约束。本轮确认自动详情页失效扇出未注册，Page 回执只写了恢复端，Presentation 多语言发布还会在 DB 与文件系统之间留下部分成功。

## 逐维度诊断

### 1. 结构 vs 数据

结论：Binding、ContentResolver 和内容字段白名单的结构边界仍清楚；本轮新问题在传播接线而非绑定模型。`internal/module/presentation/service/presentation_stale.go:22-58` 已实现详情页按依赖标记，`internal/routers/assembly_publish.go:352-369` 却只注册 Page，见 AR2-001。风险是内容写入成功、详情页静态字节不变。

### 2. 可视化链路一致性

结论：本轮未发现比上一轮更具体的新职责冲突。已核对 Page/Presentation 都经 `renderHTML` 与同一 builder 管线；Presentation 的缺陷是发布状态编排，不是组件 CSS 或主题职责重复。

### 3. 多语言与 SEO

结论：构建期 canonical、sitemap/hreflang 和语言过滤已有实现；多语言发布的提交顺序仍破坏一致性。`presentation_i18n.go:75-106` 先写全量 publication 再逐语言激活，AR2-003 属于多语言正确性缺口。

### 4. 性能与内存

结论：上一轮性能项已有队列、插件装配缓存和特征登记整改；本轮未新增可用代码证据的性能 finding。应继续用队列深度、单页构建耗时和激活失败率作为运行判据。

### 5. 数据库设计

结论：本轮未提出新列型或索引问题。需要关注的是 publication 记录在多语言流程中被提前写成 active，这属于事务语义错误（AR2-003），不是表结构冗余本身。

### 6. 代码组织与扩展性

结论：contract/model/service 边界和 CI 扫描有效；装配层仍允许“实现存在但未注册”。Fanout 没有要求 page 与 presentation 目标成对注册，AR2-001 是扩展接入的结构性缺口。插件注册硬编码的取舍与上一轮 REG-003/004 一致，本轮不重复报告。

### 7. UI 控件库基座

结论：本轮未发现新问题。已核对 UI 令牌命名、片段样式迁移和 CSS 门禁均有现行实现；未重复上一轮已 resolved 的悬停、注入判据和多端问题。

### 8. 并发、一致性与事务边界

结论：发布一致性仍有两处未闭合。Page 的 receipt helper 没有进入 Publish 主链（AR2-002）；Presentation 多语言先提交 DB 后切 FS，且 route registration 失败被吞掉（AR2-003/004）。这比单纯锁粒度问题更直接，因为故障后状态不可由现有恢复任务判定。

### 9. 安全与信任边界

结论：权限点同步、服务层边界和 RLS 接线已有门禁；本轮未新增安全边界失效。RLS 是否真正生效仍取决于切换非超级用户，这是既有 DB-009 的未完成部署步骤，不重复计为新 finding。

### 10. 可观测性与运维

结论：日志能记录“路由登记失败”，但该日志不改变成功状态，运维无法从状态接口得知发布已部分成功。AR2-004 要求把 Warn 转成可恢复状态和可断言指标；否则日志只能事后猜测。

### 11. 测试策略与质量门禁

结论：已有队列、RLS、发布单元测试，但覆盖面偏向局部模型/服务。缺少三条失败能力实测：Fanout 是否包含 Presentation、Publish 是否真的登记 receipt、第二语言激活失败后是否收敛。四条 finding 的 verification 即为新增门禁判据。

### 12. 升级、漂移与兼容

结论：文档 `docs/03-pipeline.md` 对队列状态仍有过时描述，代码实际已有 `internal/module/build`；这是上一轮已知漂移形态，本轮不另立 finding。更严重的当前事实是代码注释声称回执接入，但 Publish 主链没有调用，已作为 AR2-002 更正。

## 整改方案

AR2-001 成本最低，应先把 Presentation 注册进 Fanout，并把缺失来源列入装配自检。AR2-002 必须把回执登记嵌入 Page 发布主链；否则已有恢复逻辑没有输入。AR2-003 需要把 Presentation 多语言发布改成逐语言可恢复状态机，代价包含迁移，但能消除“数据库显示上线、文件实际缺失”的不可判定状态。AR2-004 是 AR2-003 的收口：路由登记必须成为成功条件，失败进入 pending/retry，而不是 Warn 后返回成功。

## 优先级排序

| 档位 | Finding | 分档依据 |
|---|---|---|
| 必须立刻改 | AR2-001、AR2-002、AR2-003 | 会让静态内容长期过期，或在故障后造成线上与控制面不可恢复分裂；分别是低、中、高成本。 |
| 建议近期 | AR2-004 | 影响路由账本、回滚和 GC，但可随多语言发布状态机一并收口。 |
| 可迭代 | 无 | 本轮没有证据充分、影响更低的新问题。 |

如果只能做三件事：先注册 Presentation Fanout；再把 Page receipt 接进真实 Publish；最后重写 Presentation 多语言发布的 DB/FS 状态机。

## 开源友好度评估

新贡献者应先读 `AGENTS.md`、`docs/01-overview.md`、`docs/03-pipeline.md`，再从 `internal/module/*/contract` 和 `internal/routers/assembly_publish.go` 找装配入口，最后用 `public/test` 的真实 PostgreSQL 链路验证。插件的轻轨边界、样式白名单和示例相对清晰；真正的上手成本在于接入一个能力要同时改 contract、service、路由装配、权限/i18n 和测试，而且“实现了但未注册”仍可能编译通过。最容易劝退贡献者的三处是：装配依赖缺少统一必需端口清单、发布故障状态机横跨 DB/FS/路由三套 API、文档中队列状态与代码不同步。

## 对既有审计的更正

- **TX-009**：上一轮 resolutionNote 认为 Page 发布已在切换前登记 pending receipt。当前代码只有 `page_publish_ledger.go:27-60` 的 helper 与启动恢复，`page_publish.go:169-214` 没有任何登记/结案调用。因此该结论需要更正为“恢复能力存在，但发布主链未接线”。
- **PERF-020/DB-007**：队列本身已实现并由 `assembly_publish.go:224-250` 启动；本轮没有把“队列未落地”重复报告。新增问题是 Presentation Fanout 注册遗漏和多语言发布补偿缺失。

## 与预期的差异

任务描述把“Presentation 自动发布共享同一发布管线”作为已实现事实，这在编译器和存储调用层成立；但内容变更到 Presentation 的 Fanout 注册并未完成，且多语言发布的文件系统补偿也未达到同一原子性。因此本报告把“共享管线”与“端到端自动传播/可恢复发布”分开判断。
