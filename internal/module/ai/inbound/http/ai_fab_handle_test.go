package aihttp

// ai_fab_handle_test.go — 悬浮球的纯逻辑断言（D9 的页面上下文 + 默认模型挑选）。

import (
	"context"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
)

// fabForm 造一个带指定表单字段的 gin.Context（只用到 PostForm）。
func fabForm(fields map[string]string) *gin.Context {
	c, _ := gin.CreateTestContext(nil)
	req := httptestNewForm(fields)
	c.Request = req
	return c
}

// TestComposeFabInputIncludesPageContext 页面上下文要进输入，且格式可辨认。
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

// TestComposeFabInputOmitsMissingContext 上下文缺失时**不写占位**。
//
// 写「（当前页面：（无））」会让模型以为自己在一个空页面上，
// 而真相是「这次请求没带上下文」—— 前者会引出更多错误的猜测。
func TestComposeFabInputOmitsMissingContext(t *testing.T) {
	got := composeFabInput(fabForm(map[string]string{"input": "你好"}), "你好")
	if got != "你好" {
		t.Fatalf("没有上下文时应当只有问题本身，实得 %q", got)
	}
	if strings.Contains(got, "当前页面") {
		t.Error("不得写上下文占位")
	}
}

// TestComposeFabInputTruncatesLongQuery 超长 query 要截断并说明。
func TestComposeFabInputTruncatesLongQuery(t *testing.T) {
	long := "?" + strings.Repeat("a=1&", 200)
	got := composeFabInput(fabForm(map[string]string{
		"input": "问问", "ctxPath": "/admin/customers", "ctxQuery": long,
	}), "问问")
	if !strings.Contains(got, "（已截断）") {
		t.Error("截断必须可见 —— 不说明的话模型会以为这就是全部筛选条件")
	}
	// 截断后整行仍要远短于原串（fabContextQueryLimit 是按 rune 计的）。
	if len([]rune(got)) > fabContextQueryLimit+200 {
		t.Errorf("截断没生效，长度 %d", len([]rune(got)))
	}
}

// fakeProviders 供应商读取端口的假实现。
type fakeProviders struct {
	list []aidto.Provider
	err  error
}

func (f fakeProviders) ListProviders(context.Context) ([]aidto.Provider, error) {
	return f.list, f.err
}

// TestFabDefaultModelSkipsUnusable 默认模型要跳过停用 / 无密钥 / 无目录的供应商。
//
// 三条判据缺一不可：停用的选了会跑在不该跑的地方；没密钥的必然调不通，
// 而错误会指向出站层；目录空的选不出模型名。
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

// TestFabDefaultModelNoneAvailable 一个可用的都没有时回 ok=false（不是回空串当成功）。
func TestFabDefaultModelNoneAvailable(t *testing.T) {
	h := &SessionPageHandle{providers: fakeProviders{}}
	if _, _, ok := h.defaultModel(context.Background()); ok {
		t.Fatal("没有可用供应商时应回 false —— 回空串会让请求走到出站层再失败")
	}
	// 端口缺席（未装配）同样 false。
	empty := &SessionPageHandle{}
	if _, _, ok := empty.defaultModel(context.Background()); ok {
		t.Fatal("端口缺席时应回 false")
	}
}
