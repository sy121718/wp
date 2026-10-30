package feature

// seo_audit_json_test.go — 产物 SEO 体检端点（POST /api/publication/seo-audit）的链路证据。
//
// 模块内用例（service 与 inbound/http 的单元用例）证形状与取词，它们不带中间件、
// 不接真库。这里证它们证不了的部分：
//
//	Session → CSRF → Casbin（publication:seo_audit + 虚拟对象 /api/seo/audit 的 seo:audit）
//	→ handler → service → PG → 产物文件
//
// 中间件链与 internal/routers/assembly.go 的 authorizedAPI 同构，含 ActorContextMiddleware
// （装配处有它，装置就必须有它 —— 装置与生产不同构时断言的是另一个系统）。
//
// 产物目录走 GO_WP_ARTIFACT_ROOT（pipeline.DefaultArtifactRoot 的正式覆盖入口，
// ActiveRoot 由它派生），不写仓库内的 public/runtime。

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go_wp/internal/middleware/adminctx"
	builtin "go_wp/internal/middleware/builtin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	pubenums "go_wp/internal/module/publication/enums"
	pubhttp "go_wp/internal/module/publication/inbound/http"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/response"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 本包的两条权限点（与生产同值）。判权只看 (sub, obj, act)，code 列只用于文案与菜单树。
const (
	seoAuditRealPath  = "/api/publication/seo-audit"
	seoAuditPerm      = "publication:seo_audit"
	seoAuditVirtual   = "/api/seo/audit"
	seoAuditLegacyObj = "seo:audit"

	// seoAuditPlainAdmin 无任何角色的普通管理员（is_admin=0）。
	//
	// 名字带 publication 前缀：本包与 navigation/user 等包共用同一批测试库不会串，
	// 但运维本地库若被误用，账号名是能一眼看出归属的最后一道线索。
	seoAuditPlainAdmin = "pub-seo-audit-plain"

	// csrfRejectText CSRF 中间件拒绝时写进 message 的那句话。
	// 出处：internal/middleware/builtin/csrf.go 的 response.ErrorWithMessage(c, 403, "CSRF 校验失败")。
	// 用它区分「CSRF 拒绝」与「权限拒绝」——只断言状态码时两者不可分。
	csrfRejectText = "CSRF 校验失败"

	// seoAuditProjectA / seoAuditProjectB 两个互相隔离的工程。
	seoAuditProjectA = "aaaaaaaa-0000-0000-0000-00000000a001"
	seoAuditProjectB = "bbbbbbbb-0000-0000-0000-00000000b001"

	// 两条激活路径，A / B 各一条且**不重叠**。
	//
	// 不造首页（"/"）：两个工程都声明 "/" 会同时踩 page_routes 的主键 (project_id, path)
	// 与「同一 page_id 只属于一页」，还会让「谁的产物算谁的」混进路径覆盖的语义里 ——
	// 串工程缺陷会被另一种冲突掩盖。每个用例再按需增量补自己的路径。
	seoAuditAPathAbout = "/a-about"
	seoAuditBPathDocs  = "/b-docs"
)

// seoAuditChain 装配完成的真实链路与它的登录态。
type seoAuditChain struct {
	Engine *gin.Engine
	DB     *gorm.DB
	Super  *support.AdminSession
	Plain  *support.AdminSession
	// PlainID 普通管理员的主键（Casbin 的 sub 就是它的十进制形式）。
	PlainID int64
	// ArtifactRoot 本用例独占的产物根（GO_WP_ARTIFACT_ROOT 指向它）。
	ArtifactRoot string
}

// newSEOAuditChain 装配与生产同构的链路，并为两个工程各落两条激活路由。
//
// 顺序是有讲究的，颠倒会造出「反例的前提不存在」的假绿：
//
//	db —— 生产迁移建表（不手抄 DDL）
//	→ 超管（031/050 按 is_admin=1 逐行授策略，超管必须先存在）
//	→ 普通管理员（必须在 seed 之前建，否则它也会吃到超管策略）
//	→ RunSeeds（业务权限点与策略落库）
//	→ InitCasbin（从库构建 enforcer）
//	→ 引擎装配 + 登录
func newSEOAuditChain(t *testing.T) *seoAuditChain {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}
	if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
		t.Fatalf("准备测试超管失败: %v", err)
	}
	plainID := seoAuditSeedPlainAdmin(t, db)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行业务权限 seed 失败: %v", err)
	}
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := pkgcasbin.Close(); err != nil {
			t.Errorf("关闭 Casbin 失败: %v", err)
		}
		_ = captcha.Close()
	})

	// 两个工程行：page_routes.project_id 有 FK → projects(id)，激活行必须先有工程。
	support.SeedProjectRow(t, db, seoAuditProjectA, "体检工程 A")
	support.SeedProjectRow(t, db, seoAuditProjectB, "体检工程 B")

	// 本用例独占产物根。必须在装配引擎之前设好：ActiveRoot 在 RunSEOAudit 里**每次调用**
	// 读一次环境变量，但产物文件要在请求前就摆好，所以这里的时序是「先建目录、再发请求」。
	artifactRoot := t.TempDir()
	t.Setenv("GO_WP_ARTIFACT_ROOT", artifactRoot)

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		GinMode:         gin.TestMode,
		UseDefaultRoute: false,
		// 组件由本用例显式装配（DB 是隔离库）；InitComponents 会去连生产库与默认 Redis。
		InitComponents: false,
		RouteRegistrar: func(engine *gin.Engine) {
			// 相对路径以**本包目录**为基准：public/test/publication/feature → 上升四级是仓库根。
			engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(api)
			adminAuthz := adminhttp.SetupAdminRoutes(permission.NewRouteGroup(api), db)
			// /admin 页面组：中间件链与 assembly.go 一致（Session + CSRF + 权限上下文）。
			// 本用例不断言页面，但登录后拿到的 CSRF token 来自 admin 模块的登录接口，
			// 页面组存在与否不影响它 —— 这里保留是为了让装置与生产同形，避免「少挂一组
			// 于是某些策略从未被加载」这类装置差异。
			_ = engine.Group("/admin",
				builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
				adminctx.PermContextMiddleware(adminAuthz))

			// /api 链与 assembly.go:504 的 authorizedAPI 逐字同构。
			// publication 路由组自身只挂 SessionAuthMiddleware，判权全靠组级 Casbin ——
			// 少了 Casbin 这层，「无策略 403」永远测不出来（拿到的会是 handler 自己的 4xx）。
			authorizedAPI := permission.NewRouteGroup(
				api.Group("", builtin.SessionAuthMiddleware(), builtin.ActorContextMiddleware(),
					builtin.CSRFMiddleware(), builtin.CasbinMiddleware()))

			// 返回值不使用：本用例不需要经契约调业务方法（造数走隔离库句柄）。
			_ = pubhttp.SetupPublicationRoutes(authorizedAPI, db)
		},
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	// 会话存储是 Redis。失败语义按 support 的严格开关走，与全仓其它 feature 一致：
	//   GOWP_REQUIRE_REDIS=1 → AcquireRedis 内部已 t.Fatalf（failIfRequired），走不到这里；
	//   否则                → Skip，而不是把一个环境缺失伪装成本用例失败。
	// 无条件 Fatal 会让「本机没起 Redis」变成红，那与严格开关的用意相反。
	redisAddr, redisSource, err := support.AcquireRedis(t)
	if err != nil {
		t.Skipf("登录链路需要真实 Redis 会话存储，本地与容器两级都不可用（GOWP_REQUIRE_REDIS 未置 1）: %v", err)
	}
	if err := support.SetupRedisForTestAt(t, redisAddr); err != nil {
		// 拿到地址却初始化失败属于真实缺陷（不是环境缺失），保持失败。
		t.Fatalf("初始化测试 Redis（%s，来源 %s）失败: %v", redisAddr, redisSource, err)
	}

	// 每个工程各一条激活路由；产物与更多路由由各用例按需增量造。
	// 装配阶段不摆任何产物：摆了什么、scanned 该是几，必须由用例自己说清，
	// 否则「scanned=1」会依赖一个看不见的默认摆设。
	if err := seedSeoAuditRoutes(t, db, seoAuditProjectA, seoAuditAPathAbout); err != nil {
		t.Fatalf("准备工程 A 激活路由失败: %v", err)
	}
	if err := seedSeoAuditRoutes(t, db, seoAuditProjectB, seoAuditBPathDocs); err != nil {
		t.Fatalf("准备工程 B 激活路由失败: %v", err)
	}

	return &seoAuditChain{
		Engine:       engine,
		DB:           db,
		Super:        seoAuditLogin(t, engine, support.TestAdminUsername, "超管"),
		Plain:        seoAuditLogin(t, engine, seoAuditPlainAdmin, "普通管理员"),
		PlainID:      plainID,
		ArtifactRoot: artifactRoot,
	}
}

// seoAuditSeedPlainAdmin 落一个**无任何角色**的普通管理员（is_admin=0）。
func seoAuditSeedPlainAdmin(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(support.TestAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	if err := db.Exec("DELETE FROM sys_admin WHERE username = ?", seoAuditPlainAdmin).Error; err != nil {
		t.Fatalf("清理普通管理员测试账号失败: %v", err)
	}
	// id 交给序列：固定 id 与既有 seed 撞上时的冲突是静默跳过的（SeedTestAdmin 已 setval）。
	if err := db.Exec(`INSERT INTO sys_admin (username, password, status, is_admin)
		VALUES (?, ?, 1, 0)`, seoAuditPlainAdmin, string(hash)).Error; err != nil {
		t.Fatalf("写入普通管理员测试账号失败: %v", err)
	}
	var row struct {
		ID      int64
		IsAdmin int
	}
	if err := db.Raw("SELECT id, is_admin FROM sys_admin WHERE username = ?", seoAuditPlainAdmin).
		Scan(&row).Error; err != nil {
		t.Fatalf("读取普通管理员测试账号失败: %v", err)
	}
	if row.ID == 0 || row.IsAdmin != 0 {
		t.Fatalf("普通管理员测试账号前置不成立: id=%d is_admin=%d", row.ID, row.IsAdmin)
	}
	if row.ID == 1 {
		// 超管固定是 id=1（SeedTestAdmin 写在前面）。普通管理员若也拿到 1，
		// 两个身份就共用同一个 Redis 会话键 user:session:1，反例的前提消失。
		t.Fatalf("普通管理员拿到了超管的 id=1，会话键冲突")
	}
	return row.ID
}

// seedSeoAuditRoutes 为一个工程写入若干 active 路由行。
//
// 用**本用例的隔离库句柄**直接写，不经 service、也不从路由返回值反取 Model()：
// 路由注册函数的返回类型是模块契约（只列业务方法），拿它类型断言回实现属于
// 越级穿透，且会把「装配层换了实现」变成用例的隐性依赖。
//
// 写的是与审计同库的隔离库（NewMigratedPGTestDB 的返回值），所以「造数据的库」
// 与「被审计的库」天然是同一个 —— 这点不需要靠 service 句柄来保证。
func seedSeoAuditRoutes(t *testing.T, db *gorm.DB, projectID string, paths ...string) error {
	t.Helper()

	routeDB := db.Model(&pubmodel.RouteEntity{})
	for _, p := range paths {
		// page_routes 要求归属者恰好一个非空（init_builder_schema.sql 的 CHECK），
		// 这里给页面归属；page_id 是 uuid 列，不能拿 path 直接当 id。
		pageID := seoAuditPageID(p)
		if err := routeDB.Create(&pubmodel.RouteEntity{
			ProjectID: projectID,
			Path:      p,
			PageID:    strPtr(pageID),
			RouteKind: pubmodel.RouteActive,
			UpdatedAt: timeNow(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// seoAuditPageID 按路径生成一个稳定的页面 uuid（用例内部唯一即可，不跨用例共享）。
//
// page_routes.page_id 是**逻辑引用、不设外键**：预留路由时实体尚未创建
// （init_builder_schema.sql 的列注释 + 迁移 053 的 drop_route_page_fk）。
// 所以这里不建 pages 行，只要形状是合法 uuid 且不重复就够。
// 用「路径 → 确定性 uuid」而不是随机值：同一路径重复建行时得到同一个 id，
// 用例不会因为插入顺序不同而产生不同数据。
func seoAuditPageID(path string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("seo-audit/"+path)).String()
}

// seoAuditArtifactHTML 一份产物 HTML 的体检相关字段。
type seoAuditArtifactHTML struct {
	Title       string
	Description string
	Canonical   string
	// ImageSrc 非空时补一张**缺 alt** 的图片（用于验证参数只带原值、不带句子）。
	ImageSrc string
	// Links 追加站内链接（用于验证死链只带 href 原值）。
	Links []string
}

// writeSeoAuditArtifact 在激活目录里按访问面的真实形状摆一份产物。
//
// 形状与 pipeline.LocalPublicationStore.relActivePath 一致：/about → active/about/index.html，
// 首页（"/"）→ active/index/index.html。**照真源写**，自己发明一套形状的测试绿了也不代表线上对。
// 这里用真实目录而不是符号链接：CollectActiveDocuments 的符号链接分支已有模块内用例覆盖
// （service/seo_audit_test.go 的 TestCollectActiveDocumentsFollowsActiveSymlinks），
// 本用例要验的是「哪些路径进了体检」，不是「怎么穿过链接读到文件」。
func writeSeoAuditArtifact(t *testing.T, root, accessPath string, page seoAuditArtifactHTML) {
	t.Helper()

	rel := strings.Trim(strings.TrimSpace(accessPath), "/")
	if rel == "" {
		rel = "index" // 首页的激活条目名是 index，不是 index.html
	}
	dir := filepath.Join(root, "public", "active", filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建立产物目录 %s 失败: %v", dir, err)
	}

	var b strings.Builder
	b.WriteString("<!doctype html><html><head>")
	if page.Title != "" {
		b.WriteString("<title>" + htmlEscape(page.Title) + "</title>")
	}
	if page.Description != "" {
		b.WriteString(`<meta name="description" content="` + htmlEscape(page.Description) + `">`)
	}
	if page.Canonical != "" {
		b.WriteString(`<link rel="canonical" href="` + htmlEscape(page.Canonical) + `">`)
	}
	b.WriteString("</head><body>")
	if page.ImageSrc != "" {
		// 刻意不带 alt：这是 image_alt_missing 的触发条件。
		b.WriteString(`<img src="` + htmlEscape(page.ImageSrc) + `">`)
	}
	for _, href := range page.Links {
		b.WriteString(`<a href="` + htmlEscape(href) + `">链接</a>`)
	}
	b.WriteString("</body></html>")

	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("写入产物 %s 失败: %v", filepath.Join(dir, "index.html"), err)
	}
}

// htmlEscape 只做本用例需要的最小转义（标题与链接原文里可能出现引号或尖括号）。
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// seoAuditLogin 走真实登录链路拿会话 Cookie 与 CSRF token。
func seoAuditLogin(t *testing.T, engine *gin.Engine, username, label string) *support.AdminSession {
	t.Helper()

	sess, err := support.LoginAdminSession(t, engine, username, support.TestAdminPassword)
	if err != nil {
		t.Fatalf("%s（%s）登录失败: %v", label, username, err)
	}
	if sess.Cookie == "" || sess.CSRFToken == "" {
		t.Fatalf("%s（%s）登录未拿到完整会话: cookie=%q csrf_token=%q", label, username, sess.Cookie, sess.CSRFToken)
	}
	return sess
}

// seoAuditRequest 发一次真实请求（真中间件、真 handler）。
type seoAuditRequest struct {
	Method      string
	Query       map[string]string
	ContentType string
	// Body 原样作为请求体；nil 表示无体。
	Body    string
	Cookie  string
	CSRF    string
	Headers map[string]string
}

// seoAuditRawResponse 原始响应的关键面：状态码、Content-Type、体、是否跳转。
type seoAuditRawResponse struct {
	Status      int
	ContentType string
	Body        string
	Location    string
	// CacheControl 用于断言「无缓存指令泄漏」这类边角——本用例只记录不使用。
	CacheControl string
}

// doSeoAudit 发请求并把响应原样收下来（不解析，先把「是不是 JSON」这件事本身断言清楚）。
func doSeoAudit(t *testing.T, engine *gin.Engine, req seoAuditRequest) seoAuditRawResponse {
	t.Helper()

	method := req.Method
	if method == "" {
		method = http.MethodPost
	}
	target := seoAuditRealPath
	if len(req.Query) > 0 {
		q := url.Values{}
		for k, v := range req.Query {
			q.Set(k, v)
		}
		target += "?" + q.Encode()
	}

	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	httpReq := httptest.NewRequest(method, target, body)
	httpReq.RemoteAddr = "192.0.2.1:1234"
	if req.ContentType != "" {
		httpReq.Header.Set("Content-Type", req.ContentType)
	}
	if req.Cookie != "" {
		httpReq.Header.Set("Cookie", req.Cookie)
	}
	if req.CSRF != "" {
		httpReq.Header.Set("X-CSRF-Token", req.CSRF)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httpReq)

	out := seoAuditRawResponse{
		Status:       rec.Code,
		ContentType:  rec.Header().Get("Content-Type"),
		Body:         rec.Body.String(),
		Location:     rec.Header().Get("Location"),
		CacheControl: rec.Header().Get("Cache-Control"),
	}
	return out
}

// seoAuditEnvelope 端点的对外报文形状：{code,message,data}。
type seoAuditEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// decodeSeoAuditEnvelope 解析信封；解析失败直接失败（调用点都已断言过 200/4xx）。
func decodeSeoAuditEnvelope(t *testing.T, label string, res seoAuditRawResponse) seoAuditEnvelope {
	t.Helper()

	var env seoAuditEnvelope
	if err := json.Unmarshal([]byte(res.Body), &env); err != nil {
		t.Fatalf("%s：响应不是 {code,message,data} JSON（Content-Type=%q，体前 200 字节=%q）: %v",
			label, res.ContentType, firstN(res.Body, 200), err)
	}
	return env
}

// seoAuditData 端点的业务数据面（与冻结的 AuditResult 逐字段对应）。
//
// **不复用 service 里的类型**：这是对外契约，用生产类型解析会让「契约改了但用例跟着改」
// 变成静默通过。这里独立声明，字段名与 JSON tag 是对外承诺。
type seoAuditData struct {
	Scanned int `json:"scanned"`
	Count   int `json:"count"`
	Issues  []struct {
		Path   string `json:"path"`
		Item   string `json:"item"`
		Level  string `json:"level"`
		Params struct {
			Title string   `json:"title"`
			Src   string   `json:"src"`
			Href  string   `json:"href"`
			Paths []string `json:"paths"`
		} `json:"params"`
	} `json:"issues"`
}

// decodeSeoAuditData 解析 data 面。
func decodeSeoAuditData(t *testing.T, label string, env seoAuditEnvelope) seoAuditData {
	t.Helper()

	if len(env.Data) == 0 || string(env.Data) == "null" {
		t.Fatalf("%s：响应没有 data 字段（体=%q）", label, env.Message)
	}
	var data seoAuditData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("%s：data 不符合 {scanned,count,issues[]} 形状: %v（原=%s）", label, err, env.Data)
	}
	return data
}

// firstN 取前缀，用于把响应体塞进错误消息时截断（整页 HTML 会淹没日志）。
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// seoAuditMultipartBody 编一段真实的 multipart 请求体，返回体与 Content-Type。
//
// 为什么必须真编：`binding.JSON` 的分支由 Content-Type 决定，而 multipart 的边界串
// 写在 Content-Type 里。手写一段 `project=x` 配一个 multipart 头，请求会在解析阶段
// 就失败 —— 那时测到的是「报文坏了」，不是「multipart 传 project 能不能行」。
func seoAuditMultipartBody(t *testing.T, fields map[string]string) (string, string) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	// 键排序后写：multipart 的字段顺序会影响字节，但不应影响绑定结果；
	// 固定顺序让失败时的体是可复现的。
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := w.WriteField(k, fields[k]); err != nil {
			t.Fatalf("写入 multipart 字段 %s 失败: %v", k, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart 写入器失败: %v", err)
	}
	return buf.String(), w.FormDataContentType()
}

// assertSeoAuditJSON 断言「这是一次 JSON 响应、而不是页面或跳转」。
//
// 三件事一起断言，因为它们在旧实现里是同一个症状的三种表现：
//   - HX 分支渲染 HTML 片段 → Content-Type 是 text/html；
//   - 失败态用 200 承载 → 状态码不能区分「跑失败」与「跑通了没问题」；
//   - 页面出口发 302 → 客户端按 JSON 解析会崩。
func assertSeoAuditJSON(t *testing.T, label string, res seoAuditRawResponse, wantStatus int) {
	t.Helper()

	if res.Status != wantStatus {
		t.Fatalf("%s：状态码 %d，期望 %d（体前 200 字节=%q）", label, res.Status, wantStatus, firstN(res.Body, 200))
	}
	if !strings.Contains(strings.ToLower(res.ContentType), "application/json") {
		t.Fatalf("%s：Content-Type=%q，期望 JSON —— 非 JSON 意味着出口还是页面或片段", label, res.ContentType)
	}
	if res.Location != "" {
		t.Fatalf("%s：响应带 Location=%q —— 成功/失败都由前端决定导航，后端不发 302", label, res.Location)
	}
	if strings.Contains(strings.ToLower(res.Body), "<html") ||
		strings.Contains(strings.ToLower(res.Body), "<table") ||
		strings.Contains(strings.ToLower(res.Body), "data-seo-audit") {
		t.Fatalf("%s：响应体里出现 HTML 结构（%q）—— 端点只回事实 JSON", label, firstN(res.Body, 200))
	}
}

// seoAuditCall 发一次带超管凭证的请求并断言 JSON 形状，返回解析好的 data 面。
//
// 覆盖 90% 的调用点：认证层用例自己构造凭证，业务用例一律用它 —— 三行样板
// （doSeoAudit → assert → decode）在六个用例里重复出现，重复处最容易出现
// 「某处忘了断言 Content-Type」的漏判。
func seoAuditCall(t *testing.T, chain *seoAuditChain, label, body string, wantStatus int) seoAuditData {
	t.Helper()

	res := doSeoAudit(t, chain.Engine, seoAuditRequest{
		ContentType: "application/json",
		Body:        body,
		Cookie:      chain.Super.Cookie,
		CSRF:        chain.Super.CSRFToken,
	})
	assertSeoAuditJSON(t, label, res, wantStatus)
	if wantStatus != http.StatusOK {
		return seoAuditData{}
	}
	return decodeSeoAuditData(t, label, decodeSeoAuditEnvelope(t, label, res))
}

// ─────────────────────────────────────────────────────────────────────────────
// 用例 1：三传输 + query 回落 + 畸形 JSON + HX 无关性
// ─────────────────────────────────────────────────────────────────────────────

// TestSEOAuditAcceptsThreeTransportsAndEchoesJSONContract 同一个端点对
// JSON / urlencoded / multipart 三种传输一视同仁，且任意 HX-Request 组合都回 JSON。
//
// 为什么 HX 组合必须测：旧实现按 `HX-Request: true` 分流到 HTML 片段出口、
// 按 200 + 提示文案承载失败。只要那个分支还活着，前端就永远无法用状态码判断
// 「体检跑失败了」，写基座的失败提示也会落在片段上而不是 JSON 上。
func TestSEOAuditAcceptsThreeTransportsAndEchoesJSONContract(t *testing.T) {
	chain := newSEOAuditChain(t)

	type transport struct {
		name        string
		contentType string
		body        string
	}
	// 工程 A 的 /a-about 摆一份干净产物：本用例要证的是「三种传输都真的跑完体检」，
	// 干净页面让 scanned=1 / count=0 成为无歧义的读数（有结论也不影响结论，但会多一层解释）。
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 站关于页", Canonical: "https://a.example.com/a-about",
	})

	transports := []transport{
		{"JSON", "application/json", `{"project":"` + seoAuditProjectA + `"}`},
		{"urlencoded", "application/x-www-form-urlencoded", "project=" + url.QueryEscape(seoAuditProjectA)},
		{"multipart", "multipart/form-data", ""}, // body 由下面单独构造
	}

	for _, tr := range transports {
		for _, hx := range []string{"", "true"} {
			hx := hx
			name := tr.name
			if hx == "true" {
				name += "/HX"
			}
			t.Run(name, func(t *testing.T) {
				req := seoAuditRequest{
					ContentType: tr.contentType,
					Body:        tr.body,
					Cookie:      chain.Super.Cookie,
					CSRF:        chain.Super.CSRFToken,
				}
				if tr.name == "multipart" {
					// SendRequest 只负责 JSON 体；multipart 要真的编出来，否则测的是
					// 「没带 Content-Type 的落空请求」而不是「multipart 传 project」。
					body, ct := seoAuditMultipartBody(t, map[string]string{"project": seoAuditProjectA})
					req.Body, req.ContentType = body, ct
				}
				if hx == "true" {
					req.Headers = map[string]string{"HX-Request": "true", "Accept": "text/html"}
				}

				res := doSeoAudit(t, chain.Engine, req)
				assertSeoAuditJSON(t, tr.name, res, http.StatusOK)

				env := decodeSeoAuditEnvelope(t, tr.name, res)
				// 成功信封的 code = http.StatusOK（pkg/response/response.go 的 Success 以
				// http.StatusOK 同时作 HTTP 状态码与信封 code）。不是 200 说明走了别的出口。
				if env.Code != http.StatusOK {
					t.Fatalf("%s：成功信封 code=%d，期望 %d（体=%q）",
						tr.name, env.Code, http.StatusOK, firstN(res.Body, 200))
				}
				data := decodeSeoAuditData(t, tr.name, env)

				// 工程 A 只激活一条路径 /a-about，且本用例只给它摆了产物 ⇒ scanned 恒为 1。
				// 断言等式而不是「大于 0」：>1 说明站点级目录里别的工程的产物被计入。
				if data.Scanned != 1 {
					t.Fatalf("%s：scanned=%d，期望 1（只有 /a-about 有产物；"+
						"大于 1 说明站点级目录里 B 工程的产物被计入）", tr.name, data.Scanned)
				}
				if data.Count != len(data.Issues) {
					t.Fatalf("%s：count=%d 与 issues 长度 %d 不一致 —— Count 必须等于 len(Issues)",
						tr.name, data.Count, len(data.Issues))
				}
			})
		}
	}
}

// TestSEOAuditQueryFallbackOnlyWhenBodyProjectBlank 请求体工程去空白后为空时才回落到查询串。
//
// 这条行为是既有输入契约的一部分（页面直链带查询串），职责迁移不该顺手改掉；
// 同时「body 优先」必须成立 —— 否则前端明确的表单意图会被 URL 上的旧值覆盖。
func TestSEOAuditQueryFallbackOnlyWhenBodyProjectBlank(t *testing.T) {
	chain := newSEOAuditChain(t)

	// 两工程各自只激活一条路径，scanned 都是 1，光看计数分不出走了哪个工程。
	// 所以让两边的结论**可区分**：A 的 /a-about 放一张缺 alt 的图，B 的 /b-docs 放一条死链。
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 站关于页", Canonical: "https://a.example.com/a-about",
		ImageSrc: "/assets/a.png",
	})
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditBPathDocs, seoAuditArtifactHTML{
		Title: "B 文档", Description: "B 站文档页", Canonical: "https://b.example.com/b-docs",
		Links: []string{"/b-missing"},
	})

	cases := []struct {
		name      string
		body      string
		query     map[string]string
		wantPath  string
		wantItem  string
		wantParam func(path string) string
	}{
		{
			name:      "body 给出工程：忽略查询串",
			body:      `{"project":"` + seoAuditProjectA + `"}`,
			query:     map[string]string{"project": seoAuditProjectB},
			wantPath:  seoAuditAPathAbout,
			wantItem:  "image_alt_missing",
			wantParam: func(string) string { return "/assets/a.png" },
		},
		{
			name:      "body 工程为空白：回落查询串",
			body:      `{"project":"   "}`,
			query:     map[string]string{"project": seoAuditProjectB},
			wantPath:  seoAuditBPathDocs,
			wantItem:  "internal_link_broken",
			wantParam: func(string) string { return "/b-missing" },
		},
		{
			name:      "body 完全没给工程：回落查询串",
			body:      `{}`,
			query:     map[string]string{"project": seoAuditProjectB},
			wantPath:  seoAuditBPathDocs,
			wantItem:  "internal_link_broken",
			wantParam: func(string) string { return "/b-missing" },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := doSeoAudit(t, chain.Engine, seoAuditRequest{
				ContentType: "application/json",
				Body:        tc.body,
				Query:       tc.query,
				Cookie:      chain.Super.Cookie,
				CSRF:        chain.Super.CSRFToken,
			})
			assertSeoAuditJSON(t, tc.name, res, http.StatusOK)
			data := decodeSeoAuditData(t, tc.name, decodeSeoAuditEnvelope(t, tc.name, res))
			var hit bool
			for _, issue := range data.Issues {
				if issue.Path == tc.wantPath && issue.Item == tc.wantItem {
					hit = true
					// 参数只带原值：图片带 src、死链带 href，且都等于产物里的原字符串。
					got := issue.Params.Src
					if tc.wantItem == "internal_link_broken" {
						got = issue.Params.Href
					}
					if got != tc.wantParam(tc.wantPath) {
						t.Fatalf("%s：%s 的参数是 %q，期望原值 %q —— 参数位只放从产物读到的原值",
							tc.name, tc.wantItem, got, tc.wantParam(tc.wantPath))
					}
				}
			}
			if !hit {
				t.Fatalf("%s：结论里没有 {%s, %s} —— 工程选取走错了分支（实际结论 %+v）",
					tc.name, tc.wantPath, tc.wantItem, data.Issues)
			}
		})
	}
}

// TestSEOAuditMalformedJSONIs400WithoutFallback 畸形 JSON 立刻 400，不回落成一次「跑通的体检」。
//
// 旧实现用 binding.Form 绑表单，畸形体在表单绑定里等同于「没带 project」，
// 于是回落到查询串或空工程 —— 前者静默跑了另一个工程，后者报「缺少工程」。
// 两种都不对：客户端的报文是坏的，就该说报文是坏的。
func TestSEOAuditMalformedJSONIs400WithoutFallback(t *testing.T) {
	chain := newSEOAuditChain(t)

	// 只列**当前实现确实拒绝**的两种报文。尾部残留（`{"project":"x"}{`）不在本组：
	// gin 的 binding 用 encoding/json 的 Decoder.Decode，只解第一个值、不查 EOF，
	// 这种报文会被接受 —— 那是全仓 binding 的共享缺口，不在本域伪设成 400。
	cases := []struct {
		name string
		body string
	}{
		{"截断的 JSON", `{"project":`},
		{"类型不匹配", `{"project":{"id":"x"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := doSeoAudit(t, chain.Engine, seoAuditRequest{
				ContentType: "application/json",
				Body:        tc.body,
				// 查询串给了合法工程：如果实现回落了，这一次会变成 200 并真的跑一遍体检。
				Query:  map[string]string{"project": seoAuditProjectA},
				Cookie: chain.Super.Cookie,
				CSRF:   chain.Super.CSRFToken,
			})
			assertSeoAuditJSON(t, tc.name, res, http.StatusBadRequest)

			env := decodeSeoAuditEnvelope(t, tc.name, res)
			if len(env.Data) > 0 && string(env.Data) != "null" {
				t.Fatalf("%s：400 响应不该带业务数据（data=%s）—— 带了说明体检还是跑了", tc.name, env.Data)
			}
			// 兜底文案是「请求参数无效」那一类，不能把 JSON 解析器的原文漏出去。
			lower := strings.ToLower(env.Message)
			for _, leak := range []string{"json:", "unexpected", "syntax error", "invalid character", "offset"} {
				if strings.Contains(lower, leak) {
					t.Fatalf("%s：400 文案里出现解析器原文 %q（完整文案=%q）", tc.name, leak, env.Message)
				}
			}
		})
	}
}

// TestSEOAuditMissingProjectIs400FromDeclaringSite body 与 query 都没给工程时 400，
// 文案来自 enums 的声明处（ErrProjectRequired），状态码也在声明处给。
//
// 断言的是「声明处的 key 与兜底」而不是某一句中文：文案随语言变，fact 不变。
func TestSEOAuditMissingProjectIs400FromDeclaringSite(t *testing.T) {
	chain := newSEOAuditChain(t)

	res := doSeoAudit(t, chain.Engine, seoAuditRequest{
		ContentType: "application/json",
		Body:        `{}`,
		Cookie:      chain.Super.Cookie,
		CSRF:        chain.Super.CSRFToken,
	})
	assertSeoAuditJSON(t, "缺工程", res, http.StatusBadRequest)

	env := decodeSeoAuditEnvelope(t, "缺工程", res)
	if env.Code != http.StatusBadRequest {
		t.Fatalf("缺工程：信封 code=%d，期望 %d —— 信封 code 与 HTTP 状态码同源", env.Code, http.StatusBadRequest)
	}
	if strings.TrimSpace(env.Message) == "" {
		t.Fatal("缺工程：400 响应的 message 为空 —— 用户看到的是一个没有任何说明的失败")
	}
	// 「文案来自声明处」的可验证形式：拿声明处那条 error 走与出口同一条取词函数，
	// 结果必须与本响应逐字相同。比中文字面量会把用例绑在某个语言上；
	// 比取词结果才是形状判据 —— handler 自己拼一句、或换个 key，这里都会红。
	wantMsg := response.MessageOf(nil, "", pubenums.ErrProjectMissing())
	if env.Message != wantMsg {
		t.Fatalf("缺工程：文案=%q，期望声明处 ErrProjectMissing 的取词结果 %q —— "+
			"文案与状态码都要在 enums 声明，出口只调 response.ErrorStatusAuto", env.Message, wantMsg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 用例 2：两工程隔离（真 PG + 站点级产物目录）
// ─────────────────────────────────────────────────────────────────────────────

// TestSEOAuditKeepsTwoProjectsIsolatedInSharedActiveRoot 两个工程共用一个站点级激活目录时，
// 一个工程的体检结论里不得出现另一个工程的路径。
//
// 为什么这条必须在链路层测：RunSEOAudit 的过滤逻辑是
//
//	paths := ListActivePaths(projectID)   // 本工程的激活路径
//	docs  := CollectActiveDocuments(ActiveRoot(), paths)  // 站点级目录
//	mine  := docs 里 Path ∈ paths 的那些
//
// 「过滤还在不在」在手改集合时看不出来 —— 把 mine 的过滤删掉，scanned 会从 1 变成 2，
// 但每个工程的结论各自看着都「有内容」。必须有跨工程的路径断言。
func TestSEOAuditKeepsTwoProjectsIsolatedInSharedActiveRoot(t *testing.T) {
	chain := newSEOAuditChain(t)

	// 两个工程各摆一份产物，都故意带一条指向**本工程**路径的死链：
	//   A：/a-about → /a-about-dead（不存在）
	//   B：/b-docs  → /b-docs-dead（不存在）
	// 若工程过滤失效，A 的结论里会同时出现 /b-docs 这个 Path，两条死链也会互相「变活」。
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 描述", Canonical: "https://a.example.com/a-about",
		Links: []string{"/a-about-dead"},
	})
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditBPathDocs, seoAuditArtifactHTML{
		Title: "B 文档", Description: "B 描述", Canonical: "https://b.example.com/b-docs",
		Links: []string{"/b-docs-dead"},
	})

	audit := func(t *testing.T, projectID string) seoAuditData {
		t.Helper()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        `{"project":"` + projectID + `"}`,
			Cookie:      chain.Super.Cookie,
			CSRF:        chain.Super.CSRFToken,
		})
		assertSeoAuditJSON(t, projectID, res, http.StatusOK)
		return decodeSeoAuditData(t, projectID, decodeSeoAuditEnvelope(t, projectID, res))
	}

	dataA := audit(t, seoAuditProjectA)
	dataB := audit(t, seoAuditProjectB)

	pathsOf := func(data seoAuditData) []string {
		seen := map[string]bool{}
		var out []string
		for _, issue := range data.Issues {
			if !seen[issue.Path] {
				seen[issue.Path] = true
				out = append(out, issue.Path)
			}
		}
		sort.Strings(out)
		return out
	}
	hrefsOf := func(data seoAuditData) []string {
		var out []string
		for _, issue := range data.Issues {
			if issue.Params.Href != "" {
				out = append(out, issue.Params.Href)
			}
		}
		sort.Strings(out)
		return out
	}

	// scanned 的等式：每个工程恰好一份产物在站点级目录里。
	// 站点级目录里此刻共有 2 份产物（A 一份、B 一份）——
	// scanned 必须是 1 而不是 2，这是过滤是否生效最直接的读数。
	if dataA.Scanned != 1 {
		t.Fatalf("工程 A 的 scanned=%d，期望 1 —— 站点级激活目录里有 %d 份产物，"+
			"多出来的那份是另一个工程的（工程过滤缺口）", dataA.Scanned, 2)
	}
	if dataB.Scanned != 1 {
		t.Fatalf("工程 B 的 scanned=%d，期望 1 —— 同上，工程过滤缺口", dataB.Scanned)
	}

	// 结论归属：A 的结论只挂在 /a-about 上，B 的只挂在 /b-docs 上。
	if got, want := pathsOf(dataA), []string{seoAuditAPathAbout}; !equalStrings(got, want) {
		t.Fatalf("工程 A 的结论路径 %v，期望 %v —— 出现了别的工程的路径", got, want)
	}
	if got, want := pathsOf(dataB), []string{seoAuditBPathDocs}; !equalStrings(got, want) {
		t.Fatalf("工程 B 的结论路径 %v，期望 %v —— 出现了别的工程的路径", got, want)
	}

	// 死链只对照本工程的路由表：A 只该报 /a-about-dead，B 只该报 /b-docs-dead。
	// 若 activePath 集合把两个工程并起来，两条死链都会「变成活的」而一条都不报。
	if got, want := hrefsOf(dataA), []string{"/a-about-dead"}; !equalStrings(got, want) {
		t.Fatalf("工程 A 的死链 %v，期望 %v —— 内链对照用了错误的路径集合（并集会漏报，串工程会误报）", got, want)
	}
	if got, want := hrefsOf(dataB), []string{"/b-docs-dead"}; !equalStrings(got, want) {
		t.Fatalf("工程 B 的死链 %v，期望 %v —— 同上", got, want)
	}

	// 重复标题是**跨页面**的结论，它的 Params.Paths 同样不得跨工程：
	// 两个工程各有页面标题不同，这里不该出现 title_duplicate。
	for _, data := range []seoAuditData{dataA, dataB} {
		for _, issue := range data.Issues {
			if issue.Item == "title_duplicate" {
				t.Fatalf("出现了 title_duplicate（Paths=%v）—— 两个工程各只有一页标题，重复只能来自把两工程的产物混在一起",
					issue.Params.Paths)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 用例 4：会话 / CSRF / 双权限的真实链
// ─────────────────────────────────────────────────────────────────────────────

// TestSEOAuditRequiresSessionCSRFAndBothPermissions 沿真实中间件链逐层剥离凭证与权限，
// 断言每一层的**真实状态码**与 JSON 形状，并确认没有任何一层用 302 代替拒绝。
//
// 这一层是「四层判定」的现场证据：
//
//	无 Cookie                → 401（SessionAuthMiddleware，未登录）
//	有 Cookie、无 CSRF       → 403（CSRFMiddleware）
//	有 Cookie+CSRF、无任一权限 → 403（Casbin，两处都要命中）
//	两个权限都有             → 进入 handler（本用例用合法工程，得 200）
//
// 为什么必须走真实链而不是直接调 handler：模块内用例不带中间件，
// 它们能证明形状与取词，却证明不了「没带 CSRF 发不出去」。策略只授一半时的拒绝
// 更是只有装了两层 Casbin 才会出现 —— 少了 CasbinMiddlewareForPath 那一层，
// 「只有 publication:seo_audit」会静默放行。
func TestSEOAuditRequiresSessionCSRFAndBothPermissions(t *testing.T) {
	chain := newSEOAuditChain(t)

	// 前置：生产 seed 必须已经把两条策略的**权限点声明**落进 sys_permission。
	// 这里不重建台账，只核对它落地了 —— 台账缺一条时下面的判权断言会以「超管也 403」
	// 的形式失败，那种失败很难定位，先说清楚缺的是哪条。
	for _, item := range []struct{ code, path string }{
		{seoAuditPerm, seoAuditRealPath},
		{seoAuditLegacyObj, seoAuditVirtual},
	} {
		var n int64
		if err := chain.DB.Raw(
			`SELECT COUNT(*) FROM sys_permission
			  WHERE permission_code = ? AND api_path = ? AND api_method = 'POST' AND status = 1`,
			item.code, item.path).Scan(&n).Error; err != nil || n != 1 {
			t.Fatalf("权限点 %s 路径 %s 缺失: count=%d err=%v —— 生产 seed 没落这条（本用例不重写台账）",
				item.code, item.path, n, err)
		}
	}

	// 工程 A 的 /a-about 摆一份干净产物：下面的「双权限放行」要断言 scanned==1，
	// 而这个计数来自真实产物，不是来自某个默认摆设。
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 描述", Canonical: "https://a.example.com/a-about",
	})

	// 普通管理员是**无任何角色**的身份：它的放行只能来自本用例显式 AddPolicy 的两条策略，
	// 不会像超管那样被 is_admin=1 的策略整片覆盖。
	subject := fmt.Sprintf("%d", chain.PlainID)
	enforcer := pkgcasbin.GetEnforcer()
	if enforcer == nil {
		t.Fatal("Casbin enforcer 为 nil：InitCasbin 没有生效")
	}
	// 清理用 t.Cleanup：任何一条断言提前结束也要把策略摘掉，否则同一进程里
	// 后续用例会拿到一个「已授权」的普通管理员。
	dropPolicies := func() {
		for _, p := range [][]string{
			{subject, seoAuditRealPath, "POST", seoAuditPerm},
			{subject, seoAuditVirtual, "POST", seoAuditLegacyObj},
		} {
			if _, err := enforcer.RemovePolicy(p[0], p[1], p[2], p[3]); err != nil {
				t.Errorf("清理策略 %v 失败: %v", p, err)
			}
		}
	}
	t.Cleanup(dropPolicies)

	grant := func(path, perm string) {
		t.Helper()
		if _, err := enforcer.AddPolicy(subject, path, "POST", perm); err != nil {
			t.Fatalf("授予策略 (%s, %s) 失败: %v", path, perm, err)
		}
	}
	// 合法体：工程 A。每个反例都用它，才能保证「被拦住」只可能来自鉴权层 ——
	// 用空体的话，放行后 handler 自己也会 400，反例就失去了区分力。
	validBody := `{"project":"` + seoAuditProjectA + `"}`

	// 先授双权限，再逐层剥离凭证 —— 顺序是判据不是风格：
	// 缺 CSRF / 错 CSRF 这两例若在「无权限」状态下跑，403 可能来自 Casbin 而不是 CSRF，
	// 断言就变成了「某个层拒绝了」，而不是「CSRF 这层拒绝了」。先把权限给满，
	// 剩下的唯一变量才是凭证本身。
	grantBoth := func() {
		t.Helper()
		dropPolicies()
		grant(seoAuditRealPath, seoAuditPerm)
		grant(seoAuditVirtual, seoAuditLegacyObj)
	}

	t.Run("无会话 Cookie", func(t *testing.T) {
		grantBoth()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        validBody,
			// 有意只带 CSRF 头、不带 Cookie：会话缺失时 CSRF 也不该成为通行证。
			CSRF: chain.Plain.CSRFToken,
		})
		assertSeoAuditJSON(t, "无会话", res, http.StatusUnauthorized)
	})

	t.Run("有会话但缺 CSRF", func(t *testing.T) {
		grantBoth()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        validBody,
			Cookie:      chain.Plain.Cookie,
			// 不带 X-CSRF-Token，也不带 csrf_token 表单字段。
		})
		assertSeoAuditJSON(t, "缺 CSRF", res, http.StatusForbidden)
		// 三重证据区分「CSRF 拒绝」与「Casbin 拒绝」：
		//   ① 权限此刻已给满 ⇒ 403 不可能是缺权限；
		//   ② 文案是 CSRF 中间件那句（与 Casbin 的拒绝文案不同）；
		//   ③ 没有业务数据 ⇒ 请求没进 handler（进了的话合法工程会 200）。
		env := decodeSeoAuditEnvelope(t, "缺 CSRF", res)
		if env.Code != http.StatusForbidden {
			t.Fatalf("缺 CSRF：信封 code=%d，期望 %d", env.Code, http.StatusForbidden)
		}
		if !strings.Contains(env.Message, csrfRejectText) {
			t.Fatalf("缺 CSRF：文案=%q，期望含 %q —— 权限已授满，403 只应来自 CSRF 层",
				env.Message, csrfRejectText)
		}
		if len(env.Data) > 0 && string(env.Data) != "null" {
			t.Fatalf("缺 CSRF：拒绝响应里带了业务数据（data=%s）—— 请求已经跑到 handler 里去了", env.Data)
		}
	})

	t.Run("CSRF 错误值", func(t *testing.T) {
		grantBoth()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        validBody,
			Cookie:      chain.Plain.Cookie,
			CSRF:        "not-the-right-token",
		})
		assertSeoAuditJSON(t, "CSRF 错误", res, http.StatusForbidden)
		env := decodeSeoAuditEnvelope(t, "CSRF 错误", res)
		if !strings.Contains(env.Message, csrfRejectText) {
			t.Fatalf("CSRF 错误：文案=%q，期望含 %q —— 同样要证明是 CSRF 层拒绝的",
				env.Message, csrfRejectText)
		}
		if len(env.Data) > 0 && string(env.Data) != "null" {
			t.Fatalf("CSRF 错误：拒绝响应里带了业务数据（data=%s）", env.Data)
		}
	})

	t.Run("有会话有 CSRF 但两个权限都没有", func(t *testing.T) {
		// 这一例的变量是**权限**，所以凭证必须完整有效：CSRF token 直取会话里那一个，
		// 否则 403 可能来自 CSRF 层，「缺权限」这件事根本没被测到。
		dropPolicies()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        validBody,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		assertSeoAuditJSON(t, "无权限", res, http.StatusForbidden)
		// 与 CSRF 拒绝区分：文案不该是 CSRF 那句。
		env := decodeSeoAuditEnvelope(t, "无权限", res)
		if strings.Contains(env.Message, csrfRejectText) {
			t.Fatalf("无权限：文案=%q 是 CSRF 拒绝 —— 凭证有效时 403 必须来自权限层", env.Message)
		}
	})

	t.Run("只有 publication:seo_audit", func(t *testing.T) {
		dropPolicies()
		grant(seoAuditRealPath, seoAuditPerm)
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        validBody,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		// 这条是「双权限」的关键反例：路由注册处声明的那个码命中，
		// 但组内 CasbinMiddlewareForPath("/api/seo/audit") 那一层还要求 seo:audit。
		assertSeoAuditJSON(t, "只授 publication", res, http.StatusForbidden)
	})

	t.Run("只有 seo:audit", func(t *testing.T) {
		dropPolicies()
		grant(seoAuditVirtual, seoAuditLegacyObj)
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        validBody,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		assertSeoAuditJSON(t, "只授 seo:audit", res, http.StatusForbidden)
	})

	t.Run("两个权限都有", func(t *testing.T) {
		grantBoth()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        validBody,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		// 放行后 handler 拿到的是合法工程 ⇒ 200 且带 scanned。
		// 断言 200 而不是「不是 403」：只要不是 200 就说明放行没发生或 handler 坏了。
		assertSeoAuditJSON(t, "双权限", res, http.StatusOK)
		data := decodeSeoAuditData(t, "双权限", decodeSeoAuditEnvelope(t, "双权限", res))
		if data.Scanned != 1 {
			t.Fatalf("双权限：scanned=%d，期望 1 —— 鉴权放行后 handler 应真的跑完体检", data.Scanned)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 用例 3：两种「干净」的区分 + 重复标题不带单页归属
// ─────────────────────────────────────────────────────────────────────────────

// TestSEOAuditDistinguishesNoArtifactFromNoIssue scanned=0（没有产物）与
// scanned>0 且 count=0（跑过了、没问题）是两档结论，不能合并。
//
// 为什么重要：把两者并成一句「未发现问题」，会让**从未发布过**的站点看起来体检通过。
// 前端的两档空态（data-seo-audit-state="empty" / "clean"）正是靠这两个读数区分的，
// 后端若把 scanned=0 也写成 200 带 count=0 就算「一致」，前端只能猜。
func TestSEOAuditDistinguishesNoArtifactFromNoIssue(t *testing.T) {
	chain := newSEOAuditChain(t)

	// 工程 B 有一条激活路径 /b-docs，但本用例**不给它摆产物** ⇒ 有激活路径、没有产物。
	// 工程 A 的 /a-about 摆一份完全干净的产物 ⇒ 有产物、没问题。
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 描述", Canonical: "https://a.example.com/a-about",
	})

	resNoArtifact := doSeoAudit(t, chain.Engine, seoAuditRequest{
		ContentType: "application/json",
		Body:        `{"project":"` + seoAuditProjectB + `"}`,
		Cookie:      chain.Super.Cookie,
		CSRF:        chain.Super.CSRFToken,
	})
	assertSeoAuditJSON(t, "无产物", resNoArtifact, http.StatusOK)
	dataNoArtifact := decodeSeoAuditData(t, "无产物", decodeSeoAuditEnvelope(t, "无产物", resNoArtifact))
	if dataNoArtifact.Scanned != 0 || dataNoArtifact.Count != 0 {
		t.Fatalf("无产物：scanned=%d count=%d，期望 0/0", dataNoArtifact.Scanned, dataNoArtifact.Count)
	}
	// 空 Issues 编成 []，不是 null —— 前端 `data.issues.length` 与 `Array.isArray` 都依赖这一点。
	if !strings.Contains(string(decodeSeoAuditEnvelope(t, "无产物", resNoArtifact).Data), `"issues":[]`) {
		t.Fatalf("无产物：issues 必须编码为 []，实际 data=%s",
			decodeSeoAuditEnvelope(t, "无产物", resNoArtifact).Data)
	}

	// 工程 A：一份干净产物（title/description/canonical 齐、无图无链）。
	resClean := doSeoAudit(t, chain.Engine, seoAuditRequest{
		ContentType: "application/json",
		Body:        `{"project":"` + seoAuditProjectA + `"}`,
		Cookie:      chain.Super.Cookie,
		CSRF:        chain.Super.CSRFToken,
	})
	assertSeoAuditJSON(t, "干净", resClean, http.StatusOK)
	dataClean := decodeSeoAuditData(t, "干净", decodeSeoAuditEnvelope(t, "干净", resClean))
	if dataClean.Scanned != 1 || dataClean.Count != 0 {
		t.Fatalf("干净：scanned=%d count=%d，期望 1/0 —— 与「无产物」的 0/0 必须是两个读数",
			dataClean.Scanned, dataClean.Count)
	}
}

// TestSEOAuditDuplicateTitleCarriesNoSinglePageOwnership 重复标题的 Path 为空，
// Params.Paths 是全部命中路径（排序），不挑首项伪装成单页问题。
//
// 为什么这是硬断言：挑第一个命中路径写进 Path，等于给这条结论伪造一个归属页面 ——
// 运营照着改那一页，另外几页的标题还是没改。这条缺陷在服务端看起来「一切正常」，
// 只有前端把 Path 显示成「问题页面」时才会误导人，所以必须在契约层钉住。
func TestSEOAuditDuplicateTitleCarriesNoSinglePageOwnership(t *testing.T) {
	chain := newSEOAuditChain(t)

	const dupTitle = "同名标题"
	// 工程 A 再激活两条路径，与 /a-about 三个页面标题相同。
	// 路径名按字典序故意与写入顺序相反：若实现「取第一个 appends 到的」而不是排序，
	// 顺序断言会红。
	if err := seedSeoAuditRoutes(t, chain.DB, seoAuditProjectA, "/a-zzz", "/a-bbb"); err != nil {
		t.Fatalf("补工程 A 激活路由失败: %v", err)
	}
	for _, p := range []string{seoAuditAPathAbout, "/a-zzz", "/a-bbb"} {
		writeSeoAuditArtifact(t, chain.ArtifactRoot, p, seoAuditArtifactHTML{
			Title: dupTitle, Description: "描述", Canonical: "https://a.example.com" + p,
		})
	}

	res := doSeoAudit(t, chain.Engine, seoAuditRequest{
		ContentType: "application/json",
		Body:        `{"project":"` + seoAuditProjectA + `"}`,
		Cookie:      chain.Super.Cookie,
		CSRF:        chain.Super.CSRFToken,
	})
	assertSeoAuditJSON(t, "重复标题", res, http.StatusOK)
	data := decodeSeoAuditData(t, "重复标题", decodeSeoAuditEnvelope(t, "重复标题", res))

	var found int
	for _, issue := range data.Issues {
		if issue.Item != "title_duplicate" {
			continue
		}
		found++
		if issue.Path != "" {
			t.Fatalf("重复标题的 path=%q，期望空 —— 它不属于任何单页，挑首路径等于伪造归属", issue.Path)
		}
		if issue.Params.Title != dupTitle {
			t.Fatalf("重复标题的 params.title=%q，期望原值 %q", issue.Params.Title, dupTitle)
		}
		want := []string{"/a-bbb", seoAuditAPathAbout, "/a-zzz"}
		sort.Strings(want)
		if !equalStrings(issue.Params.Paths, want) {
			t.Fatalf("重复标题的 params.paths=%v，期望全部命中路径且已排序 %v（少一个就是藏页面，乱序就是没排序）",
				issue.Params.Paths, want)
		}
		if issue.Level != "warning" {
			t.Fatalf("重复标题的 level=%q，期望 warning", issue.Level)
		}
	}
	if found != 1 {
		t.Fatalf("重复标题应恰好报一条（三个同标题页面归为一条结论），实际 %d 条", found)
	}
}

// equalStrings 两个字符串切片逐元素相等（两侧都假定已排序）。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
