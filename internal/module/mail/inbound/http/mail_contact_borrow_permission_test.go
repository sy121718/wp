package mailhttp

// mail_contact_borrow_permission_test.go — 页面写操作借用的 Casbin 对象必须「已声明 + 唯一可反解」。
//
// 背景（task-13 确认，2026-10-08）：邮箱后台的写操作按**对应 API 的路径**走 Casbin 权限点，
// 其中四个联系人页面 POST 借用的对象（/api/mail/contact/save、/delete、/tag）在运行时路由表里
// 没有对应端点 —— 它们是「页面借用对象」。授权链路的同源点是：
//
//	页面注册处 CasbinMiddlewareForPath(obj)
//	  ↔ 注册处 permission.Declare(POST, obj, perm)（否则 permission.RoutesOf 查空 →
//	    ai_handle.go 的 casbinAuthorizer fail closed，用到它的 AI 工具一律 forbidden）
//	  ↔ sys_permission 里该权限点行的 api_path/api_method（角色授权的 [path,method,code]
//	    三元组从这张表反查，见 admin/service/role.go RoleMenuSave）
//
// 后两条由本文件（声明）与 mail_contact_borrow_permission_db_test.go（库行 + 角色授权实测）钉住。
//
// 判据刻意**不包含**「这条路径不是真实路由」：按既定方向（task-13 决策乙）后续会给这些写操作补
// 真实 JSON API、前端直调、再删页面 POST —— 那时页面不再借用、对象就是真实路由。把「没有路由」
// 写成长期不变的硬断言，会让新 API 正常上线反而测试变红。因此本文件只守「借用了谁，就必须声明谁、
// 且恰好反解出一个权限点」，借用集合变化（新增 / 删除）都由源码扫描自动跟随。

import (
	"os"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	"go_wp/internal/permission"
)

var (
	mailPagePostRe = regexp.MustCompile(`pages\.POST\("([^"]+)",\s*(?:builtin\.)?CasbinMiddlewareForPath(?:As)?\("(/api/[^"]+)"`)
	mailDeclareRe  = regexp.MustCompile(`permission\.Declare\("([A-Z]+)",\s*"([^"]+)"`)
)

// TestMailPagePostBorrowObjectsAreDeclared 每条页面 POST 借用的对象都要声明，且恰好反解出一个权限点。
func TestMailPagePostBorrowObjectsAreDeclared(t *testing.T) {
	// 声明表是包级装配状态，由路由注册填充；本用例只扫源码不装配，故显式登记一次（Declare 幂等）。
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
			// 期望形态：一条路径 ↔ 一个权限点。
		default:
			t.Errorf("页面 POST %s 借用的 %s 被多个权限点声明：%v —— 判权只按 (路径, 方法) 匹配，会互相串权", page, obj, matched)
		}
	}
}

// TestMailPagePostRoutesAreRegistered 扫到的页面 POST 必须真的挂在 /admin 页面组上。
//
// 与上一条同一份借用集合：页面迟早会被删（决策乙），那时这里按空集合自然通过，不需要改判据。
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

// mailPagePostBorrows 从注册源码扫出「页面 POST → 借用的鉴权对象」。
func mailPagePostBorrows(raw string) map[string]string {
	out := map[string]string{}
	for _, m := range mailPagePostRe.FindAllStringSubmatch(raw, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// mailPageRegisterStub 只为注册路由而存在：嵌入接口满足签名，被调用即测试漏覆。
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
