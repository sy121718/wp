---
id: go-wp-modules
type: architecture-design
title: 业务模块总纲
status: active
parent: go-wp-arch
---

# 业务模块总纲

`internal/module/<模块>/` 是业务单元。每个模块拥有自己的表，跨模块只经对方的 `contract`。

## 分层

标准目录：`contract`（接口 + 不可变 DTO）、`dto`（请求绑定与校验）、`enums`（枚举、错误 key + 兜底、状态码、界面 key）、`model`（本模块表访问）、`service`（业务规则与事务边界）、`inbound/http`（控制器）、`inbound/mcp`（AI 工具）、`outbound`（外部系统适配）。

每层的准入与禁令见 `internal/module/CLAUDE.md`，本节不复述。三条最常被踩的：

- 控制器方法体只有「绑定 → 调 service → 出口」；模板名只出现在 router 的 `adminui.PageSlot` / `BareSlot`。
- service 只出数据与事实（`pkg/facing` 的 key + 兜底），不拼句；事务边界在这里。
- **取词的边界是「谁拥有词条」，不是「谁调 i18n」**：service 可以按 ctx 里的取词函数把**声明处已有的事实**（enums 的 key + 兜底）取成当前语言，只是调一次内存查询；被禁的是自造词条（`"admin./shell./user./workbench."` 这类 key 字面量）。
- 成品句不进 dto 的**语言绑定字段**（译文、已取词的 `Err` / `ErrMsg`）——换一种语言就要重算，由页面出口或模板临时填。
- model 不写业务规则、不跨聚合事务、不声明列型、不出现别人的表名。

## 模块与边界

| 模块 | 拥有 | 边界要点 |
|---|---|---|
| `project` | 工程（站点）、主题、站点语言清单 | 站点级配置的唯一真源；不认内容模板表 |
| `content` | 内容实体（JSONB）+ 字段白名单 | 字段白名单只此一处；构建期按语言预载译文 |
| `contenttemplate` | 内容实体类型的页面结构定义与版本 | 结构模板拒绝字段绑定；保存宽容、发布严格 |
| `blueprint` | 新建页面文档的初始化蓝图 | 用完即弃，不参与构建与访问 |
| `block` | 可跨页面复用的结构片段 | 引用检查 fail-closed |
| `media` | 附件、分类、变体、产物引用集 | 生产代码零跨模块 import；换图不删旧文件 |
| `navigation` | 站点导航树（多 kind） | 与后台权限菜单 `sys_menus` 严格隔离 |
| `page` | 手工页面：文档、修订、发布、路由、槽位、定时 | 单 URL 的独立构建/发布/回滚；FORCE 策略表需逐工程作用域 |
| `presentation` | 自动发布实例与文档快照 | 实例由 CMS 实体驱动，不经构建器编辑；编译段不持锁 |
| `build` | 构建任务队列与租约 | 队列不反查来源表；执行器不改任务状态 |
| `artifact` | 不可变产物记录与内容对象闭包 | 不判断业务引用；GC 默认 dry run 且保留下限 |
| `publication` | URL 路由占用状态机、发布回执、站点文件 | `*Tx` 变体不新开事务；已激活路径不得直接改名 |
| `runtimefragment` | 片段能力注册表与端点 | 无 contract/model；不读页面文档、不执行 Jet；未注入依赖 fail-closed |
| `product` | 商品、变体、属性、分类、品牌、标签、定价规则、捆绑 | 商品不存价，价格只在变体；不存库存真源 |
| `inventory` | 库存真源、仓库、变动流水、BOM、货源、采购入库 | 可用量只读库存行锁内；入库不得旁路 |
| `cart` | 客户端签名 cookie 里的购物车 | 无表；不建订单、不落账 |
| `order` | 订单、明细、状态流水、退货、优惠码、客户分析 | 不 import inventory（经收窄的 `StockOperator` 契约） |
| `membership` | 会员等级、权益、分配 | `Resolve` 在访问面绝不写库 |
| `comment` | 评论与审核状态机 | 不认识具体实体表，规则问注入的 `EntityPolicy` |
| `masterdata` | 主数据字段级变更留痕 | 不认识商品/库存表；与库存流水分工 |
| `admin` | 后台账号、角色、权限点、菜单、部门、数据规则 | 与 `user`（访客账号）分属两套会话命名空间 |
| `user` | 访客账号、资料、偏好、会话 | 会话 Redis 唯一真源；依赖方向 user → mail 单向 |
| `mail` | 邮件账号、模板、联系人、群发、自动化 | 不 import 任何模块契约；不认识用户表 |
| `analytics` | 访问打点、归一化、日报聚合 | `Collect` 是访问面唯一写入口（恒 204、无鉴权） |
| `webhook` | 投递端点与投递记录 | HMAC 签名 + SSRF 逐 IP 判定；出站由 worker 执行 |
| `plugin` | 插件注册、安装、启停、巡检与装配 | 不热加载 Go 代码；组件能力只经构建器装配面 |
| `ai` | 模型配置、会话、事件、调用与工具日志 | 密钥只在加密入参/请求头两瞬；SSRF 防护独立实现 |
| `sysconfig` | 系统配置、字典、行政区 | 写必须带版本；整组替换不合并 |
| `workbench` | 工作台的编排与计算 | 无 model/contract/表；不持 DB、不写响应 |
| `common` | 验证码端点 | 无 service/model，直调 `pkg/captcha` |

## 跨模块依赖

只经 `contract`。写进契约的形状见各模块 SPEC；这里只记边的方向。

```text
page          -> publication, artifact, build, blueprint, media
presentation  -> contenttemplate, project, publication, page, media, product, navigation, plugin, block
content       -> project, presentation, page, contenttemplate
contenttemplate -> project, content, product, block
blueprint/block -> builder, project
product       -> inventory, masterdata, comment, content, contenttemplate, page, presentation, project
inventory     -> product, masterdata, order(outbound), project
cart          -> product, order, membership, project
order         -> product, membership, user, sysconfig, webhook, project
user          -> mail, membership, order, project
navigation    -> page, presentation, content, block, project
workbench     -> product, page, block, blueprint, contenttemplate, plugin, presentation, project
runtimefragment -> product, cart, comment, content, membership, order, page, presentation, project, sysconfig, user
plugin        -> admin
admin         -> page, sysconfig
```

反向实现（调用方持接口、被调方实现）不算反向依赖：`inventory/outbound/orderstock` 实现 `order` 的 `StockOperator`；`page` 与 `presentation` 各自实现 `media` 的 `StaleMarker`。

## 数据与事务

- 表只被一个模块读写；跨模块读经契约，不 join 别人的表。
- 两处以上写入必须同一事务，边界在 service，句柄从 model 下传（`…Tx` 方法）。
- 带 FORCE 策略的表在缺工程作用域时表现为「不存在」：只有 id 的入口必须逐工程探测归属再进事务，不得退化成「不限工程」。
- 时间列：新表 `create_time` / `update_time`，业务时刻沿用 `*_at`；统一 `timestamptz` + `time.Time`；对外 JSON 给 unix 毫秒。
- 迁移是版本化幂等 SQL + `register.go`；seed 门槛逐条枚举本批对象，禁用前缀 LIKE 与全库总量。

## 已知限制

存量重构债未清（控制器取词、dto 展示字段、`c.PostForm`、`shell.RenderJump`、`*_err.go` / `*_jump.go`）；判据与台账见 `PLAN.md` 与 `internal/architecture/`。
