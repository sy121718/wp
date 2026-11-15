package feature

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
	pubhttp "go_wp/internal/module/publication/inbound/http"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/templates"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	mw "go_wp/pkg/middleware"
	text "go_wp/pkg/view/jet"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

const (
	seoAuditRealPath  = "/api/publication/seo-audit"
	seoAuditPerm      = "publication:seo_audit"
	seoAuditVirtual   = "/api/seo/audit"
	seoAuditLegacyObj = "seo:audit"

	seoAuditPlainAdmin = "pub-seo-audit-plain"

	csrfRejectText = "CSRF 校验失败"

	seoAuditProjectA = "aaaaaaaa-0000-0000-0000-00000000a001"
	seoAuditProjectB = "bbbbbbbb-0000-0000-0000-00000000b001"

	seoAuditAPathAbout = "/a-about"
	seoAuditBPathDocs  = "/b-docs"
)

type seoAuditChain struct {
	Engine *gin.Engine
	DB     *gorm.DB
	Super  *support.AdminSession
	Plain  *support.AdminSession

	PlainID int64

	ArtifactRoot string
}

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
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
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

	support.SeedProjectRow(t, db, seoAuditProjectA, "体检工程 A")
	support.SeedProjectRow(t, db, seoAuditProjectB, "体检工程 B")

	artifactRoot := t.TempDir()
	t.Setenv("GO_WP_ARTIFACT_ROOT", artifactRoot)

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		GinMode:         gin.TestMode,
		UseDefaultRoute: false,

		InitComponents: false,
		RouteRegistrar: func(engine *gin.Engine) {

			engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			// 生产装配在模板中间件里挂请求级取词作用域；片段里的 t()/tn() 依赖它，
			// 缺了就会在 Execute 期失败并落成空响应。
			engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(mw.NewRegistry(), api)
			adminAuthz := adminhttp.SetupAdminRoutes(mw.NewRouteGroup(api, mw.RouteProtected), db)

			_ = engine.Group("/admin",
				builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
				adminctx.PermContextMiddleware(adminAuthz))

			authorizedAPI := mw.NewRouteGroup(
				api.Group("", builtin.SessionAuthMiddleware(), builtin.ActorContextMiddleware(),
					builtin.CSRFMiddleware(), builtin.CasbinMiddleware()), mw.RouteProtected)

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

	redisAddr, redisSource, err := support.AcquireRedis(t)
	if err != nil {
		t.Skipf("登录链路需要真实 Redis 会话存储，本地与容器两级都不可用（GOWP_REQUIRE_REDIS 未置 1）: %v", err)
	}
	if err := support.SetupRedisForTestAt(t, redisAddr); err != nil {

		t.Fatalf("初始化测试 Redis（%s，来源 %s）失败: %v", redisAddr, redisSource, err)
	}

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

func seoAuditSeedPlainAdmin(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(support.TestAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	if err := db.Exec("DELETE FROM sys_admin WHERE username = ?", seoAuditPlainAdmin).Error; err != nil {
		t.Fatalf("清理普通管理员测试账号失败: %v", err)
	}

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

		t.Fatalf("普通管理员拿到了超管的 id=1，会话键冲突")
	}
	return row.ID
}

func seedSeoAuditRoutes(t *testing.T, db *gorm.DB, projectID string, paths ...string) error {
	t.Helper()

	routeDB := db.Model(&pubmodel.RouteEntity{})
	for _, p := range paths {

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

func seoAuditPageID(path string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("seo-audit/"+path)).String()
}

type seoAuditArtifactHTML struct {
	Title       string
	Description string
	Canonical   string

	ImageSrc string

	Links []string
}

func writeSeoAuditArtifact(t *testing.T, root, accessPath string, page seoAuditArtifactHTML) {
	t.Helper()

	rel := strings.Trim(strings.TrimSpace(accessPath), "/")
	if rel == "" {
		rel = "index"
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

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

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

type seoAuditRequest struct {
	Method      string
	Query       map[string]string
	ContentType string

	Body    string
	Cookie  string
	CSRF    string
	Headers map[string]string
}

type seoAuditRawResponse struct {
	Status      int
	ContentType string
	Body        string
	Location    string

	CacheControl string
}

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

func seoAuditMultipartBody(t *testing.T, fields map[string]string) (string, string) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

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

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SEO 体检端点的协议契约：**HTML 片段**（不是 JSON），与 seo.html 的 HTMX 表单一致。
//
// 为什么要重写这一段：代码库里曾并存两套互斥的实现 —— 4f637ba4 把端点从 JSON 改为
// 「标准表单 → Jet 片段」，同时 seo.html 改成 `hx-post` + `data-seo-audit-result`，
// 模块内的 seo_audit_handle_test.go 也同步改成 HTML 契约；而这里的 JSON 断言
// （seoAuditEnvelope / decodeSeoAuditData / assertSeoAuditJSON）没有跟着改，
// 于是 8 条用例长期红着。任务书 §出口 的判据是「路由声明的真实出口形态」，
// 本文件按现行为重写断言，不把端点改回 JSON。
//
// 三条不变量：1) 任意 HX/Accept 组合都是 HTML 且无导航头；2) 失败按声明状态码出去、
// 原文不泄漏；3) 结论档位（无产物 / 干净 / 有问题）由模板判定并在 HTML 里可辨。

func assertSeoAuditHTML(t *testing.T, label string, res seoAuditRawResponse, wantStatus int) {
	t.Helper()

	if res.Status != wantStatus {
		t.Fatalf("%s：状态码 %d，期望 %d（体前 200 字节=%q）", label, res.Status, wantStatus, firstN(res.Body, 200))
	}
	if !strings.Contains(strings.ToLower(res.ContentType), "text/html") {
		t.Fatalf("%s：Content-Type=%q，期望 HTML 片段", label, res.ContentType)
	}
	if res.Location != "" {
		t.Fatalf("%s：响应带 Location=%q —— 结论由片段换入，后端不发 302", label, res.Location)
	}
	if res.CacheControl == "" {
		t.Fatalf("%s：缺少 Cache-Control —— 体检结果随产物变化，不能进缓存", label)
	}
}

// seoAuditState 从片段的 data-seo-audit-state 读结论档位；没有该标记时返回空串。
func seoAuditState(body string) string {
	const marker = `data-seo-audit-state="`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func seoAuditRun(t *testing.T, chain *seoAuditChain, label, projectID string, wantStatus int) seoAuditRawResponse {
	t.Helper()

	res := doSeoAudit(t, chain.Engine, seoAuditRequest{
		ContentType: "application/x-www-form-urlencoded",
		Body:        "project=" + url.QueryEscape(projectID),
		Cookie:      chain.Super.Cookie,
		CSRF:        chain.Super.CSRFToken,
	})
	assertSeoAuditHTML(t, label, res, wantStatus)
	return res
}

// TestSEOAuditAcceptsFormTransportsAndReturnsFragment 证明声明的表单编码
// （urlencoded / multipart）都能得到片段，且 HX-Request 头不改变出口形态
// （协议由路由声明，不由请求头协商）。
//
// JSON 是退役的兼容入口：端点已收窄成标准表单（write-contracts 记 form，
// encodings 为 urlencoded + multipart），JSON 必须 400，
// 而不是被静默当成空表单继续跑体检。
func TestSEOAuditAcceptsFormTransportsAndReturnsFragment(t *testing.T) {
	chain := newSEOAuditChain(t)

	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 站关于页", Canonical: "https://a.example.com/a-about",
	})

	for _, hx := range []string{"", "true"} {
		label := "urlencoded"
		if hx == "true" {
			label += "/HX"
		}
		t.Run(label, func(t *testing.T) {
			req := seoAuditRequest{
				ContentType: "application/x-www-form-urlencoded",
				Body:        "project=" + url.QueryEscape(seoAuditProjectA),
				Cookie:      chain.Super.Cookie,
				CSRF:        chain.Super.CSRFToken,
			}
			if hx == "true" {
				req.Headers = map[string]string{"HX-Request": "true", "Accept": "text/html"}
			}

			res := doSeoAudit(t, chain.Engine, req)
			assertSeoAuditHTML(t, label, res, http.StatusOK)
			if state := seoAuditState(res.Body); state != "clean" {
				t.Fatalf("%s：结论档位 %q，期望 clean（/a-about 的标题、描述、canonical 都齐全）",
					label, state)
			}
		})
	}

	t.Run("multipart 也在契约内", func(t *testing.T) {
		body, ct := seoAuditMultipartBody(t, map[string]string{"project": seoAuditProjectA})
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: ct,
			Body:        body,
			Cookie:      chain.Super.Cookie,
			CSRF:        chain.Super.CSRFToken,
		})
		assertSeoAuditHTML(t, "multipart", res, http.StatusOK)
		if state := seoAuditState(res.Body); state != "clean" {
			t.Fatalf("multipart：结论档位 %q，期望 clean（同一个 project 的产物集）", state)
		}
	})
}

// TestSEOAuditJSONBodyIsRejected 钉子：收窄成标准表单后，JSON 提交必须 400 而不是被静默接受。
// 页面表单发的是 formdata；JSON 体在这里被显式拒绝。
func TestSEOAuditJSONBodyIsRejected(t *testing.T) {
	chain := newSEOAuditChain(t)

	res := doSeoAudit(t, chain.Engine, seoAuditRequest{
		ContentType: "application/json",
		Body:        `{"project":"` + seoAuditProjectA + `"}`,
		Cookie:      chain.Super.Cookie,
		CSRF:        chain.Super.CSRFToken,
	})
	assertSeoAuditHTML(t, "JSON 不再接受", res, http.StatusBadRequest)
	if !strings.Contains(res.Body, "data-ui-error") {
		t.Fatalf("JSON 提交应得到共享失败片段，实际 %q", firstN(res.Body, 200))
	}
}

// TestSEOAuditProjectComesFromFormOnly 证明工程只从表单 body 取：
// query 不再补 body（表单入口只读 body，不合并查询串）。
func TestSEOAuditProjectComesFromFormOnly(t *testing.T) {
	chain := newSEOAuditChain(t)

	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 站关于页", Canonical: "https://a.example.com/a-about",
	})
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditBPathDocs, seoAuditArtifactHTML{
		Title: "B 文档", Description: "B 站文档页", Canonical: "https://b.example.com/b-docs",
	})

	t.Run("body 给出工程", func(t *testing.T) {
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        "project=" + url.QueryEscape(seoAuditProjectA),
			Cookie:      chain.Super.Cookie,
			CSRF:        chain.Super.CSRFToken,
		})
		assertSeoAuditHTML(t, "body 工程", res, http.StatusOK)
		if state := seoAuditState(res.Body); state != "clean" {
			t.Fatalf("结论档位 %q，期望 clean（工程 A 的产物齐全）", state)
		}
	})

	t.Run("query 不能补 body", func(t *testing.T) {
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        "",
			Query:       map[string]string{"project": seoAuditProjectB},
			Cookie:      chain.Super.Cookie,
			CSRF:        chain.Super.CSRFToken,
		})
		assertSeoAuditHTML(t, "query 不补 body", res, http.StatusBadRequest)
	})
}

// TestSEOAuditMissingProjectIs400FromDeclaringSite 缺工程由 service 按声明的 key 拒绝。
func TestSEOAuditMissingProjectIs400FromDeclaringSite(t *testing.T) {
	chain := newSEOAuditChain(t)

	res := seoAuditRun(t, chain, "缺工程", "", http.StatusBadRequest)
	if !strings.Contains(res.Body, "data-ui-error") {
		t.Fatalf("缺工程应得到共享失败片段，实际 %q", firstN(res.Body, 200))
	}
	// 失败片段按状态码取共享兜底文案；具体 key 归 enums 声明，HTML 出口不拼句子。
	if strings.Contains(res.Body, "ErrProjectRequired") {
		t.Fatalf("失败片段不得把裸 key 交给用户：%q", firstN(res.Body, 200))
	}
}

// TestSEOAuditKeepsTwoProjectsIsolatedInSharedActiveRoot 两个工程共用站点级激活根时不得串产物。
func TestSEOAuditKeepsTwoProjectsIsolatedInSharedActiveRoot(t *testing.T) {
	chain := newSEOAuditChain(t)

	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 描述", Canonical: "https://a.example.com/a-about",
		Links: []string{"/a-about-dead"},
	})
	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditBPathDocs, seoAuditArtifactHTML{
		Title: "B 文档", Description: "B 描述", Canonical: "https://b.example.com/b-docs",
		Links: []string{"/b-docs-dead"},
	})

	resA := seoAuditRun(t, chain, "工程 A", seoAuditProjectA, http.StatusOK)
	if state := seoAuditState(resA.Body); state != "issues" {
		t.Fatalf("工程 A 结论档位 %q，期望 issues（/a-about-dead 是死链）", state)
	}
	if !strings.Contains(resA.Body, "/a-about-dead") {
		t.Fatalf("工程 A 的结论应带自己的死链 /a-about-dead")
	}
	if strings.Contains(resA.Body, "/b-docs-dead") || strings.Contains(resA.Body, seoAuditBPathDocs) {
		t.Fatalf("工程 A 的结论里出现工程 B 的路径 —— 站点级激活根没有按工程过滤")
	}

	resB := seoAuditRun(t, chain, "工程 B", seoAuditProjectB, http.StatusOK)
	if !strings.Contains(resB.Body, "/b-docs-dead") {
		t.Fatalf("工程 B 的结论应带自己的死链 /b-docs-dead")
	}
	if strings.Contains(resB.Body, "/a-about-dead") || strings.Contains(resB.Body, seoAuditAPathAbout) {
		t.Fatalf("工程 B 的结论里出现工程 A 的路径 —— 站点级激活根没有按工程过滤")
	}
	// 两个工程各只有一个页面，重复标题只能来自把两工程的产物混在一起。
	if strings.Contains(resA.Body, "多个页面使用了同一个标题") ||
		strings.Contains(resB.Body, "多个页面使用了同一个标题") {
		t.Fatalf("出现了 title_duplicate —— 两个工程各只有一页，重复只能来自混读产物")
	}
}

// TestSEOAuditRequiresSessionCSRFAndBothPermissions 双权限与 CSRF 的拒绝面。
func TestSEOAuditRequiresSessionCSRFAndBothPermissions(t *testing.T) {
	chain := newSEOAuditChain(t)

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

	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 描述", Canonical: "https://a.example.com/a-about",
	})

	subject := fmt.Sprintf("%d", chain.PlainID)
	enforcer := pkgcasbin.GetEnforcer()
	if enforcer == nil {
		t.Fatal("Casbin enforcer 为 nil：InitCasbin 没有生效")
	}

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
	grantBoth := func() {
		t.Helper()
		dropPolicies()
		grant(seoAuditRealPath, seoAuditPerm)
		grant(seoAuditVirtual, seoAuditLegacyObj)
	}

	form := "project=" + url.QueryEscape(seoAuditProjectA)

	t.Run("无会话 Cookie", func(t *testing.T) {
		grantBoth()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        form,
			CSRF:        chain.Plain.CSRFToken,
		})
		assertSeoAuditHTML(t, "无会话", res, http.StatusUnauthorized)
	})

	t.Run("有会话但缺 CSRF", func(t *testing.T) {
		grantBoth()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        form,
			Cookie:      chain.Plain.Cookie,
		})
		assertSeoAuditHTML(t, "缺 CSRF", res, http.StatusForbidden)
		if strings.Contains(res.Body, "data-seo-audit-state") {
			t.Fatalf("缺 CSRF：响应里出现体检结论 —— 请求已经跑到 handler 里去了")
		}
	})

	t.Run("CSRF 错误值", func(t *testing.T) {
		grantBoth()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        form,
			Cookie:      chain.Plain.Cookie,
			CSRF:        "not-the-right-token",
		})
		assertSeoAuditHTML(t, "CSRF 错误", res, http.StatusForbidden)
	})

	t.Run("有会话有 CSRF 但两个权限都没有", func(t *testing.T) {
		dropPolicies()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        form,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		assertSeoAuditHTML(t, "无权限", res, http.StatusForbidden)
	})

	t.Run("只有 publication:seo_audit", func(t *testing.T) {
		dropPolicies()
		grant(seoAuditRealPath, seoAuditPerm)
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        form,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		assertSeoAuditHTML(t, "只授 publication", res, http.StatusForbidden)
	})

	t.Run("只有 seo:audit", func(t *testing.T) {
		dropPolicies()
		grant(seoAuditVirtual, seoAuditLegacyObj)
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        form,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		assertSeoAuditHTML(t, "只授 seo:audit", res, http.StatusForbidden)
	})

	t.Run("两个权限都有", func(t *testing.T) {
		grantBoth()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/x-www-form-urlencoded",
			Body:        form,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		assertSeoAuditHTML(t, "双权限", res, http.StatusOK)
		if state := seoAuditState(res.Body); state != "clean" {
			t.Fatalf("双权限：结论档位 %q，期望 clean —— 鉴权放行后 handler 应真的跑完体检", state)
		}
	})
}

// TestSEOAuditDistinguishesNoArtifactFromNoIssue 0/0 与 1/0 必须是两个读数。
func TestSEOAuditDistinguishesNoArtifactFromNoIssue(t *testing.T) {
	chain := newSEOAuditChain(t)

	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 描述", Canonical: "https://a.example.com/a-about",
	})

	resNoArtifact := seoAuditRun(t, chain, "无产物", seoAuditProjectB, http.StatusOK)
	if state := seoAuditState(resNoArtifact.Body); state != "empty" {
		t.Fatalf("无产物：结论档位 %q，期望 empty（还没有可检查的产物）", state)
	}

	resClean := seoAuditRun(t, chain, "干净", seoAuditProjectA, http.StatusOK)
	if state := seoAuditState(resClean.Body); state != "clean" {
		t.Fatalf("干净：结论档位 %q，期望 clean（扫到了产物且没有问题）", state)
	}
	if resNoArtifact.Body == resClean.Body {
		t.Fatal("「无产物」与「干净」渲染出同一份片段 —— 两个读数必须可区分")
	}
}

// TestSEOAuditDuplicateTitleCarriesNoSinglePageOwnership 重复标题不属于任何单页，且列出全部命中路径。
func TestSEOAuditDuplicateTitleCarriesNoSinglePageOwnership(t *testing.T) {
	chain := newSEOAuditChain(t)

	const dupTitle = "同名标题"

	if err := seedSeoAuditRoutes(t, chain.DB, seoAuditProjectA, "/a-zzz", "/a-bbb"); err != nil {
		t.Fatalf("补工程 A 激活路由失败: %v", err)
	}
	for _, p := range []string{seoAuditAPathAbout, "/a-zzz", "/a-bbb"} {
		writeSeoAuditArtifact(t, chain.ArtifactRoot, p, seoAuditArtifactHTML{
			Title: dupTitle, Description: "描述", Canonical: "https://a.example.com" + p,
		})
	}

	res := seoAuditRun(t, chain, "重复标题", seoAuditProjectA, http.StatusOK)
	if state := seoAuditState(res.Body); state != "issues" {
		t.Fatalf("重复标题：结论档位 %q，期望 issues", state)
	}
	if !strings.Contains(res.Body, "多个页面使用了同一个标题") {
		t.Fatalf("结论里缺少重复标题项：%q", firstN(res.Body, 400))
	}
	// 三个同标题页面归为一条结论：三处路径都要在受影响路径列表里（少一个就是藏页面）。
	for _, p := range []string{seoAuditAPathAbout, "/a-zzz", "/a-bbb"} {
		if !strings.Contains(res.Body, "<code>"+p+"</code>") {
			t.Fatalf("重复标题的受影响路径缺少 %s", p)
		}
	}
	if got := strings.Count(res.Body, "多个页面使用了同一个标题"); got != 1 {
		t.Fatalf("重复标题应恰好报一条（三个同标题页面归为一条结论），实际 %d 条", got)
	}
}
