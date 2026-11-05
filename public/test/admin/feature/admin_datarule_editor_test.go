package feature

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/adminui"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/datarule"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestAdminDataruleEditorPageUsesLocalTemplates(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	found := false
	for _, seed := range migrations.AllSeeds() {
		if seed.Version != "707-admin-datarule-editor-i18n" {
			continue
		}
		found = true
		if err := db.Exec("PREPARE rule_editor_seed AS " + seed.SQL).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("EXECUTE rule_editor_seed").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("DEALLOCATE rule_editor_seed").Error; err != nil {
			t.Fatal(err)
		}
		var ready int
		if err := db.Raw(seed.ConditionSQL).Scan(&ready).Error; err != nil || ready != 1 {
			t.Fatalf("词库门槛=%d err=%v", ready, err)
		}
	}
	if !found {
		t.Fatal("编辑器词库seed未注册")
	}
	var words []struct{ ItemKey, Lang, ItemValue string }
	if err := db.Table("sys_i18n").Where("item_key IN ?", []string{"admin.datarules.editor.domain_conflict", "admin.datarules.editor.load_failed", "admin.datarules.editor.loading"}).Find(&words).Error; err != nil {
		t.Fatal(err)
	}
	lexicon := map[string]map[string]string{}
	for _, word := range words {
		if lexicon[word.ItemKey] == nil {
			lexicon[word.ItemKey] = map[string]string{}
		}
		lexicon[word.ItemKey][word.Lang] = word.ItemValue
	}
	i18n.InjectForTest(lexicon, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	const domain = "EDITOR_LOCAL_TEST"
	if err := datarule.RegisterDomain(datarule.DomainConfig{Domain: domain, DomainLabel: "测试域", TableName: "sys_admin", WhiteList: []datarule.FieldDef{{Field: "username", Label: "账号", Operators: []string{"EQ", "LIKE"}}}}); err != nil {
		t.Fatal(err)
	}
	rule := adminmodel.SysRuleEntity{RuleName: "编辑器真实配置", Domain: domain, Status: 1, Config: "{\"omit_fields\":[\"username\"],\"condition_groups\":[{\"logic\":\"OR\",\"conditions\":[{\"field\":\"username\",\"op\":\"LIKE\",\"value\":\"alice\"}]}]}"}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	svc := adminservice.NewService(db)
	h := adminhttp.NewAdminPagesHandle(svc, svc, svc, svc, svc, svc)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			t.Errorf("页面渲染错误: %v", c.Errors)
		}
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/admin/datarules/edit", adminui.PageSlot(adminhttp.DataruleEditPageTemplate), h.DatarulesEditPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/datarules/edit?id="+strconv.FormatUint(rule.ID, 10), nil))
	if rec.Code != 200 {
		t.Fatalf("页面状态=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"data-datarule-editor", "data-rule-group-template", "data-rule-row-template", "data-initial-field=\"username\"", "data-initial-op=\"LIKE\"", "value=\"alice\"", "name=\"omit_fields\" value=\"username\" checked", "/static/js/ui/datarule-editor.js", "请选择字段"} {
		if !strings.Contains(body, want) {
			t.Errorf("首屏缺少真实配置或模板 %q", want)
		}
	}
	if strings.Contains(body, "/admin/datarules/config-editor") {
		t.Fatal("页面仍依赖已退役HTML端点")
	}
	for _, lang := range []string{"zh-CN", "en-US", "zh-CN"} {
		translated := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/datarules/edit?id="+strconv.FormatUint(rule.ID, 10), nil)
		req.Header.Set("Accept-Language", lang)
		engine.ServeHTTP(translated, req)
		for key, entries := range lexicon {
			if translated.Code != 200 || !strings.Contains(translated.Body.String(), entries[lang]) {
				t.Errorf("%s 的 %s 未按数据库词库渲染", lang, key)
			}
		}
	}
	if err := os.WriteFile("/tmp/wp-admin-datarule-editor-page.html", rec.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
