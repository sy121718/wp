# pkg/auth — Session + Cookie 认证 + Redis 会话管理

## 设计原则

**Cookie 会话只管认证，Redis 管用户数据**，互不耦合。

| 职责 | 技术 | 特点 |
|---|---|---|
| 你是谁 | Cookie 会话（gin-contrib/sessions） | 签名防篡改，HTMX 请求自动携带 |
| 你能做什么 | Casbin | 策略引擎，独立于认证 |
| 你叫什么/权限/头像 | Redis | 有状态，可查可改 |
| 你还在不在 | Redis | 心跳 TTL 自动过期 |
| 你被踢了没 | Redis | 封禁时间戳即时生效 |

## 文件结构

```
pkg/auth/
├── cookie_session.go  # Cookie 会话存储（认证载体 + Init/Ready/Close）
├── session.go         # Redis 用户会话管理
└── README.md          # 本文件
```

## 认证流程

### 登录

```
POST /api/admin/login
  ↓
验证密码 → auth.NewSessionID() 生成会话 ID
  ↓
auth.SaveUserSession() → 写入 Redis（user:session:{id}）
  ↓
auth.SaveCookieSession() → Set-Cookie: gowp_session=...
  ↓
builtin.EnsureCSRFToken() → 在 cookie session 中写入 CSRF token
  ↓
响应体只返回成功，不再下发 token
```

### 日常请求

```
请求 → Cookie: gowp_session=...
  ↓
SessionAuthMiddleware:
  1. GetCookieSession → 读 user_id / username / session_id / issued_at
  2. IsBlocked → 查 Redis 是否被封禁
  3. GetUserSession → 校验 Redis 会话与会话 ID 一致
  4. 放行 → c.Set("user_id", int64) / c.Set("username")
  ↓
c.Next() 后:
  RefreshOnline() → 刷新在线心跳
```

### 登出

```
POST /api/admin/logout
  ↓
auth.DeleteUserSession() → 删除 Redis 会话
auth.ClearSession() → 删除 cookie（MaxAge=-1）
```

## Cookie 属性

| 属性 | 值 |
|---|---|
| HttpOnly | true（防 XSS 偷 cookie） |
| Secure | release 模式 true（生产环境） |
| SameSite | Lax（防 CSRF） |
| Path | / |
| 有效期 | 默认 24h，勾选记住我 7d |

## 函数清单

| 函数 | 文件 | 用途 |
|---|---|---|
| `Init` / `Ready` / `Close` | cookie_session.go | 会话存储生命周期 |
| `SaveCookieSession` | cookie_session.go | 登录成功写 cookie |
| `GetCookieSession` | cookie_session.go | 读 cookie 认证会话 |
| `ClearSession` | cookie_session.go | 退出登录清 cookie |
| `NewSessionID` | cookie_session.go | 生成会话 ID |
| `SaveUserSession` | session.go | 登录成功写入 Redis |
| `GetUserSession` | session.go | 读用户信息（profile） |
| `DeleteUserSession` | session.go | 退出登录清理 Redis |
| `BlockUser` / `UnblockUser` | session.go | 封禁 / 解封用户 |
| `IsBlocked` | session.go | 检查封禁状态 |
| `RefreshOnline` | session.go | 刷新在线心跳 |

## 配置

```yaml
auth:
  session_secret: your-secret-key  # Cookie 会话签名密钥，生产环境必须修改
```

Redis 配置：

```yaml
redis:
  host: 127.0.0.1
  port: 6379
  enabled: true
```
