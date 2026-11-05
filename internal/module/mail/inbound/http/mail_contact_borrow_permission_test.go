package mailhttp

import (
	"os"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	"go_wp/internal/permission"
)

var (
	mailPagePostRe = regexp.MustCompile(`pages\.POST\("([^"]+)",[^)\n]*?CasbinMiddlewareForPath(?:As)?\("(/api/[^"]+)"`)
	mailDeclareRe  = regexp.MustCompile(`permission\.Declare\("([A-Z]+)",\s*"([^"]+)"`)
)

func TestMailPagePostBorrowObjectsAreDeclared(t *testing.T) {

	declareMailPageObjects()

	raw := readMailPageRouterSource(t)
	borrows := mailPagePostBorrows(raw)
	if len(borrows) == 0 {
		t.Fatal("没有扫到任何页面 POST 的鉴权对象 —— 正则与注册写法脱节，本用例会静默变成空转")
	}

	declaredObjs := map[string]bool{}
	for _, m := range mailDeclareRe.FindAllStringSubmatch(raw, -1) {
		if m[1] == "POST" {
			declaredObjs[m[2]] = true
		}
	}

	for page, obj := range borrows {
		if !declaredObjs[obj] {
			t.Errorf("页面 POST %s 借用了 %s，但注册处没有 permission.Declare —— permission.RoutesOf 查不到它，用到它的 AI 工具一律 forbidden", page, obj)
			continue
		}
		var matched []permission.Perm
		for _, p := range permission.All() {
			for _, r := range permission.RoutesOf(p) {
				if r.Path == obj && r.Method == "POST" {
					matched = append(matched, p)
				}
			}
		}
		switch len(matched) {
		case 0:
			t.Errorf("页面 POST %s 借用的 %s 在声明表里查不到权限点 —— AI 工具调用会被判越权（fail closed）", page, obj)
		case 1:

		default:
			t.Errorf("页面 POST %s 借用的 %s 被多个权限点声明：%v —— 判权只按 (路径, 方法) 匹配，会互相串权", page, obj, matched)
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
