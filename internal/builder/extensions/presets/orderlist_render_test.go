package presets

import (
	"go_wp/pkg/builder"
	"context"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"
)

func TestOrderListCompilesCompleteJSONWorkflowWithoutSlots(t *testing.T) {
	set, err := templates.NewComponentSet("../../../../internal/templates/components")
	if err != nil {
		t.Fatal(err)
	}
	doc := []byte(`{"schemaVersion":1,"settings":{"layout":{"mode":"full"}},"root":[{"id":"orders-a","type":"core.orderList","props":{"title":"我的订单记录","showTitle":true,"pageSize":5}},{"id":"orders-b","type":"core.orderList","props":{}}]}`)
	page, err := builder.ParsePage(doc)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := builder.Compile(page, builder.WithCompileMode(builder.CompileModePublish), builder.WithComponentSet(set), builder.WithProjectID("project"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-component-instance="orders-a"`, `data-component-instance="orders-b"`, `data-site-view="ordersList"`, `/api/site/orders?`, `limit=5`, `data-component-part="list"`, `data-component-part="detail"`, `data-component-part="return"`, `data-component-part="result"`, `data-write-handler="commerce.orders"`, `我的订单记录`} {
		if !strings.Contains(cp.HTML, want) {
			t.Errorf("缺完整静态流程 %s", want)
		}
	}
	for _, bad := range []string{`/_fragments/`, `hx-get=`, `/account`, `href="/login"`, `costPrice`} {
		if strings.Contains(cp.HTML, bad) {
			t.Errorf("残留旧接线 %s", bad)
		}
	}
}

func TestOrderPresetHasAccountAndOwnOrderTargets(t *testing.T) {
	raw, err := (BusinessPresetRegistry{}).ResolvePreset(context.Background(), "commerce.orders", 1)
	if err != nil {
		t.Fatal(err)
	}
	page, err := builder.ParsePage(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	set, err := templates.NewComponentSet("../../../../internal/templates/components")
	if err != nil {
		t.Fatal(err)
	}
	cp, err := builder.Compile(page, builder.WithCompileMode(builder.CompileModePublish), builder.WithProjectID("project"), builder.WithComponentSet(set))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-component-action="login"`, `data-action-instance="orders-account"`, `data-action-part="login"`, `data-component-action="authenticated"`, `data-action-kind="local"`, `data-action-part="account"`} {
		if !strings.Contains(cp.HTML, want) {
			t.Errorf("默认流程未连接 %s", want)
		}
	}
}
