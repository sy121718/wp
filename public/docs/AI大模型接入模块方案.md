# AI 大模型接入模块方案

> 状态:待评审。本文档只做设计与接口规划,确认合适后再落地代码。
> 定位:全新独立模块 `aimodel`,不触碰现有任何模块(cursor 正在改的其他模块不受影响)。

## 一、目标与边界

提供统一的 AI 大模型接入与调用能力:

- 后台管理大模型配置(CRUD),支持多厂商
- 对内对外提供统一调用入口,屏蔽各厂商协议差异
- 连接配置(config)随模型配置一起管理,调用时按 model_id 自动带出

本模块只负责「模型配置 + 协议适配 + 统一调用」,不承担具体业务决策(例如:文章模块用它生成摘要,那是文章模块的逻辑,本模块只把请求转发给对应厂商并返回结果)。

### 核心概念:协议 driver

| 概念 | 含义 | 示例 | 决定什么 |
|------|------|------|----------|
| `config.protocol` 协议 | 通信协议,映射到哪个 driver | 由已注册的 driver 决定,用户自填 | outbound 用哪个 driver 转发请求 |

`protocol` 的值由系统已注册的 driver 列表提供,不预设限制。厂商不再是独立字段,由用户在 `model_name` 中自定义标识即可。

## 二、数据模型

### 2.1 模型配置表 `ai_model`

```sql
CREATE TABLE ai_model (
  id          bigint unsigned NOT NULL AUTO_INCREMENT,
  model_name  varchar(100)  NOT NULL COMMENT '模型显示名称',
  model_code  json          NOT NULL COMMENT '模型标识列表,如 ["gpt-4o","gpt-4o-mini"]',
  config      json          NOT NULL COMMENT '连接配置: {"endpoint":"...","api_key":"...","protocol":"openai"}',
  status      tinyint       NOT NULL DEFAULT 1 COMMENT '0禁用 1启用',
  remark      varchar(200)  DEFAULT NULL COMMENT '备注',
  create_by   bigint unsigned NOT NULL DEFAULT 0,
  create_time datetime(3)   DEFAULT NULL,
  update_by   bigint unsigned NOT NULL DEFAULT 0,
  update_time datetime(3)   DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_status (status)
) COMMENT='AI 大模型配置';
```

参考 `sys_rule` 的字段风格(`bigint unsigned` 主键、`datetime(3)` 时间、`tinyint` 状态)。

### 2.2 模型标识 model_code

`model_code` 为 JSON 数组,存放该供应商下可用的多个模型标识(如 `["gpt-4o","gpt-4o-mini"]`),由管理员自填,**系统不做任何预设模型校验**,填什么转发什么。调用方可用 `model_id` 关联记录自动带出提供商和模型列表,也可直接传 `provider` + `model_code`。

### 2.3 连接配置 config

`config` 为 JSON 字段,存储调用该模型所需的连接信息:

```json
{
  "endpoint": "https://api.openai.com/v1",
  "api_key": "sk-xxx",
  "protocol": "openai"
}
```

| 字段 | 说明 |
|------|------|
| `endpoint` | API 地址 |
| `api_key` | 密钥 |
| `protocol` | 协议类型,决定使用哪个 driver。值取自系统已注册的 driver 列表,用户新增模型时下拉选择 |

`protocol` 与 `provider` 未必相同,例如 DeepSeek 兼容 OpenAI 协议时: `provider=deepseek`, `config.protocol=openai`。

## 三、协议适配(outbound driver)

各厂商协议不同,用 driver 模式屏蔽差异。service 只调统一接口,不关心厂商。driver 不预设具体协议清单,通过注册中心可扩展。

```text
internal/module/aimodel/outbound/
└── driver.go        # Driver 接口 + 注册中心
```

具体 driver 实现在 `internal/module/aimodel/outbound/` 下按文件独立存放(如 `openai.go`、`anthropic.go`、`doubao.go`),新增协议只需加文件 + 注册。

`Driver` 接口(v1 先做非流式):

```go
// Invoke 调用大模型,统一出入参,屏蔽厂商差异。
type Driver interface {
    Invoke(ctx context.Context, cfg ModelConfig, req InvokeRequest) (*InvokeResult, error)
}
```

`ModelConfig`(provider/model_code/endpoint/api_key/protocol/params) 由 service 从模型配置中取出并组装;`InvokeRequest` 含 messages;`InvokeResult` 含 content/usage/raw。注册中心按 `protocol` 取 driver,找不到返回明确错误。

> driver 注册流程:实现 Driver 接口 → 在 `init()` 中调用 `Register("protocol名", driver实例)` → 用户在后台配模型时,`config.protocol` 下拉即可看到已注册的协议。一个厂商使用标准协议时无需新增 driver,直接复用已有(如 DeepSeek 复用 `openai` driver)。

## 四、对外契约(contract)

`contract/aimodel_contract.go` 暴露两部分能力:

1. **管理能力**(供 inbound/handle 调用):List/Detail/Create/Update/Delete
2. **调用能力**(供其他模块直接 import 调用):`Invoke(ctx, req) (*InvokeResult, error)`

其他业务模块要用大模型时,依赖本模块 `contract`,不导入 `service/model/dto`(遵循模块隔离约定)。

## 五、接口清单

统一前缀 `/api/aimodel`,管理类走 `JWTAuthMiddleware + CasbinMiddleware`(与 datarule 一致)。

### 5.1 模型 CRUD

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/aimodel/list` | 分页列表(管理用),支持 `status`/`model_name` 筛选 |
| GET  | `/aimodel/detail?id=` | 详情 |
| POST | `/aimodel/create` | 新建 |
| POST | `/aimodel/update` | 更新 |
| POST | `/aimodel/delete` | 批量删除(传 ids) |

`create`/`update` 请求体核心字段:

```json
{
  "model_name": "GPT 系列",
  "model_code": ["gpt-4o", "gpt-4o-mini", "gpt-4-turbo"],
  "config": {
    "endpoint": "https://api.openai.com/v1",
    "api_key": "sk-xxx",
    "protocol": "openai"
  },
  "status": 1,
  "remark": ""
}
```

### 5.2 前端选模型

供前端展示一个模型选择器（下拉或卡片）,用户选择后拿到 `model_id` 传给 invoke。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/aimodel/select/list` | 可供调用的模型列表 |

返回:

```json
[
  {
    "id": 1,
    "model_name": "GPT 系列",
    "model_code": ["gpt-4o", "gpt-4o-mini", "gpt-4-turbo"]
  },
  {
    "id": 2,
    "model_name": "DeepSeek 系列",
    "model_code": ["deepseek-chat", "deepseek-reasoner"]
  }
]
```

只返回 `status=1` 的记录,不暴露 config 等敏感字段。前端拿到 `id` 后,后续 invoke 直接传 `model_id`。

### 5.3 协议元数据

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/aimodel/protocol/list` | 协议/driver 列表(从注册中心读取已注册的协议,供前端下拉) |

### 5.4 调用

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/aimodel/invoke` | 调用大模型 |

请求:前端从 select/list 拿到 `model_id` 后,传入模型 ID 和提示词即可,连接配置自动带出。

```json
{
  "model_id": 1,
  "model_code": "gpt-4o",
  "messages": [
    { "role": "system", "content": "你是摘要助手" },
    { "role": "user", "content": "帮我总结:..." }
  ],
  "params": { "temperature": 0.3, "max_tokens": 2000 }
}
```

- `model_id` 必传,关联 `ai_model` 记录,自动带出 `provider`、`config`(endpoint/api_key/protocol)。
- `model_code` 选传,从该模型的 model_code 列表中指定一个;不传则使用列表第一个。
- `messages` 传入提示词(支持 system + user 多轮)。
- `params` 选传,覆盖调用参数(如 temperature、max_tokens)。

响应:

```json
{
  "content": "生成的内容...",
  "model": "gpt-4o",
  "provider": "openai",
  "usage": { "prompt_tokens": 100, "completion_tokens": 50 }
}
```

## 六、调用方与鉴权

`/invoke` 与管理接口一致,走 `JWTAuthMiddleware`(登录即可调用)。密钥存储在 `ai_model.config` 中,由管理员在后台配置时录入,调用时自动带出。

> 若后续要开放给无登录态的外部第三方系统,再叠加 `signature` 等签名中间件,当前不做。

## 七、目录结构

```text
internal/module/aimodel/
├── contract/aimodel_contract.go
├── inbound/http/
│   ├── aimodel_router.go      # 自装配 + 注册路由
│   └── aimodel_handle.go
├── outbound/
│   └── driver.go       # Driver 接口 + 注册中心;具体 driver 文件按协议独立存放
├── service/
│   ├── aimodel_service.go     # Service + NewService + 编译期断言
│   ├── aimodel_crud.go        # List/Detail/Create/Update/Delete
│   └── aimodel_invoke.go      # 调用:组 config → 取 driver → 调用
├── model/aimodel_model.go     # Entity + Model + DB(ctx)
├── dto/aimodel_dto.go         # req/resp
└── enums/aimodel_enums.go     # ErrXxx / MsgXxx 中文常量
```

顶层 `internal/routers/routes.go` 只追加一行(放 datarule 之后、wpsite 之前,无跨模块依赖):

```go
aimodelhttp.SetupAimodelRoutes(api, db)
```

## 八、实现步骤

1. 建 DDL(`ai_model`)→ 验证:表可建
2. model / dto / enums → 验证:编译通过
3. service CRUD → 验证:编译通过
4. outbound driver(先实现一个最常用的协议 driver)+ invoke → 验证:用一个真实 key 跑通 invoke
5. inbound router/handle,注册到 routes.go → 验证:`go build` + 健康检查 + 接口可达
6. 补 feature 链路测试(List/CRUD/invoke mock)

## 九、已定(全部确认完毕)

- 模型配置表 `ai_model` 在后台管理,`model_code` 为 JSON 数组(一条配置可存多个模型标识)
- `config` JSON 字段存储连接配置(endpoint/api_key/protocol),调用时自动带出
- `provider` 字段已删除,`model_name` 由用户自定义即可,`config.protocol` 为协议名,决定使用哪个 driver
- 模块名 `aimodel`
- driver 通过注册中心可扩展,具体协议按需加 driver 实现文件即可;不预设协议清单
- `/invoke` 走 JWT

设计闭环,开始落地。
