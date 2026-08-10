# public/test 测试规范（草案）

本目录用于集中管理测试代码与测试资源，采用“按业务模块分组 + 按测试类型分层”的组织方式。

## 一、目标

- 测试代码集中放在 `public/test`，不分散到业务目录中查找。
- 目录结构与业务模块保持一致，便于定位与维护。
- 以接口链路测试（feature）为主，单元测试（unit）为补充。
- 使用 `go test` 按包运行，不依赖逐文件执行。

## 二、目录规划

```text
public/test/
├── backend/                         # 对应 internal/module/backend
│   ├── admin/
│   │   ├── feature/                 #用例测试 接口链路测试（主）
│   │   │   ├── admin_login_test.go
│   │   │   └── admin_user_crud_test.go
│   │   └── unit/                    # 模块规则单元测试（辅）
│   │       └── admin_rule_test.go
│   └── user/
│       ├── feature/
│       └── unit/
├── support/                         # 公共测试支撑（初始化、客户端、断言辅助）
│   ├── test_bootstrap.go
│   └── test_client.go
└── fixtures/                        # 测试数据（json/sql/yaml）
    ├── admin/
    └── user/
```

## 三、测试类型定义

### 1. feature（接口链路测试）

- 从 HTTP 接口入口触发。
- 覆盖路由、中间件、参数绑定、handler、service、model 的完整链路。
- 用于验证“业务场景是否可用”，例如登录成功、登录失败、权限不足等。

### 2. unit（单元测试）

- 测试单个函数或局部规则逻辑。
- 主要覆盖复杂规则、边界条件、异常分支。
- 不是临时代码，属于长期维护的回归资产。

## 四、命名规范

### 1. 目录命名

- 使用业务模块名：`admin`、`user`。
- 类型目录固定为：`feature`、`unit`。

### 2. 文件命名

- 使用“模块 + 场景”命名：`admin_login_test.go`、`user_profile_update_test.go`。
- 一个文件可包含同一场景下多个测试点。

### 3. 测试函数命名

- 统一格式：`Test<模块><场景><结果>`。
- 示例：
  - `TestAdminLoginSuccess`
  - `TestAdminLoginInvalidPassword`
  - `TestUserCreateForbidden`

## 五、用例组织方式

- 每个核心场景一个顶层 `TestXxx`。
- 场景内使用 `t.Run(...)` 切分测试点。
- 优先覆盖以下测试点：
正常与边界输入
场景：正常输入、最大值、最小值、空字符串、空数组。
预期：返回正确结果；边界值处理符合业务规则，不崩溃。

参数校验错误
场景：必传参数为空；类型不合法（如 string 传 int、nil）；格式不合法（JSON 解析失败、日期格式错误）。
预期：返回明确参数错误码与错误信息，不进入核心业务逻辑。

鉴权与权限不足
场景：未登录；无角色权限；无数据权限；token 过期或无效。
预期：返回未授权/禁止访问错误，不泄露敏感信息。

资源不存在
场景：查询记录不存在；依赖外部资源不存在（文件、缓存、数据库记录）。
预期：返回资源不存在错误，可区分“未找到”与“系统异常”。

状态冲突与并发问题
场景：并发更新导致版本冲突；当前状态不允许执行操作（如已支付不可取消）。
预期：返回状态冲突/状态非法错误，数据保持一致性。

异常与恶意输入（前端卡 bug）
场景：前端传异常结构、超大数据、恶意构造参数；超长字符串、特殊字符、注入攻击字符。
预期：请求被安全拦截或校验失败；服务稳定，无注入风险。

后端依赖与业务异常
场景：依赖服务失败（超时、重试失败）；数据库失败（约束冲突、锁等待）；业务规则不满足（余额不足、库存不足）。
预期：返回可识别业务/系统错误；必要时触发重试、降级或回滚。

## 六、公共支撑约定

- support/test_bootstrap.go：只做测试环境初始化与清理（配置、依赖装配、测试路由），不启动 main。
- support/test_client.go：统一封装请求构造、发送、响应解析，减少重复代码。
- fixtures/：只放静态测试数据（json/sql/yaml 等），不放业务逻辑和流程控制代码。

## 七、执行方式

```bash
# 运行全部测试
go test ./public/test/...

# 运行某模块 feature
go test ./public/test/backend/admin/feature -v

# 运行某模块 unit
go test ./public/test/backend/admin/unit -v

# 按函数名筛选
go test ./public/test/backend/admin/feature -run TestAdminLogin -v
```

## 八、实施顺序建议

1. 先完成 `feature` 冒烟链路（每模块 1~2 个核心接口）。
2. 再补充 `feature` 异常场景。
3. 最后按风险补 `unit`（复杂规则与边界逻辑）。

## 九、边界约束

- 测试代码统一放在 `public/test`，不要求分散到业务代码目录。
- 不启动生产服务进程进行测试，使用测试进程内构造的路由与依赖。
- 测试配置与开发配置隔离，避免误操作真实数据。
