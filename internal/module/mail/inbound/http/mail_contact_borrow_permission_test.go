package mailhttp

import (
	"os"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
)

var (
	mailPagePostRe = regexp.MustCompile(`pages\.POST\("([^"]+)",[^)\n]*?CasbinMiddlewareForPath(?:As)?\("(/api/[^"]+)"`)
	mailAPIPostRe  = regexp.MustCompile(`g\.POST\("(/[^"]+)",\s*(?:pageview\.HTMLFragment,\s*)?mailenums\.(\w+)`)
)

func TestMailPagePostBorrowObjectsAreDeclared(t *testing.T) {
	raw := readMailPageRouterSource(t)
	borrows := mailPagePostBorrows(raw)
	if len(borrows) == 0 {
		t.Fatal("没有扫到任何页面 POST 的鉴权对象 —— 正则与注册写法脱节，本用例会静默变成空转")
	}

	apiRaw, err := os.ReadFile("mail_router.go")
	if err != nil {
		t.Fatalf("读 mail_router.go 失败：%v", err)
	}
	apiObjs := map[string]bool{}
	for _, m := range mailAPIPostRe.FindAllStringSubmatch(string(apiRaw), -1) {
		apiObjs["/api/mail"+m[1]] = true
	}
	if len(apiObjs) == 0 {
		t.Fatal("没有扫到任何 API 写路由 —— 正则与注册写法脱节")
	}

	for page, obj := range borrows {
		if !apiObjs[obj] {
			t.Errorf("页面 POST %s 借用了 %s，但 mail_router.go 里没有对应的真实 API 写路由 —— 权限绑定只来自真实注册处", page, obj)
		}
	}
}

func TestMailPagePostRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	pages := r.Group("/admin")
	setupMailPageRoutes(pages, &mailPageRegisterStub{})

	registered := map[string]bool{}
	for _, route := range r.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	borrows := mailPagePostBorrows(readMailPageRouterSource(t))
	if len(borrows) == 0 {
		t.Fatal("没有扫到任何页面 POST 的鉴权对象 —— 正则与注册写法脱节")
	}
	for page := range borrows {
		if !registered["POST /admin"+page] {
			t.Errorf("页面 POST /admin%s 没有注册（扫到的借用集合里有它）", page)
		}
	}
}

func mailPagePostBorrows(raw string) map[string]string {
	out := map[string]string{}
	for _, m := range mailPagePostRe.FindAllStringSubmatch(raw, -1) {
		out[m[1]] = m[2]
	}
	return out
}

type mailPageRegisterStub struct {
	mailcontract.MailService
}

func readMailPageRouterSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("mail_page_router.go")
	if err != nil {
		t.Fatalf("读 mail_page_router.go 失败：%v", err)
	}
	return string(raw)
}
