package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	"go_wp/internal/templates"

	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"

	"github.com/gin-gonic/gin"
)

func TestMailCampaignPageRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	if err := f.seedCampaign(ctx, "九月活动"); err != nil {
		t.Fatal(err)
	}
	cps, err := f.svc.ListCampaigns(ctx, &maildto.CampaignListReq{Page: 1, PageSize: 10})
	if err != nil || len(cps.Items) == 0 {
		t.Fatalf("活动未建成功: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)

	router.GET("/admin/mail/campaign",
		adminui.PageSlot(mailhttp.CampaignPageTemplate),
		mailhttp.NewMailPageHandle(f.svc).MailCampaignPage)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/mail/campaign?id="+strconv.FormatUint(cps.Items[0].ID, 10), nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("页面状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"九月活动", "投递与互动", "打开率", "估算",
		"点击率", "链接点击排行", "收件人明细", "返回群发活动",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
}
