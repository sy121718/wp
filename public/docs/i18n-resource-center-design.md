# i18n 综合资源中心设计方案

## 1. 背景

当前业务模块的 `enums` 仍然直接保存中文文案，例如：

```go
const (
	ErrAdminNotFound = "管理员不存在"
	MsgBadRequest     = "请求参数错误"
)
```

项目已有 `sys_i18n` 表和 `pkg/i18n` 内存缓存组件，但业务模块尚未形成完整的使用链路。

本方案将 `sys_i18n` 定位为全局综合资源中心，统一承载：

- 业务错误信息；
- 操作提示；
- UI 文本；
- 固定业务枚举的显示文本；
- 无复杂业务规则的轻量动态选项。

核心原则：

> Go 负责稳定编码和业务约束，`sys_i18n` 负责多语言展示，HTTP 层负责根据请求语言完成翻译。

## 2. 当前状态

### 2.1 数据库

当前 `sys_i18n` 实际字段如下：

| 字段 | 作用 |
|---|---|
| `id` | 主键 |
| `item_key` | 全局稳定资源编码 |
| `lang` | 语言编码，例如 `zh-CN`、`en-US` |
| `item_value` | 翻译内容 |
| `http_code` | 错误资源对应的 HTTP 状态码 |
| `category` | `error`、`ui`、`dict`、`msg` |
| `remark` | 资源说明 |
| `status` | 是否启用 |
| `create_time` | 创建时间 |
| `update_time` | 更新时间 |

现有唯一索引：

```sql
UNIQUE KEY uk_key_lang (item_key, lang)
```

当前本地数据库已有：

- 40 个资源 key；
- 80 行中英文资源；
- `error` 26 个 key；
- `dict` 4 个 key；
- `msg` 5 个 key；
- `ui` 5 个 key。

已有资源包括：

```text
ErrAdminNotFound
ErrAdminExists
ErrAdminDisabled
ErrInvalidPassword
ErrInvalidParams
ErrUnauthorized
ErrUserNotFound
msg_operation_success
dict_admin_status_normal
dict_admin_status_disabled
```

### 2.2 运行时组件

`pkg/i18n` 已实现：

- 应用启动时从 `sys_i18n` 加载启用资源；
- 内存 Map 查询；
- 指定语言读取；
- 默认语言回退；
- 缺失语言回退；
- `http_code` 映射；
- 手动重新加载；
- 定时自动刷新；
- 并发安全的缓存替换。

因此正常业务请求不需要访问数据库：

```text
sys_i18n
    ↓ 启动或定时刷新
pkg/i18n 内存缓存
    ↓ 请求期间直接读取
业务响应
```

当前主要缺口：

1. 模块 `enums` 保存的是最终中文，而不是资源 key；
2. 没有统一解析 `Accept-Language`；
3. service 只返回普通字符串错误，无法保留 i18n key 和格式化参数；
4. handler 仍然自行决定大量 `400`、`500` 状态码；
5. 业务模块尚未统一调用 `pkg/i18n`。

## 3. 设计目标

### 3.1 目标

- 模块 `enums` 只定义稳定资源 key；
- service 返回业务错误 key 和格式化参数，不关心请求语言；
- HTTP 层根据请求语言翻译；
- `pkg/response` 继续只输出最终响应，不依赖 i18n；
- 固定业务枚举继续由 Go 控制合法值；
- 轻量动态选项可以直接由 `sys_i18n` 管理；
- 所有线上读取优先使用本地内存缓存；
- 缺失翻译时具备明确、可测试的降级行为。

### 3.2 非目标

本方案不处理以下动态业务内容：

- 商品名称和描述；
- 文章标题和正文；
- 每个客户或订单的翻译；
- 大量租户私有内容；
- 带层级、颜色、图标、排序、计算参数的复杂字典。

这些内容应由所属业务模块维护自己的数据表，不能持续堆入 `sys_i18n`。

## 4. 职责边界

| 位置 | 职责 |
|---|---|
| `internal/module/<module>/enums` | 定义本模块稳定 i18n key |
| `internal/module/<module>/service` | 返回业务错误 key 和格式化参数 |
| `internal/module/<module>/inbound/http` | 获取请求语言、翻译业务错误、输出响应 |
| `internal/middleware` | 解析并规范化请求语言 |
| `pkg/i18n` | 加载、缓存和查询翻译资源 |
| `pkg/response` | 输出最终 `code/message/data`，不翻译 |
| `sys_i18n` | 保存资源定义、翻译文本和轻量展示元数据 |

禁止形成以下依赖：

```text
pkg/response → pkg/i18n
```

`pkg/response` 必须继续接收最终文案：

```go
response.ErrorWithMessage(c, httpCode, translatedMessage)
```

## 5. 资源 key 规范

### 5.1 常量名与 item_key 保持一致

本项目不引入 `common.error.system`、`admin.error.not_found` 这类层级命名空间。`category` 字段已经负责区分 `error`、`msg`、`dict`、`ui`，无需再把分类重复编码进 `item_key`。

`item_key` 直接使用现有 Go 常量名或现有消息、字典 key：

```text
ErrSystemError
ErrAdminNotFound
ErrAdminDisabled
ErrInvalidParams
msg_operation_success
msg_admin_logout_success
dict_admin_status_normal
dict_admin_status_disabled
```

错误 key 使用 Go 导出常量命名风格，通过业务前缀区分归属：

```text
ErrAdmin...
ErrUser...
ErrUpload...
ErrPermission...
```

消息和字典继续使用现有下划线风格：

```text
msg_admin_...
dict_admin_...
ui_admin_...
```

key 必须满足：

- 在同一语言下全局唯一；
- 与 Go 常量名保持一致，避免第二套映射；
- 只表达稳定语义；
- 不包含具体语言；
- 不随中文或英文文案调整；
- 一旦被业务数据持久化，不得随意改名。

数据库通过 `(item_key, lang)` 唯一索引保证每个 key 在每种语言下只有一条记录。

### 5.2 key 即兜底

模块在尚未接入 i18n 时，直接使用固定中文文案：

```go
const ErrSystemError = "系统异常"
```

接入 i18n 后，常量值替换为 `item_key`：

```go
const ErrSystemError = "ErrSystemError"
```

`pkg/i18n.GetText` 查不到翻译时直接返回 key 本身，这是已有行为，不需要额外机制。

完整关系：

```text
ErrSystemError = "ErrSystemError"
                   │
                   ├── i18n 缓存命中 ──→ 当前语言文案
                   │
                   └── 缓存未命中 ──→ 返回 key 本身 "ErrSystemError"
```

key 本身就是语义化的（`ErrAdminNotFound`、`ErrAccountLocked`），key 没录入 `sys_i18n` 时用户看到 key 也能理解错误含义。

不需要：

- 修改 Go 常量名；
- 额外拼接模块路径；
- `Fallback` 常量；
- `RegisterFallback` 注册表；
- `GetTextOr` / `GetHttpCodeOr` 额外 API；
- 在代码中维护任何中文兜底文案。

HTTP 状态码同理。`GetHttpCode` 查不到时返回默认值。如果需要更精确的状态码，由 handler 层在 `writeError` 中指定默认值（如 `400`），而不是额外维护一张状态码注册表。

## 6. 模块 enums 改造

`enums` 不直接调用 `i18n.GetText()`；接入后只需将原中文常量值替换为 `item_key`：

```go
package adminenums

const (
	ErrCaptchaExpired   = "ErrCaptchaExpired"
	ErrBadCredentials   = "ErrInvalidPassword"
	ErrAccountLocked    = "ErrAccountLocked"
	ErrAccountDisabled  = "ErrAdminDisabled"
	ErrAdminNotFound    = "ErrAdminNotFound"
	ErrEmailExists      = "ErrAdminEmailExists"
	ErrUsernameExists   = "ErrAdminUsernameExists"
	ErrPhoneExists      = "ErrAdminPhoneExists"
	ErrUserNotFound     = "ErrUserNotFound"
	ErrDeleteSelf       = "ErrAdminDeleteSelf"
	ErrDeleteSuperAdmin = "ErrAdminDeleteSuperAdmin"
)

const (
	MsgSuccess       = "msg_operation_success"
	MsgLogoutSuccess = "msg_admin_logout_success"
	MsgBadRequest    = "ErrInvalidParams"
	MsgUnauthorized  = "ErrUnauthorized"
	MsgWrongUserType = "ErrAdminInvalidUserIDType"
)
```

key 常量负责查询 `sys_i18n`。i18n 缓存查不到时返回 key 本身，不需要额外维护中文兜底。

部分常量可以复用已有的全局资源 key，例如 `ErrInvalidPassword`、`ErrInvalidParams` 和 `ErrUnauthorized`。只有语义不同的错误才新增 key，不能因为文案相似而错误复用。

不推荐：

```go
var ErrAdminNotFound = i18n.GetText("ErrAdminNotFound", "zh-CN")
```

原因：

- 包初始化时 `i18n` 组件可能尚未初始化；
- 文案会固定为启动时语言；
- 无法根据每个请求切换语言；
- `enums` 会与基础组件产生隐式生命周期耦合。


## 7. 业务错误模型

### 7.1 为什么不能继续只用普通字符串

简单错误可以暂时返回 key：

```go
return nil, errors.New(adminenums.ErrAdminNotFound)
```

但带动态参数的错误无法正确处理：

```text
账号已被锁定，请 25m 后重试
Account locked, retry in 25m
```

service 必须保留：

- 资源 key；
- 格式化参数；
- 原始底层错误，按需保留。

### 7.2 轻量业务错误结构

建议新增一个只表达业务错误的结构，不承担翻译：

```go
package apperror

// Error 只保存资源 key 和格式化参数。
type Error struct {
	Key   string
	Args  []any
	Cause error
}

func (e *Error) Error() string {
	return e.Key
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func New(key string, args ...any) error {
	return &Error{
		Key:  key,
		Args: args,
	}
}

func Wrap(key string, cause error, args ...any) error {
	return &Error{
		Key:   key,
		Args:  args,
		Cause: cause,
	}
}
```

service 使用：

```go
return nil, apperror.New(adminenums.ErrAdminNotFound)
```

带参数的错误：

```go
remaining := time.Until(*entity.LockedUntilTime).Round(time.Minute).String()
return nil, apperror.New(adminenums.ErrAccountLocked, remaining)
```

HTTP 层翻译时直接用 `i18n.GetText` 和 `i18n.GetHttpCode`。缓存命中返回翻译，缓存未命中返回 key 本身和默认状态码。

数据库文案：

| item_key | lang | item_value | category | http_code |
|---|---|---|---|---:|
| `ErrAccountLocked` | `zh-CN` | `账号已被锁定，请 %s 后重试` | `error` | 423 |
| `ErrAccountLocked` | `en-US` | `Account locked, retry in %s` | `error` | 423 |

## 8. 请求语言

### 8.1 来源优先级

推荐语言优先级：

1. 用户个人语言设置，后续按需支持；
2. `Accept-Language` 请求头；
3. `i18n.default_lang`；
4. 最终回退到 `zh-CN`。

### 8.2 规范化

请求头可能是：

```text
en-US,en;q=0.9,zh-CN;q=0.8
```

不能把整段字符串直接传给 `i18n.GetText()`。语言中间件需要：

1. 解析语言及权重；
2. 与系统支持语言匹配；
3. 选出一个规范语言；
4. 写入 `gin.Context`。

示例：

```go
const ContextLanguageKey = "language"

func LanguageMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := resolveSupportedLanguage(c.GetHeader("Accept-Language"))
		c.Set(ContextLanguageKey, lang)
		c.Next()
	}
}
```

业务 service 不读取 `gin.Context`，也不接收语言参数。语言只属于入站协议层。

## 9. HTTP 错误输出

模块 HTTP 层增加统一错误出口：

```go
func writeError(c *gin.Context, err error) {
	lang := requestLanguage(c)

	var businessErr *apperror.Error
	if errors.As(err, &businessErr) {
		text := i18n.GetText(businessErr.Key, lang)
		message := fmt.Sprintf(text, businessErr.Args...)
		httpCode := i18n.GetHttpCode(businessErr.Key)
		response.ErrorWithMessage(c, httpCode, message)
		return
	}

	// 记录真实底层错误，对外返回稳定系统错误。
	message := i18n.GetText("ErrSystemError", lang)
	response.ErrorWithMessage(c, http.StatusInternalServerError, message)
}
```

`i18n.GetText` 缓存命中返回翻译，缓存未命中返回 key 本身。`i18n.GetHttpCode` 缓存命中返回 `sys_i18n.http_code`，未命中返回默认值 200。

handler 调用需要更精确的状态码时，可以在 `writeError` 中覆盖默认值：

```go
res, err := h.as.Detail(c.Request.Context(), &req)
if err != nil {
	writeError(c, err)
	return
}
```

这样 `ErrAdminNotFound` 可以由 `sys_i18n.http_code = 404` 决定状态码，不再被 handler 错误地统一返回为 `500`。

参数绑定错误不需要进入 service：

```go
if err := c.ShouldBindJSON(&req); err != nil {
	message := i18n.GetText(adminenums.MsgBadRequest, requestLanguage(c))
	response.ErrorWithMessage(c, http.StatusBadRequest, message)
	return
}
```

底层数据库、Redis、网络错误不能直接把 `err.Error()` 暴露给前端；应记录原始错误，并返回稳定的系统错误资源。

## 10. Go 枚举与字典资源

### 10.1 固定业务枚举

涉及业务判断的状态必须由 Go 控制合法值。

例如管理员状态：

```go
type AdminStatus int

const (
	AdminStatusActive   AdminStatus = 1
	AdminStatusDisabled AdminStatus = 2
	AdminStatusBanned   AdminStatus = 3
)

func (s AdminStatus) I18nKey() string {
	switch s {
	case AdminStatusActive:
		return "dict_admin_status_normal"
	case AdminStatusDisabled:
		return "dict_admin_status_disabled"
	case AdminStatusBanned:
		return "dict_admin_status_banned"
	default:
		return "dict_admin_status_unknown"
	}
}
```

业务表继续保存：

```text
1
2
3
```

不要将 `dict_admin_status_normal` 写入管理员状态字段。

职责：

- Go 枚举规定合法值和状态转换；
- 业务表保存稳定业务值；
- `sys_i18n` 保存状态的多语言显示文本。

### 10.2 动态轻量选项

如果选项不参与核心业务判断，可以直接由 `sys_i18n` 管理，例如普通客户来源：

```text
dict_crm_customer_source_website
dict_crm_customer_source_referral
dict_crm_customer_source_exhibition
```

这类选项可以按资源前缀批量读取，但不能在列表中逐条访问数据库或远程服务。

### 10.3 复杂字典边界

出现以下字段时，不再继续扩展 `sys_i18n`：

- 父子层级；
- 图标；
- 标签颜色；
- 手工排序；
- 默认选项；
- 复杂扩展参数；
- 折扣、金额等计算规则；
- 核心状态机定义。

此时应建立所属业务模块的专用表，`sys_i18n` 只保存显示文本。

## 11. 前端资源使用

`sys_i18n` 可以保存 UI 文本，但前端不能每显示一个文本就请求一次后端。

推荐模式：

```text
前端启动或切换语言
    ↓
按 module + lang 批量获取资源包
    ↓
合并到 vue-i18n messages
    ↓
页面使用 key 本地读取
```

例如：

```text
GET /i18n/resources?module=admin&lang=zh-CN
```

返回：

```json
{
  "ui_admin_btn_save": "保存",
  "ui_admin_btn_delete": "删除",
  "dict_admin_status_normal": "正常"
}
```

静态框架文案可以继续保留在前端 `locales` 中；需要后台动态维护和多个系统共享的资源，再由 `sys_i18n` 下发。

## 12. 缓存与刷新

当前采用全量内存缓存，适合现有规模。

读取路径：

```text
业务请求
    ↓
pkg/i18n 内存缓存
    ↓ 未命中时按降级规则返回
默认语言或 key
```

资源更新路径：

```text
管理端更新 sys_i18n
    ↓
显式调用 Reload 或等待自动刷新
    ↓
构建新的完整 Map
    ↓
原子替换内存缓存
```

现阶段不需要 Redis，也不需要拆成微服务。

只有多个独立系统需要共享同一资源中心并独立部署时，才考虑：

```text
业务服务本地缓存
    ↓ 未命中或版本更新
Redis/资源包缓存
    ↓
i18n 服务
    ↓
MySQL
```

是否微服务化由部署和复用边界决定，而不是由字典行数决定。

## 13. 表结构一致性

当前 `category`、`http_code`、`status`、`remark` 会在每种语言记录中重复。

例如同一个 key 可能被误配置为：

```text
zh-CN http_code = 403
en-US http_code = 400
```

当前加载器按扫描顺序取第一个 `http_code`，这会造成不确定行为。

第一阶段保持现有表，但必须增加以下规则：

- 同一 `item_key` 的 `category` 必须一致；
- 同一 `item_key` 的 `http_code` 必须一致；
- 同一 `item_key` 的 `status` 必须一致；
- 更新资源时以事务同时保存所有语言；
- 缓存加载时检测元数据冲突，发现冲突应返回初始化错误；
- `category != error` 时，`http_code` 固定使用 `200`。

未来出现资源审核、版本发布、翻译协作时，再规范化为：

```text
sys_i18n_resource
  id
  item_key
  category
  http_code
  status
  remark

sys_i18n_translation
  resource_id
  lang
  item_value
```

当前数据量不需要立即拆表。

## 14. 降级规则

i18n 查不到翻译时直接返回 key 本身，这是 `GetText` 已有行为，不需要额外机制。

降级顺序：

1. 查询请求语言；
2. 请求语言不存在时查询默认语言；
3. 默认语言不存在时返回其他可用语言；
4. key 不在 `sys_i18n` 表中时返回 key 本身；
5. `http_code` 不在表中时返回默认值 200。

```text
请求 ErrAdminNotFound / en-US
    ↓
en-US 命中         → Admin not found
    ↓ 未命中
默认 zh-CN 命中    → 管理员不存在
    ↓ key 未录入 sys_i18n
返回 key 本身      → ErrAdminNotFound
```

key 是语义化的（`ErrAdminNotFound`、`ErrAccountLocked`），缓存中查不到 key 时用户看到 key 也能理解错误含义。

i18n 是 Critical 组件，启动时加载失败应用不会启动。正常运行期间缓存一定可用，查不到只可能是某个 key 没录入 `sys_i18n`，返回 key 本身即可。

## 15. 分阶段实施

### 阶段一：管理员模块闭环

1. 将 `admin/enums` 中文常量值替换为稳定 key；
2. 补齐管理员模块的中英文 `sys_i18n` 资源；
3. 增加语言解析中间件；
4. 增加轻量业务错误结构；
5. 管理员 handler 使用统一错误输出；
6. 业务错误优先使用 `sys_i18n.http_code`，key 未录入时返回默认状态码；
7. 补管理员状态 `banned` 字典资源。

验证目标：

- `zh-CN` 请求返回中文；
- `en-US` 请求返回英文；
- 不支持语言正确回退；
- `ErrAccountLocked` 参数正确格式化；
- `ErrAdminNotFound` 返回 404，不再返回 500；
- key 未录入 `sys_i18n` 时返回 key 本身，不影响请求继续处理；
- 请求过程中不查询 `sys_i18n` 数据库表。

### 阶段二：公共能力收敛

1. 提取公共语言读取辅助函数；
2. 提取模块可复用的业务错误写出函数；
3. 逐模块迁移硬编码业务文案；
4. 清理历史 `pkg/enums` 依赖；
5. 加入缺失资源检测。

注意：这里只复用技术机制，模块资源 key 仍由各自 `enums` 管理。

### 阶段三：资源管理和前端资源包

1. 增加资源管理接口；
2. 增加按模块、语言批量读取；
3. 前端切换语言时加载资源包；
4. 资源更新后主动刷新缓存；
5. 需要时增加资源版本号。

## 16. 测试要求

至少覆盖：

### `pkg/i18n`

- 指定语言命中；
- 默认语言回退；
- key 不在缓存中时 `GetText` 返回 key 本身；
- key 不在缓存中时 `GetHttpCode` 返回默认值 200；
- 禁用资源不进入缓存；
- `http_code` 正确读取；
- 同 key 元数据冲突时初始化失败；
- 自动刷新后资源更新；
- 并发读取期间缓存替换安全。

### 语言中间件

- 单语言请求头；
- 带权重的多语言请求头；
- 不支持语言；
- 空请求头；
- 大小写和空格规范化。

### 管理员 feature 链路

- 验证码错误的中英文响应；
- 用户名或密码错误的中英文响应；
- 管理员禁用响应；
- 管理员锁定及动态参数；
- 管理员不存在返回 404；
- 参数错误返回 400；
- 底层系统错误不泄露内部错误内容。

## 17. 最终结论

`sys_i18n` 可以继续作为综合性的全局文本与轻量选项资源中心。

当前架构不需要重写 `pkg/i18n`，也不需要微服务化。优先完成以下数据链路：

```text
模块 enums：只定义稳定 key
    ↓
service：返回 key + 参数
    ↓
HTTP 层获取请求语言
    ↓
pkg/i18n 从缓存翻译，缓存未命中返回 key 本身
    ↓
pkg/response 输出最终文案
```

必须长期保持的边界：

- Go 枚举控制核心业务合法值；
- `sys_i18n` 只控制显示内容；
- `pkg/response` 不负责翻译；
- service 不依赖 Gin，也不读取请求语言；
- 动态业务实体内容不进入全局资源表；
- 前端和跨服务调用必须批量加载资源，禁止逐 key 远程查询。