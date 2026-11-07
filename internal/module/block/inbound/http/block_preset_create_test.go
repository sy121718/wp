package blockhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

type presetCreateStub struct {
	blockcontract.BlockService
	request *blockdto.CreateReq
	err     error
}

func (s *presetCreateStub) Create(_ context.Context, req *blockdto.CreateReq) (*blockdto.BlockResp, error) {
	s.request = req
	return &blockdto.BlockResp{ID: "created", Name: req.Name}, s.err
}

// createEngine 复刻生产接线：失败中间件、请求取词与 HTML 渲染都必须在场。
func createEngine(svc blockcontract.BlockService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	r.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	r.POST("/api/block/create", pageview.HTMLFragment, (&Handle{svc: svc}).Create)
	return r
}

func createPresetResponse(t *testing.T, svc *presetCreateStub, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/block/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createEngine(svc).ServeHTTP(rec, req)
	return rec
}

func TestPresetCreateAcceptsFormAndReturnsHTML(t *testing.T) {
	injectBlockWords(t, map[string]map[string]string{
		"blkCreated":  {"zh-CN": "块「{name}」已创建", "en-US": "Block \"{name}\" created"},
		"blkOpenEdit": {"zh-CN": "立即打开编辑", "en-US": "Open the editor"},
	})
	svc := &presetCreateStub{}
	rec := createPresetResponse(t, svc, url.Values{
		"projectId": {"project"}, "name": {"账号中心"},
		"presetKey": {"user.account"}, "presetVersion": {"2"},
	})
	if rec.Code != http.StatusOK || svc.request == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if svc.request.PresetKey != "user.account" || svc.request.PresetVersion != 2 || len(svc.request.Document) != 0 {
		t.Fatalf("绑定来源漂移：%+v", svc.request)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("创建必须回 HTML 片段，实际 %q", ct)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("后端不得控制导航：Location=%q", loc)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data-block-created-link") || !strings.Contains(body, "/workbench?block=created") {
		t.Fatalf("成功片段必须带回下一步链接（由 Jet 按实体事实生成），实际 %s", firstBlockN(body, 400))
	}
	if !strings.Contains(body, "账号中心") {
		t.Fatalf("成功片段应点名新建的块，实际 %s", firstBlockN(body, 400))
	}
}

func TestPresetCreateRejectsInvalidCombinations(t *testing.T) {
	base := func() url.Values {
		return url.Values{"projectId": {"project"}, "name": {"账号中心"}, "presetKey": {"user.account"}, "presetVersion": {"1"}}
	}
	for _, invalid := range []string{"missing-version", "zero-version", "negative-version", "orphan-version"} {
		t.Run(invalid, func(t *testing.T) {
			form := base()
			switch invalid {
			case "missing-version":
				form.Del("presetVersion")
			case "zero-version":
				form.Set("presetVersion", "0")
			case "negative-version":
				form.Set("presetVersion", "-1")
			case "orphan-version":
				form.Del("presetKey")
			}
			svc := &presetCreateStub{}
			rec := createPresetResponse(t, svc, form)
			if rec.Code != http.StatusBadRequest || svc.request != nil {
				t.Fatalf("非法组合进入 service：%d %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, "data-ui-error") {
				t.Fatalf("失败应渲染错误片段，实际 %s", firstBlockN(body, 400))
			}
			for _, leak := range []string{"CreateForm", "json:", "cannot unmarshal"} {
				if strings.Contains(body, leak) {
					t.Fatalf("绑定原文泄漏到响应里（%q）：%s", leak, firstBlockN(body, 400))
				}
			}
		})
	}
}

func TestCreatePresetUnavailableGivesControlledText(t *testing.T) {
	svc := &presetCreateStub{err: blockenums.Err(blockenums.ErrBlockPresetUnavailable)}
	rec := createPresetResponse(t, svc, url.Values{
		"projectId": {"project"}, "name": {"预设"},
		"presetKey": {"missing"}, "presetVersion": {"1"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("预设缺失应 400，实际 %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, blockenums.ErrBlockPresetUnavailable) {
		t.Fatalf("提示里出现裸 key：%s", firstBlockN(body, 400))
	}
	// 没有注入词库时，失败片段退成该状态码档的通用文案：不泄漏裸 key，也不泄漏内部原文。
	if !strings.Contains(body, "参数有误") && !strings.Contains(body, response.InternalFallback) {
		t.Fatalf("缺词时应退成通用文案，实际 %s", firstBlockN(body, 400))
	}
}

// injectBlockWords 只给本用例注入它断言的词条。
func injectBlockWords(t *testing.T, entries map[string]map[string]string) {
	t.Helper()
	i18n.InjectForTest(entries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}
