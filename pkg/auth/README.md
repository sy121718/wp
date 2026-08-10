# pkg/auth — JWT 认证 + Redis 会话管理

## 设计原则

**JWT 只管认证，Redis 管用户数据**，互不耦合。

| 职责 | 技术 | 特点 |
|---|---|---|
| 你是谁 | JWT | 无状态，验签名 + 过期时间 |
| 你能做什么 | Casbin | 策略引擎，独立于认证 |
| 你叫什么/权限/头像 | Redis | 有状态，可查可改 |
| 你还在不在 | Redis | 心跳 TTL 自动过期 |
| 你被踢了没 | Redis | 封禁时间戳即时生效 |

## 文件结构

```
pkg/auth/
├── jwt.go          # JWT 签发、解析、配置
├── session.go      # Redis 用户会话管理
└── README.md       # 本文件
```

## Redis 数据结构

### 1. 用户会话 — `user:session:{id}`

登录成功后写入，profile 接口从此读取。

```
Key:   user:session:1
Type:  String（JSON）
Value: {
  "id": 1,
  "username": "admin",
  "name": "管理员",
  "avatar": "https://...",
  "email": "admin@example.com",
  "phone": "13800138000",
  "status": 1,
  "is_admin": 1,
  "permissions": ["user:list", "user:create"]
}
TTL:   24h（登录时刷新）
```

### 2. 封禁标记 — `user:blocked:{id}`

管理员踢人时写入，JWT 中间件每次请求检查。

```
Key:   user:blocked:1
Type:  String
Value: 1740384000  （封禁时的 Unix 时间戳）
TTL:   到封禁到期自动删除
```

校验逻辑：`token 签发时间 < 封禁时间` → 拒绝。

### 3. 在线心跳 — `online:{id}`

每次请求由 JWT 中间件刷新。

```
Key:   online:1
Type:  String
Value: "1"
TTL:   5 分钟（每次请求刷新）
```

在线用户列表通过 `SCAN online:*` 获取。

## 数据流

### 登录

```
POST /api/admin/login
  ↓
验证密码 → auth.GenerateToken() → JWT
  ↓
查数据库获取用户信息 + 权限
  ↓
auth.SaveUserSession() → 写入 Redis
  ↓
响应头 X-New-Token: eyJ...
响应体 {code:200, message:"success"}
```

### 日常请求

```
请求 → Authorization: Bearer eyJ...
  ↓
JWTAuthMiddleware:
  1. ParseToken → 验签名 + 过期
  2. IsBlocked → 查 Redis 是否被封
  3. 放行 → c.Set("user_id", ...)
  ↓
c.Next() 后:
  token 剩余 < 10min → 续期 X-New-Token
  RefreshOnline() → 刷新在线心跳
```

### 踢下线

```
管理员点击"踢下线"
  ↓
auth.BlockUser(userID, time.Now())
  ↓
用户下次请求 → IsBlocked = true → 401
  ↓
前端收到 401 → 清内存变量 → 跳登录
```

### 获取用户信息

```
GET /api/admin/profile
  ↓
从 token 解析 user_id
  ↓
auth.GetUserSession(userID) → 读 Redis
  ├─ 命中 → 返回
  └─ 未命中 → 查数据库回源 → 写入 Redis → 返回
```

## 函数清单

| 函数 | 文件 | 用途 |
|---|---|---|
| `GenerateToken` | jwt.go | 签发单 token |
| `ParseToken` | jwt.go | 解析验证 JWT |
| `SaveUserSession` | session.go | 登录成功写入 Redis |
| `GetUserSession` | session.go | 读用户信息（profile） |
| `DeleteUserSession` | session.go | 退出登录清理 |
| `BlockUser` | session.go | 封禁用户 |
| `UnblockUser` | session.go | 解封用户 |
| `IsBlocked` | session.go | 检查封禁状态 |
| `RefreshOnline` | session.go | 刷新在线心跳 |
| `GetOnlineUsers` | session.go | 获取在线用户列表 |

## 配置

JWT 配置在 `config.yaml`：

```yaml
jwt:
  secret: your-secret-key
  expire_time: 24       # 小时，默认 24h
  issuer: go_wp
```

Redis 配置同样在 `config.yaml`：

```yaml
redis:
  host: 127.0.0.1
  port: 6379
  password: ""
  db: 0
  enabled: true
```

## 封禁 vs 等过期

| 方式 | 即时性 | 依赖 |
|---|---|---|
| Redis 封禁（当前方案） | 即时 | Redis |
| 等 JWT 自然过期 | 最长 24h | 无 |

Redis 不可用时，封禁功能暂时失效，但 JWT 认证不受影响。