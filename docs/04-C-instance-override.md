# 04-C 商品级文档覆盖（Instance Override）设计

> 状态：设计定稿（用户已选方案 B）。实施按本文档步骤推进，跨会话以此为准。

## 1. 目标

商品（及未来任意 presentation 实体）在选模板后可**进入 workbench 可视化自定义**，
保存的是**该实例自己的文档**，不影响共享模板，也不影响同模板的其他商品。

配套交互改造：
- 商品新建/编辑弃抽屉，改整页表单（左表单 + 右模板选择/预览卡片区）；
- 「进入自定义」从商品页跳 workbench（画布用真实商品数据渲染，可点选、换组件）；
- 保存直接落实例覆盖文档，302 返回商品编辑页。

## 2. 现状链路（已核实）

- `presentation_instances` 无实例级文档；`persistBuild` 的快照 `Document: tpl.Document`
  直接烘模板 AST（`internal/module/presentation/service/presentation_persist.go`）。
- `Rebuild` 每次经 `resolveBoundTemplate` 重解析模板 → **实例级自定义必须躲开这条覆盖路径**，
  否则实体数据一更新，自定义就被模板冲掉。
- workbench 现有保存目标 `contenttemplate.Update`（模板级共享）；
  画布预览 `PreviewInstance` 已支持 `entityType/entityId/template` + 草稿 POST。

## 3. 核心机制：override_document

### 3.1 存储

`presentation_instances` 加一列（迁移版本接当前最新号递增，幂等 SQL）：

```sql
ALTER TABLE presentation_instances ADD COLUMN IF NOT EXISTS override_document jsonb;
COMMENT ON COLUMN presentation_instances.override_document IS '实例级文档覆盖；NULL=跟随模板';
```

- NULL = 跟随模板（既有行为不变，零回归）；
- 非空 = 该实例发布/重建时以此文档为准（binding 节点照常经 ContentResolver 取最新实体数据）。

### 3.2 读取优先级（发布/重建/预览统一）

```
document = inst.OverrideDocument 非空 ? inst.OverrideDocument : tpl.Document
```

改动点（均在 presentation service 内）：
- `persistBuild`：快照 `Document` 取值改为上述优先级。
- `Rebuild`（实体数据更新触发）：照常执行——override 存在时用 override 编译，
  **实体数据照常刷新**（binding 解析发生在编译期，这正是覆盖方案不丢数据的关键）。
- 模板切换（`req.TemplateID` 非空）：**清除 override**（换底稿 = 放弃自定义），
  另提供「重新同步」动作 = 清 override + 重建，行为一致。

## 4. 保存通道（workbench → 实例）

- workbench 新增实例编辑模式入口：`/workbench?instance={instanceId}&editor=1`；
  进入时若 override 为空，先以当前模板文档**播种** override（画布即可编辑），
  草稿预览复用 `TemplatePreviewDraft` 通道（DraftDocument 直传，不依赖模板文档）。
- 新增 presentation 契约方法（service 实现，走既有事务约定）：
  - `SaveOverrideDocument(ctx, req{InstanceID, Document})` —— 落 override + 重编译发布（同事务链）；
  - `ClearOverride(ctx, req{InstanceID, TemplateID?})` —— 清覆盖并按（新）模板重建。
- 权限：挂在既有 presentation API 组下，**同批 seed 权限点 + 超管策略**（072/077/078/079/151 的教训），
  改完跑 `bash scripts/check-permission-gaps.sh`。

### 4.1 模板更新的 stale 语义

模板出新版本时，带 override 的实例**不自动跟进模板**（自定义就是分叉）：
`presentation_stale.go` 的模板依赖传播对这些实例跳过自动重建，仅在商品编辑页提示
「模板有新版本」，一键「放弃自定义跟进新版」= 清 override + 重建。

## 5. 商品整页表单（弃抽屉）

- `/admin/products/new`、`/admin/products/edit?id=`：整页布局。
  左侧：基础信息表单（沿用现有字段）；右侧：模板卡片区——默认模板 + 候选列表
  （「当前绑定」高亮）+ 每卡「预览」（新标签 PreviewInstance）与「进入自定义」按钮。
- 「进入自定义」：商品未发布时先建实例（复用 `CreateInstance`），再跳 workbench 实例模式。
- 保存链：表单 POST 存商品 → 已发布实例按需 `Rebuild`（数据字段变更）→ 302 回列表。

## 6. 实施步骤（顺序执行）

1. 迁移：`presentation_instances.override_document`（幂等 + register 注册 + schema-snapshot 同步）。
2. model：`InstanceEntity` 加 `OverrideDocument json.RawMessage`（gorm type 标签属 jsonb 例外清单，同 `Settings` 口径）+ `UpdateOverrideTx` 等方法。
3. presentation service：persistBuild / Rebuild / PreviewInstance 文档取值优先级 + `SaveOverrideDocument` / `ClearOverride`；stale 传播跳过带 override 的实例。
4. workbench：实例模式路由 + 保存目标分流（instance 模式存 override；UI 明确当前编辑对象，防误改共享模板）。
5. product 页面：整页表单 + 模板卡片 + 新建/编辑/自定义三态串联。
6. seed：presentation API 新权限点 + 超管策略；`check-permission-gaps.sh` 通过。
7. 测试：feature 链路（选模板→自定义→保存→发布产物含自定义内容；实体数据更新重建不丢自定义；切模板清覆盖；另一商品不受影响），`support.NewMigratedPGTestDB`。

## 7. 红线自查清单

- [ ] 事务：override 写入 + 快照 + 产物 + 指针同一事务（persistBuild 既有链内）。
- [ ] 无内部错误直出（响应/重定向 query/模板数据三形态）；错误文案三件套。
- [ ] 路由只用 GET/POST；新接口挂 authorizedAPI 必带权限点 seed。
- [ ] `go vet` / `make test` / `check-no-internal-error-leak.sh` 全绿。
- [ ] 完成后 `code-review-graph update`。
