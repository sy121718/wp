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
	pubenums "go_wp/internal/module/publication/enums"
	pubhttp "go_wp/internal/module/publication/inbound/http"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/templates"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	json "go_wp/pkg/jsonx"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/response"
	"go_wp/public/migrations"
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

type seoAuditEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func decodeSeoAuditEnvelope(t *testing.T, label string, res seoAuditRawResponse) seoAuditEnvelope {
	t.Helper()

	var env seoAuditEnvelope
	if err := json.Unmarshal([]byte(res.Body), &env); err != nil {
		t.Fatalf("%s：响应不是 {code,message,data} JSON（Content-Type=%q，体前 200 字节=%q）: %v",
			label, res.ContentType, firstN(res.Body, 200), err)
	}
	return env
}

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

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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

func TestSEOAuditAcceptsThreeTransportsAndEchoesJSONContract(t *testing.T) {
	chain := newSEOAuditChain(t)

	type transport struct {
		name        string
		contentType string
		body        string
	}

	writeSeoAuditArtifact(t, chain.ArtifactRoot, seoAuditAPathAbout, seoAuditArtifactHTML{
		Title: "A 关于", Description: "A 站关于页", Canonical: "https://a.example.com/a-about",
	})

	transports := []transport{
		{"JSON", "application/json", `{"project":"` + seoAuditProjectA + `"}`},
		{"urlencoded", "application/x-www-form-urlencoded", "project=" + url.QueryEscape(seoAuditProjectA)},
		{"multipart", "multipart/form-data", ""},
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

					body, ct := seoAuditMultipartBody(t, map[string]string{"project": seoAuditProjectA})
					req.Body, req.ContentType = body, ct
				}
				if hx == "true" {
					req.Headers = map[string]string{"HX-Request": "true", "Accept": "text/html"}
				}

				res := doSeoAudit(t, chain.Engine, req)
				assertSeoAuditJSON(t, tr.name, res, http.StatusOK)

				env := decodeSeoAuditEnvelope(t, tr.name, res)

				if env.Code != http.StatusOK {
					t.Fatalf("%s：成功信封 code=%d，期望 %d（体=%q）",
						tr.name, env.Code, http.StatusOK, firstN(res.Body, 200))
				}
				data := decodeSeoAuditData(t, tr.name, env)

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

func TestSEOAuditQueryFallbackOnlyWhenBodyProjectBlank(t *testing.T) {
	chain := newSEOAuditChain(t)

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

func TestSEOAuditMalformedJSONIs400WithoutFallback(t *testing.T) {
	chain := newSEOAuditChain(t)

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

				Query:  map[string]string{"project": seoAuditProjectA},
				Cookie: chain.Super.Cookie,
				CSRF:   chain.Super.CSRFToken,
			})
			assertSeoAuditJSON(t, tc.name, res, http.StatusBadRequest)

			env := decodeSeoAuditEnvelope(t, tc.name, res)
			if len(env.Data) > 0 && string(env.Data) != "null" {
				t.Fatalf("%s：400 响应不该带业务数据（data=%s）—— 带了说明体检还是跑了", tc.name, env.Data)
			}

			lower := strings.ToLower(env.Message)
			for _, leak := range []string{"json:", "unexpected", "syntax error", "invalid character", "offset"} {
				if strings.Contains(lower, leak) {
					t.Fatalf("%s：400 文案里出现解析器原文 %q（完整文案=%q）", tc.name, leak, env.Message)
				}
			}
		})
	}
}

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

	wantMsg := response.MessageOf(nil, "", pubenums.ErrProjectMissing())
	if env.Message != wantMsg {
		t.Fatalf("缺工程：文案=%q，期望声明处 ErrProjectMissing 的取词结果 %q —— "+
			"文案与状态码都要在 enums 声明，出口只调 response.ErrorStatusAuto", env.Message, wantMsg)
	}
}

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

	if dataA.Scanned != 1 {
		t.Fatalf("工程 A 的 scanned=%d，期望 1 —— 站点级激活目录里有 %d 份产物，"+
			"多出来的那份是另一个工程的（工程过滤缺口）", dataA.Scanned, 2)
	}
	if dataB.Scanned != 1 {
		t.Fatalf("工程 B 的 scanned=%d，期望 1 —— 同上，工程过滤缺口", dataB.Scanned)
	}

	if got, want := pathsOf(dataA), []string{seoAuditAPathAbout}; !equalStrings(got, want) {
		t.Fatalf("工程 A 的结论路径 %v，期望 %v —— 出现了别的工程的路径", got, want)
	}
	if got, want := pathsOf(dataB), []string{seoAuditBPathDocs}; !equalStrings(got, want) {
		t.Fatalf("工程 B 的结论路径 %v，期望 %v —— 出现了别的工程的路径", got, want)
	}

	if got, want := hrefsOf(dataA), []string{"/a-about-dead"}; !equalStrings(got, want) {
		t.Fatalf("工程 A 的死链 %v，期望 %v —— 内链对照用了错误的路径集合（并集会漏报，串工程会误报）", got, want)
	}
	if got, want := hrefsOf(dataB), []string{"/b-docs-dead"}; !equalStrings(got, want) {
		t.Fatalf("工程 B 的死链 %v，期望 %v —— 同上", got, want)
	}

	for _, data := range []seoAuditData{dataA, dataB} {
		for _, issue := range data.Issues {
			if issue.Item == "title_duplicate" {
				t.Fatalf("出现了 title_duplicate（Paths=%v）—— 两个工程各只有一页标题，重复只能来自把两工程的产物混在一起",
					issue.Params.Paths)
			}
		}
	}
}

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

	validBody := `{"project":"` + seoAuditProjectA + `"}`

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
		})
		assertSeoAuditJSON(t, "缺 CSRF", res, http.StatusForbidden)

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

		dropPolicies()
		res := doSeoAudit(t, chain.Engine, seoAuditRequest{
			ContentType: "application/json",
			Body:        validBody,
			Cookie:      chain.Plain.Cookie,
			CSRF:        chain.Plain.CSRFToken,
		})
		assertSeoAuditJSON(t, "无权限", res, http.StatusForbidden)

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

		assertSeoAuditJSON(t, "双权限", res, http.StatusOK)
		data := decodeSeoAuditData(t, "双权限", decodeSeoAuditEnvelope(t, "双权限", res))
		if data.Scanned != 1 {
			t.Fatalf("双权限：scanned=%d，期望 1 —— 鉴权放行后 handler 应真的跑完体检", data.Scanned)
		}
	})
}

func TestSEOAuditDistinguishesNoArtifactFromNoIssue(t *testing.T) {
	chain := newSEOAuditChain(t)

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

	if !strings.Contains(string(decodeSeoAuditEnvelope(t, "无产物", resNoArtifact).Data), `"issues":[]`) {
		t.Fatalf("无产物：issues 必须编码为 []，实际 data=%s",
			decodeSeoAuditEnvelope(t, "无产物", resNoArtifact).Data)
	}

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
