package builtin

// access_guard.go — AccessGuard 访问面守卫（PIPE-6）：密码保护 / 登录用户可见。
//
// 判定链（只看文件系统 + 一个签名 cookie，不查库、不执行模板）：
//
//	请求路径 →（去挂载前缀）→ 站点相对路径归一并拒越界（pipeline.CleanSiteRel）
//	         → 条目名（pipeline.EntryDir，**与访问面取文件的映射同源**）
//	         → os.Lstat 条目是符号链接？（不是 → 不是激活产物）
//	         → os.Readlink 得产物目录
//	         → 产物目录里有 guard.json？（有 → 本页受限）
//	         → 按类型判身份：password 验签名 cookie；members 读访客会话（只读 Redis）
//
// 第一安全要点是**条目名归一**：访问面既能用 `/about` 取到产物，也能用
// `/about/index.html` 取到同一份产物（http.FileServer 会跟随 `about` 这个
// 目录符号链接直出里面的 index.html）。守卫若只认前者，`GET /about/index.html`
// 就是一条一行就写完的绕过 —— 判 `/about` 受限、而静态面对显式文件名走
// 「产物内普通文件」分支原样直出。所以这里必须复用 pipeline.EntryDir，
// 而不是像 site_redirect.go 那样直接把 rel 拼到 ActiveRoot 上
// （重定向那条路对 `/about/index.html` 命中不了符号链接，只是少一次 301；
// 守卫漏掉同样一条，是把受限内容交出去）。
//
// 失败口径（与 redirect 刻意相反，别照抄它的写法）：
//   - guard.json 不存在 → 放行（判据就是「存在性」本身）；
//   - 存在但读不出/解析失败 → **fail closed**（当受限处理）；
//   - guard.html 读不到 → 内置兜底守卫页，**绝不放行**内容。
//
// 重定向解析失败只是少一次 301、内容仍然正确；守卫解析失败若按「无守卫」放行，
// 等于把受限内容交给未授权访客 —— 不可逆。两边代价不对称，判据就不能对称。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"
	"go_wp/pkg/auth"
)

// AccessUnlockPath 解锁端点路径。
//
// 必须**显式注册**（routers.setupStaticFace）：站点独占域名根之后，根挂载点的
// 静态面是 NoRoute，未显式注册的路径会被它当「站点里没有这个路径」吃掉
// （表现为守卫页提交后 404）。
const AccessUnlockPath = "/access/unlock"

const (
	// accessCookieName 解锁 cookie 名（前缀与购物车 gw_cart 同风格：gw_）。
	accessCookieName = "gw_access"
	// accessCookieMaxAgeSeconds 解锁有效期（12 小时）：够一次会话用，
	// 又不至于让一次借用的设备整天留着通行证。
	accessCookieMaxAgeSeconds = 12 * 3600
	// accessCookieVersion 载荷版本。格式变了就升版本，老 cookie 直接判废 ——
	// 比写兼容代码便宜，也比「解析出半个通行证」安全。
	accessCookieVersion = 1
	// maxAccessCookiePaths 单个 cookie 能记的条目上限。
	//
	// 上限是必需的：cookie 有 4KB 上限，且每多一条都要在每次请求上多比一次；
	// 无上限的输入等于把「cookie 长度」和「判定成本」交给发起者决定。
	// 满了按 FIFO 丢最早的一条 —— 解锁新的比留住旧的更接近用户意图。
	maxAccessCookiePaths = 20
	// maxAccessCookieBytes cookie 值长度上限（留足余量给同请求上的其它 cookie）。
	maxAccessCookieBytes = 3500
	// accessWrongPasswordStatus 未授权 / 密码错误的状态码。
	//
	// 403 而不是 401：401 在浏览器里会触发基础的认证弹窗（Basic 挑战），
	// 而在守卫页上那是全屏的系统框，把我们的提示挤掉。403 语义也准确 ——
	// 「身份已知、就是不给看」。
	accessWrongPasswordStatus = http.StatusForbidden
)

// accessLoginProbe 访客登录态探针（装配层注入，见 routers 的 access-guard 接线）。
//
// 为什么走注入而不是直接 import user 模块：middleware 被 user 模块的
// inbound/http 反向依赖（访客 cookie 会话用 builtin.EnsureCSRFTokenWith），
// 同一包再 import 回去就是环。注入点与 runtimefragment.SetVisitorIdentityMiddleware
// 同一形态：模块提供能力，装配层决定接线。
//
// 未注入（nil）时 members 类型**恒判未登录**（fail closed）—— 配置了
// 「登录可见」却没接上身份来源，表现为「登录了也看不到」，
// 而不是「谁都能看」。
var (
	accessProbeMu    sync.RWMutex
	accessLoginProbe func(*gin.Context) bool
)

// SetAccessGuardLoginProbe 注入访客登录态探针（装配期调用一次）。
func SetAccessGuardLoginProbe(fn func(*gin.Context) bool) {
	accessProbeMu.Lock()
	accessLoginProbe = fn
	accessProbeMu.Unlock()
}

// accessVisitorLoggedIn 调当前探针（未注入或 panic 一律按未登录）。
func accessVisitorLoggedIn(c *gin.Context) (loggedIn bool) {
	accessProbeMu.RLock()
	fn := accessLoginProbe
	accessProbeMu.RUnlock()
	if fn == nil {
		return false
	}
	// 探针要读 Redis；异常不能把静态请求打成 500，也不能让它「顺带放行」。
	defer func() {
		if r := recover(); r != nil {
			loggedIn = false
		}
	}()
	return fn(c)
}

// accessPayload 解锁 cookie 载荷。
type accessPayload struct {
	V int `json:"v"`
	// T 签发时间（Unix 秒）：decode 时与 accessCookieMaxAgeSeconds 对齐校验。
	T int64 `json:"t"`
	// F 密码指纹（pipeline.GuardFingerprint）：改密码即作废全部旧解锁。
	F string `json:"f"`
	// P 已解锁的**条目名**（不是 URL）：/about 与 /about/index.html 归一后
	// 是同一个 "about"，一次解锁两种写法都通。
	P []string `json:"p"`
}

// sitePathOf 条目名 → 站点路径（回跳与提示用）。
func sitePathOf(entry string) string {
	if entry == "" || entry == "index" {
		return "/"
	}
	return "/" + entry
}

// activeEntryDir 条目名 → 激活产物的绝对目录（非符号链接条目返回 ok=false）。
//
// 只看符号链接：激活一律是 symlink（LocalPublicationStore），真目录与普通文件
// 都不是激活产物 —— 与 site_redirect.go 同一判据。
func activeEntryDir(entry string) (dir string, ok bool) {
	if entry == "" {
		return "", false
	}
	link := filepath.Join(pipeline.ActiveRoot(), filepath.FromSlash(entry))
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	return filepath.Clean(filepath.Join(filepath.Dir(link), target)), true
}

// isInternalArtifactRequest 请求的是产物目录里的**内部文件**吗。
//
// 两类都必须拒绝直出：
//   - guard.json：含 bcrypt 哈希。产物目录在静态面下可达（`/about` 是符号链接，
//     `/about/guard.json` 会被「产物内普通文件」分支原样直出），不拦就是
//     把离线爆破的目标随页面一起发布。
//   - guard.html：守卫页只应由本中间件在未解锁时渲染（路径占位符要按请求注入）。
//     它不含秘密，但没有正当的抓取用途，而抓到的是一份占位符没替换的半成品。
//
// redirect.json 不在此列：它不含秘密，且「可直接读取」是既有的、被依赖的行为。
func isInternalArtifactRequest(clean string) bool {
	base := filepath.Base(filepath.FromSlash(clean))
	return base == pipeline.GuardMetaFileName || base == pipeline.GuardPageFileName
}

// AccessGuardMiddleware 访问面守卫中间件（挂在访问面链上，两个挂载点共用）。
//
// 排位：在 SiteRedirect 之后（重定向产物没有守卫，先 301 省一次读盘）、
// 在 SiteCache / StaticGzip / 静态文件处理**之前**（受限响应必须由这里终结，
// 且要自己下发 private, no-store 覆盖 site_cache.go 的 public, must-revalidate）。
func AccessGuardMiddleware(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		raw, inFace := SiteFaceRel(prefix, c.Request.URL.Path)
		if !inFace {
			c.Next()
			return
		}
		clean, ok := pipeline.CleanSiteRel(raw)
		if !ok {
			c.Next()
			return
		}
		root := pipeline.ActiveRoot()
		// 条目名必须用访问面**实际命中的那一个**（ResolveActiveEntry）：
		// `/about` 与 `/about/index.html` 取的是同一份产物，而页面 URL 真叫
		// `/foo/index.html` 时它又是另一份产物 —— 这条归一与静态面同源，
		// 是守卫不被「显式文件名」绕过的前提。
		entry, isEntry := pipeline.ResolveActiveEntry(root, clean)
		if !isEntry {
			// 不是激活条目：可能是产物内普通文件请求（/about/guard.json 会走到这里，
			// 因为 about 是符号链接、而 guard.json 本身不是）。
			if isInternalArtifactRequest(clean) {
				// 产物内部文件一律拒绝直出，不给响应体（它没有任何该被看到的内容）。
				// 显式 no-store：没有缓存指令的 403 会被浏览器按启发式规则缓存，
				// 而这条响应的语义是「永远别再这样请求」。
				c.Header("Cache-Control", "private, no-store")
				c.AbortWithStatus(accessWrongPasswordStatus)
				return
			}
			c.Next()
			return
		}
		dir, hasDir := activeEntryDir(entry)
		if !hasDir {
			// 条目刚被判为激活、却读不出链接目标：说明并发重发布中途删了它。
			// 交给静态面按「没有这个产物」处理（它同样会 404），不在这里放行内容。
			c.Next()
			return
		}
		sitePath := sitePathOf(entry)
		guard, gerr := pipeline.ReadGuardMeta(dir)
		if gerr != nil {
			// 有守卫但读不出来 → fail closed（当受限处理）。
			serveGuardDenied(c, nil, dir, sitePath, accessWrongPasswordStatus)
			return
		}
		if guard == nil {
			c.Next() // 无守卫：放行（判据就是「产物里有没有 guard.json」）
			return
		}
		if guardAllows(c, guard, entry) {
			c.Next()
			return
		}
		serveGuardDenied(c, guard, dir, sitePath, accessWrongPasswordStatus)
	}
}

// guardAllows 判定当前请求是否已通过守卫。
//
// 未知类型一律 false：产物里的类型由构建期写入（白名单），读到别的值说明
// 元数据被人改过或来自更早的版本 —— 那种情况下「放行」没有任何依据。
func guardAllows(c *gin.Context, guard *pipeline.GuardMeta, entry string) bool {
	switch guard.Type {
	case builder.AccessMembers:
		return accessVisitorLoggedIn(c)
	case builder.AccessPassword:
		p, ok := decodeAccessCookie(c, guard.Fingerprint)
		return ok && p.hasEntry(entry)
	default:
		return false
	}
}

// serveGuardDenied 直出守卫页并终结请求（锁定、不留缓存）。
//
// guard 为 nil（元数据缺失或损坏）时用内置兜底页：**兜底页也是拒绝**，
// 绝不因为「守卫页读不到」而放行内容。
func serveGuardDenied(c *gin.Context, guard *pipeline.GuardMeta, dir, sitePath string, status int) {
	meta := pipeline.GuardMeta{Type: builder.AccessPassword}
	if guard != nil {
		meta = *guard
	}
	c.Header("Cache-Control", "private, no-store")
	if dir != "" {
		if body, ok := pipeline.GuardPageBody(dir); ok {
			// 守卫页里的路径占位符按本次请求注入（产物是内容寻址的，
			// 同一份产物可能挂在多个路径上，构建期不知道自己的 URL）。
			c.Data(status, "text/html; charset=utf-8", pipeline.GuardPageWithPath(body, sitePath))
			c.Abort()
			return
		}
	}
	c.Data(status, "text/html; charset=utf-8", pipeline.RenderGuardPage(meta, sitePath, false))
	c.Abort()
}

// ---- 解锁 cookie ----

// accessCodec 解锁 cookie 编解码器（密钥来自部署的会话签名密钥）。
type accessCodec struct {
	secret []byte
}

// accessCookieCodec 取编解码器；密钥未就绪时 ok=false（判废，不放行）。
//
// 密钥与后台会话、购物车同源（auth.session_secret）：签名密钥属于部署，
// 不属于某个身份域；各造一份只会在轮换时漏掉其中一个。
func accessCookieCodec() (accessCodec, bool) {
	secret := strings.TrimSpace(auth.SessionSecret())
	if secret == "" {
		return accessCodec{}, false
	}
	return accessCodec{secret: []byte(secret)}, true
}

// encode 载荷 → 带签名的 cookie 值（base64url(JSON) + "." + hmac 前 16 字节）。
func (c accessCodec) encode(p accessPayload) (string, error) {
	if p.V == 0 {
		p.V = accessCookieVersion
	}
	if p.T == 0 {
		p.T = time.Now().Unix()
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(b)
	return body + "." + c.sign(body), nil
}

// sign 对载荷体签名（HMAC-SHA256 截断到 16 字节：够用且省 cookie 空间，
// 与购物车 cookie 同一取舍）。
func (c accessCodec) sign(body string) string {
	m := hmac.New(sha256.New, c.secret)
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

// decode 解析并校验 cookie 值；任何异常都返回 ok=false（调用方按「未解锁」处理）。
func (c accessCodec) decode(raw string) (p accessPayload, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxAccessCookieBytes {
		return accessPayload{}, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return accessPayload{}, false
	}
	// 常量时间比较：签名校验不该泄漏「前几位对上了」这种信息。
	if !hmac.Equal([]byte(parts[1]), []byte(c.sign(parts[0]))) {
		return accessPayload{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return accessPayload{}, false
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return accessPayload{}, false
	}
	if p.V != accessCookieVersion {
		return accessPayload{}, false
	}
	if p.T <= 0 || time.Now().Unix()-p.T > accessCookieMaxAgeSeconds {
		return accessPayload{}, false
	}
	if len(p.P) > maxAccessCookiePaths {
		return accessPayload{}, false
	}
	return p, true
}

// hasEntry 载荷里是否含某条目。
func (p accessPayload) hasEntry(entry string) bool {
	for _, x := range p.P {
		if x == entry {
			return true
		}
	}
	return false
}

// withEntry 追加条目（幂等；满了丢最早的一条）。
func (p accessPayload) withEntry(entry string) accessPayload {
	if p.hasEntry(entry) {
		return p
	}
	out := append([]string(nil), p.P...)
	out = append(out, entry)
	if len(out) > maxAccessCookiePaths {
		out = out[len(out)-maxAccessCookiePaths:]
	}
	p.P = out
	return p
}

// decodeAccessCookie 读当前请求的解锁 cookie（指纹不符或过期即判废）。
//
// 指纹校验是「改密码立刻生效」的全部实现：cookie 里带的是签发当时那份密码哈希的
// 指纹，与产物里当前的指纹不等就作废 —— 不需要额外的版本号或失效名单。
func decodeAccessCookie(c *gin.Context, fingerprint string) (accessPayload, bool) {
	codec, ok := accessCookieCodec()
	if !ok {
		return accessPayload{}, false
	}
	raw, err := c.Cookie(accessCookieName)
	if err != nil || strings.TrimSpace(raw) == "" {
		return accessPayload{}, false
	}
	p, ok := codec.decode(raw)
	if !ok {
		return accessPayload{}, false
	}
	if fingerprint == "" || p.F != fingerprint {
		return accessPayload{}, false
	}
	return p, true
}

// writeAccessCookie 写回解锁 cookie（合并既有条目）。
func writeAccessCookie(c *gin.Context, p accessPayload) error {
	codec, ok := accessCookieCodec()
	if !ok {
		return errors.New("会话签名密钥未就绪")
	}
	value, err := codec.encode(p)
	if err != nil {
		return err
	}
	secure := c.Request != nil && c.Request.TLS != nil
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(accessCookieName, value, accessCookieMaxAgeSeconds, "/", "", secure, true)
	return nil
}

// ---- 解锁端点 ----

// AccessUnlockHandler 解锁端点（POST /access/unlock）。
//
// # 为什么不需要 CSRF token
//
// 与购物车写路径同一套理由，三句话说完：
//  1. 会话状态落在**客户端签名 cookie** 里，服务端没有「当前访客的解锁集合」可被
//     跨站请求改写 —— 服务端只能把「请求自己带来的 cookie」解密、合并、原样写回；
//  2. SameSite=Lax 让**跨站 POST 不携带**本站 cookie，所以跨站表单拿到的是一张白纸，
//     既读不到既有解锁、也无法把攻击者的解锁「嫁接」到受害者身上；
//  3. 真正的门槛是**密码本身**：伪造解锁需要知道密码，而拿到密码就不需要伪造了。
//     签名保证「本站以外的实体造不出一份能通过校验的解锁 cookie」。
//
// 限流是必需的（挂在本路由上）：bcrypt 是慢哈希，但没有限流的登录类端点仍然是
// 「一次脚本跑一整本字典」的成本 —— 慢哈希只把爆破成本乘了个常数。
func AccessUnlockHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		clean, ok := pipeline.CleanSiteRel(strings.TrimSpace(c.PostForm("path")))
		if !ok {
			serveUnlockFailure(c, nil, "/", false)
			return
		}
		root := pipeline.ActiveRoot()
		entry, isEntry := pipeline.ResolveActiveEntry(root, clean)
		if !isEntry {
			serveUnlockFailure(c, nil, "/", false)
			return
		}
		// 回跳路径**由归一结果重新构造**，绝不原样回显表单里的字符串 ——
		// 原样回显就是一处开放重定向（`path=//evil.com`）。
		back := sitePathOf(entry)

		dir, hasDir := activeEntryDir(entry)
		if !hasDir {
			serveUnlockFailure(c, nil, back, false)
			return
		}
		guard, gerr := pipeline.ReadGuardMeta(dir)
		if gerr != nil || guard == nil {
			// 没有守卫的页面不提供解锁（否则任意路径都能换来一张通行证）。
			serveUnlockFailure(c, nil, back, false)
			return
		}
		if guard.Type != builder.AccessPassword {
			// members 类型不走密码解锁：它的凭据是访客会话，不是共享密码。
			serveUnlockFailure(c, guard, back, false)
			return
		}
		password := c.PostForm("password")
		if password == "" || len(password) > builder.MaxAccessPasswordBytes {
			// 超长直接拒：bcrypt 只吃前 72 字节，静默截断意味着
			// 「设了 100 字符密码，输前 72 字符就进得去」。同时不浪费一次慢哈希。
			serveUnlockFailure(c, guard, back, true)
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(guard.PasswordHash), []byte(password)) != nil {
			serveUnlockFailure(c, guard, back, true)
			return
		}

		// 成功：合并既有条目后写回签名 cookie。
		payload := accessPayload{V: accessCookieVersion, T: time.Now().Unix(), F: guard.Fingerprint}
		if existing, ok := decodeAccessCookie(c, guard.Fingerprint); ok {
			payload.P = existing.P
		}
		if err := writeAccessCookie(c, payload.withEntry(entry)); err != nil {
			serveUnlockFailure(c, guard, back, true)
			return
		}
		c.Redirect(http.StatusFound, back)
	}
}

// serveUnlockFailure 解锁失败响应：403 + 守卫页（可带「密码不正确」）。
//
// 一律 403 且响应体形状与成功前一致：不区分「这个路径不存在」与「密码错」，
// 避免把接口变成站点路径存在性探测器。
func serveUnlockFailure(c *gin.Context, guard *pipeline.GuardMeta, back string, showError bool) {
	meta := pipeline.GuardMeta{Type: builder.AccessPassword}
	if guard != nil {
		meta = *guard
	}
	c.Header("Cache-Control", "private, no-store")
	c.Data(accessWrongPasswordStatus, "text/html; charset=utf-8",
		pipeline.RenderGuardPage(meta, back, showError))
	c.Abort()
}
