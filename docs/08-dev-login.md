# 开发阶段一键登录（dev-login）

> 适用：本地开发、自动化端到端验证。**release 模式下这个功能不存在**，不是「关掉了」而是「没注册」。

## 它解决什么问题

后台每个页面都要登录，而登录必须过验证码。人肉点一次没什么，但有两类场景会反复卡住：

1. **用浏览器做端到端验证**（比如验证画布拖拽是否真的保存了坐标）—— 每次会话重建都要重登一次；
2. **换设备 / 清缓存 / 换浏览器** 后重新进后台调页面。

所以有了一个只在开发阶段存在的免密入口。

## 怎么用

```
http://127.0.0.1:8080/admin/dev-login
http://127.0.0.1:8080/admin/dev-login?to=/admin/mail/automation/canvas%3Fid%3D1
http://127.0.0.1:8080/admin/dev-login?user=admin
```

| 参数 | 说明 |
|---|---|
| `to` | 登录后跳转的站内路径（必须以 `/` 开头，缺省 `/admin`） |
| `user` | 指定超管用户名（缺省取 `is_admin=1` 里 id 最小的那个） |

debug 模式下登录页底部也会显示这个入口（`release` 下不显示，因为路由不存在，显示了就是死链）。

命令行的典型用法（登录 + 抓目标页）：

```bash
# 建会话并保存 cookie，然后带着 cookie 访问任意后台页
curl -s -c /tmp/c.txt 'http://127.0.0.1:8080/admin/dev-login?to=/admin' -o /dev/null
curl -s -b /tmp/c.txt 'http://127.0.0.1:8080/admin/mail/automation' | head
```

## 四道锁

这是个后门形状的东西，所以每一层都刻意收紧了：

| 锁 | 做法 | 为什么这样做 |
|---|---|---|
| 1. **release 下不存在** | `routers/routes.go` 里判断 `server.mode == debug` 才 `router.GET` | 不存在的路由无法被利用，比「注册了再在 handler 里判断」更可靠 |
| 2. **只认环回地址** | 用 `c.Request.RemoteAddr` 判断，**不是** `c.ClientIP()` | 后者受 `X-Forwarded-For` 影响；代理配置不当就能从外部伪造出「本机请求」 |
| 3. **只登超管** | 查询固定带 `is_admin = 1` | 它是「本机开发便利」，不是「任意管理员后门」 |
| 4. **走同一条会话路径** | `auth.NewSessionID` + `SaveUserSession` + `RefreshOnline` + `RotateCSRFToken` | 不做「直接塞 cookie 就放行」的旁路 —— 那样开发环境的会话行为与生产不一致，用它验出来的东西不算数 |

每次使用都会写日志（`logger.Scene("admin")` + `开发阶段一键登录（仅 debug 模式可用）`），便于事后核对。

另外 `to` 参数只接受站内路径：`//evil.com`、`/\evil.com` 这类协议相对 URL 一律回落到 `/admin`。
带凭据的入口上出现开放重定向尤其危险 —— 用户会以为自己登录的是本站。

## 排障

### 访问 /admin/dev-login 返回 404

`server.mode` 不是 `debug`。检查 `config.yaml`：

```yaml
server:
  mode: debug   # release 下这个路由不会被注册
```

### 返回 403「开发登录仅限本机访问」

请求不是从环回地址来的（远程开发、容器端口转发、反向代理都会出现）。
此时**不要**放宽判断 —— 正确做法是在本机（或 SSH 隧道到本机）访问。

### 登录成功但接口 403（无权限访问）

这不是 dev-login 的问题，是 Casbin 策略没跟上。两个已知坑（`routes.go` 头部也有记录）：

1. **策略在启动时载入内存，改库不会自动重载** —— 新接口的 seed 迁移必须配合进程重启才生效，
   表现为「策略已写进库、接口仍 403」；
2. **策略按 `v0 = user_id` 授权，`is_admin=1` 不会自动放行** —— 新建的管理员需要在 seed 或后台里单独授权。

新增权限点时，**权限点和超管策略要一起插**（参考 `public/migrations/133_mail_automation_layout_perm.sql`）：
超管策略是「权限点全表的快照」（见 `051_superadmin_all_policies.sql`），只加权限点不会自动补策略。

## 与自动化验证的关系

给 agent / 脚本做端到端验证时，推荐流程：

```bash
# 1. 一键建会话
curl -s -c /tmp/c.txt 'http://127.0.0.1:8080/admin/dev-login?to=/admin' -o /dev/null
# 2. 直接访问目标页面（带 cookie）
curl -s -b /tmp/c.txt 'http://127.0.0.1:8080/admin/mail/automation/canvas?id=1' | grep -c autoCanvas
```

浏览器自动化同理：先 `navigate` 到这个 URL，后续就是已登录状态。

**注意**：dev-login 只解决「进得去」。验证仍然必须打真实接口、看真实落库 ——
比如画布保存位置这件事，光看到「位置已保存」的提示不算验证通过，要查一次库确认坐标真的写进去了。
