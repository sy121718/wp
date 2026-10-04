package routers

// access_guard_wiring.go — AccessGuard「登录可见」的访客身份接线（PIPE-6）。
//
// 守卫中间件挂在访问面链上（middleware/builtin），而「这个请求登录了吗」只有
// user 模块答得出来。两者不能直接互相 import：user 的 inbound/http 已经反向依赖
// middleware/builtin（访客 cookie 会话复用 builtin.EnsureCSRFTokenWith），
// 同包再 import 回去就是环。
//
// 所以按仓库既有的端口注入形态接线（同 runtimefragment.Deps.VisitorIdentityMiddleware）：
// 模块提供能力，装配层决定接给谁。未注入时守卫对 members 类型**恒判未登录**
// （fail closed）：表现为「已登录访客也只看到守卫页」，而不是「谁都能看」。
//
// 访问面不变量（AGENTS.md 不变量 1 的第二条例外）：这个探针**只读** Redis 里的
// 访客会话 —— 不查 PostgreSQL、不写库、不续期。且只在「该条目确实带 members 守卫」
// 时才被调用（守卫先读 guard.json、再判身份），所以绝大多数静态请求零 Redis 访问。

import (
	"strings"

	"github.com/gin-gonic/gin"

	usercontract "go_wp/internal/module/user/contract"
	"go_wp/pkg/auth"
)

// 访客会话 cookie 名与令牌键。
//
// **与 user 模块的两个内部常量同值**（userhttp.userSessionCookieName =
// "gowp_user_session"、userSessionTokenKey = "user_token"），而它们没有导出 ——
// 这里只能镜像。放宽粒度地看，这是一处「跨包字面量耦合」，需要在改动时一起改；
// 之所以接受它而不是去改 user 模块的导出面：镜像失效的方向是 **fail closed**
// （cookie 名对不上 → 探针永远解析不到会话 → 已登录访客被当未登录 → 会员页显示
// 守卫页，问题肉眼可见），不会把受限内容放给未登录访客。
const (
	visitorSessionCookieNameMirror = "gowp_user_session"
	visitorSessionTokenKeyMirror   = "user_token"
)

// newVisitorLoginProbe 构造「这个访问面请求是否已登录」的探针。
//
// 返回 nil 表示探针不可用（服务未装配 / 会话存储未初始化）：调用方把 nil 直接
// 注入给守卫，守卫对 members 恒判未登录。
func newVisitorLoginProbe(svc usercontract.UserService) func(*gin.Context) bool {
	if svc == nil {
		return nil
	}
	// 与 user 模块共用同一个 cookie store 实例（pkg/auth.NamedCookieStore 按
	// cookie 名复用），因此签名密钥与选项口径天然一致 —— 各建一份会在密钥轮换时
	// 出现「后台认、守卫不认」这类只在轮换后才暴露的分叉。
	store, err := auth.NamedCookieStore(visitorSessionCookieNameMirror)
	if err != nil {
		return nil
	}
	return func(c *gin.Context) bool {
		if c == nil || c.Request == nil {
			return false
		}
		sess, serr := store.Get(c.Request, visitorSessionCookieNameMirror)
		if serr != nil {
			return false
		}
		raw, ok := sess.Values[visitorSessionTokenKeyMirror].(string)
		if !ok {
			return false
		}
		token := strings.TrimSpace(raw)
		if token == "" {
			return false
		}
		// ResolveVisitorID 只读 Redis（不查 PG、不续期）：访问面不因守卫产生写库。
		id, ok := svc.ResolveVisitorID(c.Request.Context(), token)
		return ok && id != 0
	}
}
