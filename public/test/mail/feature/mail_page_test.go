package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/adminui"
	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

type mailFixture struct {
	svc *mailservice.Service
	db  *gorm.DB
}

func newMailFeatureFixture(t *testing.T) *mailFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	svc := mailservice.NewService(mailmodel.NewMailModel(db))
	svc.SetCipherSecret("page-test-secret")
	return &mailFixture{svc: svc, db: db}
}

func (f *mailFixture) seedContact(ctx context.Context, email, name, status, tag string) error {
	nameVal := name
	e := &mailmodel.MailContactEntity{
		Email: email, Name: &nameVal, Source: mailmodel.ContactSourceImport, Status: status,
	}
	if tag != "" {
		e.Tags = mailmodel.StringArray{tag}
	}
	if status == mailmodel.ContactStatusSubscribed {
		now := time.Now()
		e.SubscribedAt = &now
		src := "测试"
		e.ConsentSource = &src
	}
	return mailmodel.NewMailModel(f.db).CreateContact(ctx, e)
}

func (f *mailFixture) seedCampaign(ctx context.Context, name string) error {
	acc, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "营销账号", Purpose: mailmodel.AccountPurposeMarketing,
		FromEmail: "news@clker.cn", Host: "mail.clker.cn", Port: 587, Password: "p",
	})
	if err != nil {
		return err
	}
	if _, err = f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "campaign_tpl", Name: "活动模板", Subject: "主题", BodyHTML: "<p>x</p>",
	}); err != nil {
		return err
	}
	tpls, err := f.svc.ListTemplates(ctx, "campaign_tpl")
	if err != nil || len(tpls) == 0 {
		return err
	}
	_, err = f.svc.SaveCampaign(ctx, &maildto.SaveCampaignReq{
		Name: name, AccountID: acc.ID, TemplateID: tpls[0].ID, Subject: "九月上新",
	})
	return err
}

func newMailPageFixture(t *testing.T) (*gin.Engine, *mailservice.Service) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil
	}
	svc := mailservice.NewService(mailmodel.NewMailModel(db))
	svc.SetCipherSecret("page-test-secret")
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.Use(func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("gin 渲染错误: %v", e.Err)
		}
	})
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/mail",
		adminui.PageSlot(mailhttp.AccountsPageTemplate), mailhttp.NewMailPageHandle(svc).MailPage)
	return router, svc
}

func TestMailPageRendersWithData(t *testing.T) {
	router, svc := newMailPageFixture(t)
	if router == nil {
		return
	}
	ctx := context.Background()
	if _, err := svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "系统通知", Purpose: mailmodel.AccountPurposeTransactional,
		FromEmail: "admin@clker.cn", Host: "mail.clker.cn", Port: 587,
		Username: "admin@clker.cn", Password: "super-secret-password", Encryption: "starttls",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "register_verify", Locale: "", Name: "注册验证",
		Subject: "请验证你的邮箱", BodyHTML: "<p>{{.code}}</p>",
	}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/mail", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("页面状态码 %d，正文前 300 字：%s", recorder.Code, firstN(recorder.Body.String(), 300))
	}
	body := recorder.Body.String()

	for _, want := range []string{"发信账号", "系统通知", "mail.clker.cn", "已配置"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；正文长度 %d，前 600 字：\n%s", want, len(body), firstN(body, 600))
		}
	}
	if strings.Contains(body, "super-secret-password") {
		t.Fatal("页面回显了 SMTP 明文密码")
	}
}

func TestMailPageRendersEmpty(t *testing.T) {
	router, _ := newMailPageFixture(t)
	if router == nil {
		return
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/mail", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("空状态页面状态码 %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), "还没有发信账号") {
		t.Fatal("空状态提示缺失")
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

var _ = gorm.ErrRecordNotFound
