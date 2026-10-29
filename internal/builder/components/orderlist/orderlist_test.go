package orderlist

import (
	"go_wp/internal/builder/core"
	"net/url"
	"strings"
	"testing"
)

func TestOrderDataViewUsesModuleFactsWithoutSiteSlots(t *testing.T) {
	view := BuildView(&Props{PageSize: 25}, "project", "en-US&projectId=evil")
	u, err := url.Parse(view.ReadURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != OrdersPath || u.Query().Get("projectId") != "project" || u.Query().Get("limit") != "25" || u.Query().Get("lang") != "en-US&projectId=evil" {
		t.Fatalf("事实入口漂移: %s", view.ReadURL)
	}
	if view.HasLoginAction {
		t.Fatal("不可猜登录目标")
	}
	if view.DetailURL == "" || view.ReturnableURL == "" || view.ReturnsURL == "" {
		t.Fatal("完整业务读取入口缺失")
	}
	if got := BuildView(&Props{}, "", ""); got.Notice == "" || got.ReadURL != "" {
		t.Fatal("缺上下文必须可见而不请求无效入口")
	}
}
func TestOrderPartsAndStylesAreInstanceScoped(t *testing.T) {
	if len(Widget.Parts()) != 4 || len(Widget.RequiredActions()) != 0 {
		t.Fatal("内部四步骤应完整声明，登录仅可选目标")
	}
	var a, b core.CSSBuckets
	CompileCSS("a", &Props{}, &a)
	CompileCSS("b", &Props{}, &b)
	if a.String() == b.String() || !strings.Contains(a.String(), ".sky-c-a .sky-orders-row") || !strings.Contains(a.String(), "[hidden]") {
		t.Fatal("样式未按实例隔离或覆盖了hidden")
	}
}
