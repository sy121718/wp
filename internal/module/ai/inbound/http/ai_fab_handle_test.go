package aihttp

import (
	"context"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

func fabForm(fields map[string]string) *gin.Context {
	c, _ := gin.CreateTestContext(nil)
	req := httptestNewForm(fields)
	c.Request = req
	return c
}

func TestComposeFabInputIncludesPageContext(t *testing.T) {
	got := composeFabInput(fabForm(map[string]string{
		"input":    "这周卖得怎么样",
		"ctxPath":  "/admin/orders",
		"ctxQuery": "?status=paid",
		"ctxTitle": "订单管理",
	}), "这周卖得怎么样")
	if !strings.HasPrefix(got, "（当前页面：订单管理 /admin/orders?status=paid）\n") {
		t.Fatalf("上下文行格式不对：%q", got)
	}
	if !strings.HasSuffix(got, "这周卖得怎么样") {
		t.Fatalf("问题必须原样保留在末尾：%q", got)
	}
}

func TestComposeFabInputOmitsMissingContext(t *testing.T) {
	got := composeFabInput(fabForm(map[string]string{"input": "你好"}), "你好")
	if got != "你好" {
		t.Fatalf("没有上下文时应当只有问题本身，实得 %q", got)
	}
	if strings.Contains(got, "当前页面") {
		t.Error("不得写上下文占位")
	}
}

func TestComposeFabInputTruncatesLongQuery(t *testing.T) {
	long := "?" + strings.Repeat("a=1&", 200)
	got := composeFabInput(fabForm(map[string]string{
		"input": "问问", "ctxPath": "/admin/customers", "ctxQuery": long,
	}), "问问")
	if !strings.Contains(got, "（已截断）") {
		t.Error("截断必须可见 —— 不说明的话模型会以为这就是全部筛选条件")
	}

	if len([]rune(got)) > fabContextQueryLimit+200 {
		t.Errorf("截断没生效，长度 %d", len([]rune(got)))
	}
}

type fakeProviders struct {
	list []aidto.Provider
	err  error
}

func (f fakeProviders) ListProviders(context.Context) ([]aidto.Provider, error) {
	return f.list, f.err
}

func TestFabDefaultModelSkipsUnusable(t *testing.T) {
	h := &SessionPageHandle{providers: fakeProviders{list: []aidto.Provider{
		{ProviderKey: "disabled", Status: 0, HasAPIKey: true, Models: []aidto.ModelEntry{{ID: "a"}}},
		{ProviderKey: "nokey", Status: aiProviderEnabled, HasAPIKey: false, Models: []aidto.ModelEntry{{ID: "b"}}},
		{ProviderKey: "nomodels", Status: aiProviderEnabled, HasAPIKey: true},
		{ProviderKey: "ok", Status: aiProviderEnabled, HasAPIKey: true, Models: []aidto.ModelEntry{{ID: "m-1"}}},
	}}}
	key, model, ok := h.defaultModel(context.Background())
	if !ok || key != "ok" || model != "m-1" {
		t.Fatalf("应选中第一个可用的（ok/m-1），实得 %q/%q/%v", key, model, ok)
	}
}

func TestFabDefaultModelNoneAvailable(t *testing.T) {
	h := &SessionPageHandle{providers: fakeProviders{}}
	if _, _, ok := h.defaultModel(context.Background()); ok {
		t.Fatal("没有可用供应商时应回 false —— 回空串会让请求走到出站层再失败")
	}

	empty := &SessionPageHandle{}
	if _, _, ok := empty.defaultModel(context.Background()); ok {
		t.Fatal("端口缺席时应回 false")
	}
}

func TestFabTextResolvesKeysToMessages(t *testing.T) {
	for _, key := range []string{
		aienums.MsgFabNoModel, aienums.MsgFabEmptyInput, aienums.MsgFabFailed,
		aienums.MsgFabTruncated, aienums.MsgFabToolsPrefix, aienums.MsgFabToolsSuffix,
		aienums.MsgFabEmpty,
	} {
		got := response.MsgText(fabForm(nil), facing.Msg{Key: key})
		if got == "" {
			t.Errorf("%s 没有文案", key)
		}
		if got == key {
			t.Errorf("%s 没查到文案（会把 key 渲染给用户）", key)
		}
		if strings.Contains(got, "ai.fab.") {
			t.Errorf("%s 的文案里混进了 key：%q", key, got)
		}
	}
}
