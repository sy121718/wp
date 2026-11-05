package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/html"
	"gorm.io/gorm"

	"go_wp/internal/adminui"
	adminenums "go_wp/internal/module/admin/enums"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/casbin"
	"go_wp/pkg/i18n"
	"go_wp/pkg/view"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// newDrawerEngine 装配真实 Service、Jet 与请求级失败出口，覆盖授权抽屉的读取和保存。
func newDrawerEngine(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	initAdminPolicyTest(t, db)
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == "058-i18n-seed-enums" || seed.Version == "228b-i18n-seed-role-permissions" {
			if err := db.Exec(seed.SQL).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	var words []struct{ ItemKey, Lang, ItemValue string }
	if err := db.Table("sys_i18n").Where("status = ?", 1).Find(&words).Error; err != nil {
		t.Fatal(err)
	}
	lexicon := make(map[string]map[string]string)
	for _, word := range words {
		if lexicon[word.ItemKey] == nil {
			lexicon[word.ItemKey] = make(map[string]string)
		}
		lexicon[word.ItemKey][word.Lang] = word.ItemValue
	}
	i18n.InjectForTest(lexicon, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	svc := adminservice.NewService(db)
	h := adminhttp.NewAdminPagesHandle(svc, svc, svc, svc, nil, nil)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(view.FailureMiddleware(adminui.RenderFailure))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.GET("/admin/administrators/roles/drawer", view.HTMLFragment, h.AdministratorRolesDrawer)
	engine.GET("/admin/administrators/menus/drawer", view.HTMLFragment, h.AdministratorMenusDrawer)
	engine.GET("/admin/roles/permissions/drawer", view.HTMLFragment, h.RolePermissionsDrawer)
	engine.GET("/admin/menus/edit", view.HTMLFragment, h.MenusEditFragment)
	engine.GET("/admin/permissions/edit", view.HTMLFragment, h.PermissionsEditFragment)
	api := adminhttp.NewHandle(svc)
	engine.POST("/api/role/menu/save", view.HTMLFragment, func(c *gin.Context) { c.Set("user_id", int64(123)); api.RoleMenuSave(c) })
	engine.POST("/api/admin/role/save", view.HTMLFragment, func(c *gin.Context) { c.Set("user_id", int64(123)); api.AdminRoleSave(c) })
	engine.POST("/api/admin/menu/save", view.HTMLFragment, func(c *gin.Context) { c.Set("user_id", int64(123)); api.AdminMenuSave(c) })
	return engine
}

func fetchDrawer(t *testing.T, engine *gin.Engine, path string, lang string) (int, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	// 客户端 Accept 不得改变端点声明的 HTML 协议。
	req.Header.Set("Accept", "application/json")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	engine.ServeHTTP(rec, req)
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("抽屉必须返回不可缓存的 HTML: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	return rec.Code, rec.Body.Bytes()
}

// drawerCheckboxChecked 解析实际 HTML 控件，不依赖属性顺序或服务端展示对象。
func drawerCheckboxChecked(t *testing.T, body, name, value string) bool {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			t.Fatalf("缺少 checkbox %s=%s: %s", name, value, body)
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			if token.Data != "input" {
				continue
			}
			attrs := make(map[string]string)
			for _, attr := range token.Attr {
				attrs[attr.Key] = attr.Val
			}
			if attrs["type"] == "checkbox" && attrs["name"] == name && attrs["value"] == value {
				_, checked := attrs["checked"]
				return checked
			}
		}
	}
}

func TestAdminDrawerGetReturnsHTMLForms(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	engine := newDrawerEngine(t, db)
	role := &adminmodel.RoleEntity{RoleCode: "dr_role", RoleName: "抽屉角色", IsSystem: 0}
	account := &adminmodel.AdminEntity{Username: "dr_admin", Password: "x", Status: adminmodel.AdminStatusActive, IsAdmin: 1}
	for _, entity := range []any{role, account} {
		if err := db.Create(entity).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ name, path, endpoint, idName, id string }{
		{"管理员角色", "/admin/administrators/roles/drawer?user_id=" + itoa(account.ID), "/api/admin/role/save", "user_id", itoa(account.ID)},
		{"管理员菜单", "/admin/administrators/menus/drawer?user_id=" + itoa(account.ID), "/api/admin/menu/save", "user_id", itoa(account.ID)},
		{"角色权限", "/admin/roles/permissions/drawer?role_id=" + itoa(role.ID), "/api/role/menu/save", "role_id", itoa(role.ID)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := fetchDrawer(t, engine, tc.path, "zh-CN")
			if code != http.StatusOK {
				t.Fatalf("GET 应 200，实际 %d: %s", code, body)
			}
			for _, want := range []string{"<form data-drawer-fragment", `hx-post="` + tc.endpoint + `"`, `name="` + tc.idName + `" value="` + tc.id + `"`, "</form>", "data-write-status"} {
				if !strings.Contains(string(body), want) {
					t.Fatalf("Jet 表单缺少 %q: %s", want, body)
				}
			}
			if strings.Contains(string(body), "data-bind") || strings.Contains(string(body), "<html") {
				t.Fatalf("抽屉不得带旧回填或整页: %s", body)
			}
		})
	}
}

func TestAdminDrawerGetFollowsRequestLanguage(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	engine := newDrawerEngine(t, db)
	role := &adminmodel.RoleEntity{RoleCode: "dr_role_lang", RoleName: "语言角色", IsSystem: 0}
	menu := &adminmodel.MenuEntity{Title: "语言菜单", Type: 2, Status: 1}
	perm := &adminmodel.PermissionEntity{PermissionCode: "dr.perm.lang", PermissionName: "语言权限", Module: "admin", APIPath: "/dr/lang", APIMethod: "GET", Status: 1}
	for _, entity := range []any{role, menu, perm} {
		if err := db.Create(entity).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("sys_menu_permission").Create(map[string]any{"menu_id": menu.ID, "permission_code": perm.PermissionCode}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&casbin.CasbinRule{Ptype: "p", V0: role.RoleCode, V1: perm.APIPath, V2: perm.APIMethod, V3: perm.PermissionCode}).Error; err != nil {
		t.Fatal(err)
	}
	if err := casbin.ReloadPolicy(); err != nil {
		t.Fatal(err)
	}
	path := "/admin/roles/permissions/drawer?role_id=" + itoa(role.ID)
	var first []byte
	for _, lang := range []string{"zh-CN", "en-US", "zh-CN"} {
		var want string
		if err := db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "admin.roles.perm.action.save", lang).Scan(&want).Error; err != nil {
			t.Fatal(err)
		}
		code, body := fetchDrawer(t, engine, path, lang)
		if code != http.StatusOK || want == "" || !strings.Contains(string(body), want) {
			t.Fatalf("lang=%s status=%d want=%q body=%s", lang, code, want, body)
		}
		if !drawerCheckboxChecked(t, string(body), "menu_ids", itoa(menu.ID)) || !strings.Contains(string(body), menu.Title) || !strings.Contains(string(body), perm.PermissionCode) {
			t.Fatalf("切换语言改变业务事实: %s", body)
		}
		if first == nil {
			first = body
		} else if (string(first) == string(body)) != (lang == "zh-CN") {
			t.Fatalf("语言未切换或跨请求污染: lang=%s body=%s", lang, body)
		}
	}
}

func TestAdminDrawerGetFailureIsHTML(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	engine := newDrawerEngine(t, db)
	code, body := fetchDrawer(t, engine, "/admin/roles/permissions/drawer?role_id=999999", "zh-CN")
	if code != http.StatusNotFound || !strings.Contains(string(body), "data-ui-error") || strings.Contains(string(body), "<form") {
		t.Fatalf("不存在的角色须返回 404 错误片段: %d %s", code, body)
	}
}

func itoa(id uint64) string {
	return strconv.FormatUint(id, 10)
}

func TestAdminDrawerEditGETReturnsHTMLFragment(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	ctx := context.Background()
	engine := newDrawerEngine(t, db)
	menu := &adminmodel.MenuEntity{Title: "抽屉测试菜单", Type: 2, Status: 1}
	perm := &adminmodel.PermissionEntity{PermissionCode: "drawer:probe", PermissionName: "抽屉探针", Module: "admin", APIPath: "/api/drawer/probe", APIMethod: "GET", Status: 1}
	for _, entity := range []any{menu, perm} {
		if err := db.WithContext(ctx).Create(entity).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/admin/menus/edit?id=" + itoa(menu.ID), "/admin/permissions/edit?id=" + itoa(perm.ID)} {
		code, body := fetchDrawer(t, engine, path, "zh-CN")
		if code != http.StatusOK || !strings.Contains(string(body), "<form data-drawer-fragment") || !strings.Contains(string(body), "</form>") || strings.Contains(string(body), "data-bind") {
			t.Fatalf("编辑抽屉必须返回 Jet 表单: %d %s", code, body)
		}
	}
}

func TestAdminDrawerFailureFollowsWordLibrary(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	engine := newDrawerEngine(t, db)
	var previous string
	for _, lang := range []string{"zh-CN", "en-US", "zh-CN"} {
		var want string
		if err := db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", adminenums.ErrRoleNotFound, lang).Scan(&want).Error; err != nil {
			t.Fatal(err)
		}
		code, body := fetchDrawer(t, engine, "/admin/roles/permissions/drawer?role_id=999999999", lang)
		if want == "" || code != http.StatusNotFound || !strings.Contains(string(body), want) || !strings.Contains(string(body), "data-ui-error") {
			t.Fatalf("lang=%s status=%d want=%q body=%s", lang, code, want, body)
		}
		if lang == "en-US" && string(body) == previous {
			t.Fatal("中英文错误文案未隔离")
		}
		previous = string(body)
	}
}

func TestAdminDrawerSaveThenGetUsesPersistedPermissions(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	engine := newDrawerEngine(t, db)
	role := adminmodel.RoleEntity{RoleCode: "drawer_save", RoleName: "抽屉保存"}
	menu := adminmodel.MenuEntity{Title: "保存菜单", Type: 2, Status: 1}
	for _, entity := range []any{&role, &menu} {
		if err := db.Create(entity).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []string{"drawer.save", "drawer.other"} {
		perm := adminmodel.PermissionEntity{PermissionCode: code, PermissionName: code, Module: "admin", APIPath: "/" + code, APIMethod: "GET", Status: 1}
		if err := db.Create(&perm).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("sys_menu_permission").Create(map[string]any{"menu_id": menu.ID, "permission_code": "drawer.save"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, selected := range []bool{true, false} {
		form := url.Values{"role_id": {itoa(role.ID)}}
		if selected {
			form.Add("menu_ids", itoa(menu.ID))
		}
		rec := postRolePermissions(t, engine, form, true)
		assertRolePermissionsHTML(t, rec, http.StatusOK)
		var count int64
		if err := db.Model(&casbin.CasbinRule{}).Where("ptype = ? AND v0 = ? AND v3 = ?", "p", role.RoleCode, "drawer.save").Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if (count == 1) != selected {
			t.Fatalf("策略持久化错误: selected=%v count=%d", selected, count)
		}
		code, body := fetchDrawer(t, engine, "/admin/roles/permissions/drawer?role_id="+itoa(role.ID), "en-US")
		if code != http.StatusOK || drawerCheckboxChecked(t, string(body), "menu_ids", itoa(menu.ID)) != selected {
			t.Fatalf("保存后 Jet 勾选未更新: selected=%v status=%d body=%s", selected, code, body)
		}
	}
}

func TestAdminAccountDrawerFormSaveThenClear(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	engine := newDrawerEngine(t, db)
	account := adminmodel.AdminEntity{Username: "drawer_form_account", Password: "x", Status: 1}
	role := adminmodel.RoleEntity{RoleCode: "drawer_form_role", RoleName: "角色", Status: 1}
	menu := adminmodel.MenuEntity{Title: "菜单", Type: 2, Status: 1}
	for _, entity := range []any{&account, &role, &menu} {
		if err := db.Create(entity).Error; err != nil {
			t.Fatal(err)
		}
	}
	perm := adminmodel.PermissionEntity{PermissionCode: "drawer.account.menu", PermissionName: "drawer.account.menu", Module: "admin", APIPath: "/drawer/account/menu", APIMethod: "GET", Status: 1}
	if err := db.Create(&perm).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("sys_menu_permission").Create(map[string]any{"menu_id": menu.ID, "permission_code": perm.PermissionCode}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, endpoint, field, value, drawer string }{
		{"role", "/api/admin/role/save", "role_codes", role.RoleCode, "/admin/administrators/roles/drawer"},
		{"menu", "/api/admin/menu/save", "menu_ids", itoa(menu.ID), "/admin/administrators/menus/drawer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, selected := range []bool{true, false} {
				form := url.Values{"user_id": {itoa(account.ID)}}
				if selected {
					form.Add(tc.field, tc.value)
				}
				req := httptest.NewRequest(http.MethodPost, tc.endpoint, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				assertRolePermissionsHTML(t, rec, http.StatusOK)
				code, body := fetchDrawer(t, engine, tc.drawer+"?user_id="+itoa(account.ID), "en-US")
				if code != http.StatusOK || drawerCheckboxChecked(t, string(body), tc.field, tc.value) != selected {
					t.Fatalf("保存或清空未反映到 Jet 控件: selected=%v status=%d body=%s", selected, code, body)
				}
			}
		})
	}
}
