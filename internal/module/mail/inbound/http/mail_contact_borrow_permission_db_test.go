package mailhttp_test

// mail_contact_borrow_permission_db_test.go — 页面借用对象的库侧一致性（需要真实 PG）。
//
// 同批事实的源码面在 mail_contact_borrow_permission_test.go（声明 + 唯一反解，不需要数据库）。
// 这里补两条只有连库才能回答的问题：
//
//	① 注册处声明的借用对象在 sys_permission 里是否有启用行、且 api_path/api_method 逐字一致？
//	   角色授权的 [path, method, code] 三元组就是从这张表反查的（admin/service/role.go RoleMenuSave）：
//	   路径漂移 / 缺行 / 方法不符的表现都是「授权界面勾得上或勾不上，但功能一定打不开」，
//	   而且**没有任何一处会报错**（历史形态：页面路由搬走后权限点还指着旧路径）。
//	② 策略挂在该对象上时，页面 POST 是否真的放行？
//	   模型 m = (r.sub == p.sub || (g(r.sub,p.sub) && g2(p.sub,"active"))) && r.obj == p.obj && r.act == p.act
//	   —— 判权只认 (路径, 方法)，code 列只用于文案；但必须**实测**，写错一个字符就是永久 403。
//
// 授权按**角色形态**（p, role_code, obj, act, code + g, user, role + g2, role, active）：真实后台走
// 角色（sys_role → RoleMenuSave）；直接给用户授权会绕开 g/g2 分支，把「模型只认启用角色」漏掉。
//
// 集合口径：注册源码扫出的 permission.Declare 对象 ∩ 本库已有 sys_permission 行的那一批。
// 交集不是折中，而是**两个真源的分工**：有真实 API 路由的对象由 API 注册处的声明在启动期经
// SyncToDB 落行（种子库不物化它们，种子只管页面专用对象），而「页面专用对象」正是没有 API 注册点
// 能替它声明、只能靠注册处 Declare + 迁移种子的那一批 —— task-13 要核的就是它们。
// 决策乙给这些写操作补真实 JSON API 后，声明与借用一起消失，集合自然收缩为空；届时删除本文件
// （下面每条都带「扫不到就显式失败」的守卫，不会静默空转）。这里**没有**「必须不是真实路由」
// 这类反向断言。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	builtin "go_wp/internal/middleware/builtin"
	"go_wp/pkg/casbin"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

var mailDeclareDBRe = regexp.MustCompile(`permission\.Declare\("([A-Z]+)",\s*"(/api/[^"]+)"`)

// mailDeclaredBorrow 注册处声明的借用对象（enforce 方法 + 路径）。
type mailDeclaredBorrow struct {
	Act string
	Obj string
}

// mailPageDeclaredBorrows 扫 mail_page_router.go 里的 permission.Declare，去重并稳定排序。
func mailPageDeclaredBorrows(t *testing.T) []mailDeclaredBorrow {
	t.Helper()
	raw, err := os.ReadFile("mail_page_router.go")
	if err != nil {
		t.Fatalf("读 mail_page_router.go 失败：%v", err)
	}
	seen := map[string]bool{}
	out := make([]mailDeclaredBorrow, 0)
	for _, m := range mailDeclareDBRe.FindAllStringSubmatch(string(raw), -1) {
		key := m[1] + " " + m[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, mailDeclaredBorrow{Act: m[1], Obj: m[2]})
	}
	if len(out) == 0 {
		t.Fatal("没有扫到任何 permission.Declare —— 正则与注册写法脱节，本用例会静默变成空转")
	}
	return out
}

// seedMaterializedBorrows 取「注册处声明 ∩ 本库有 sys_permission 行」的借用对象。
func seedMaterializedBorrows(t *testing.T, db *gorm.DB) []mailDeclaredBorrow {
	t.Helper()
	out := make([]mailDeclaredBorrow, 0)
	for _, b := range mailPageDeclaredBorrows(t) {
		var n int64
		if err := db.Table("sys_permission").Where("api_path = ? AND api_method = ?", b.Obj, b.Act).Count(&n).Error; err != nil {
			t.Fatalf("统计 sys_permission（%s %s）失败：%v", b.Act, b.Obj, err)
		}
		if n > 0 {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		t.Fatal("页面注册处声明的借用对象没有任何一条被种子物化 —— 借用消失时请连同本用例一起删除，不要让它静默空转")
	}
	return out
}

// TestMailPageDeclaredBorrowsHaveEnabledPermissionRows 声明的借用对象必须有启用且逐字一致的权限点行。
func TestMailPageDeclaredBorrowsHaveEnabledPermissionRows(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("跑种子数据失败：%v", err)
	}

	borrows := seedMaterializedBorrows(t, db)
	t.Logf("页面注册处声明的借用对象 %d 条，其中种子物化的 %d 条：%v", len(mailPageDeclaredBorrows(t)), len(borrows), borrows)

	for _, b := range borrows {
		var rows []struct {
			Code      string `gorm:"column:permission_code"`
			APIMethod string `gorm:"column:api_method"`
			Status    int16  `gorm:"column:status"`
		}
		if err := db.Table("sys_permission").
			Select("permission_code, api_method, status").
			Where("api_path = ?", b.Obj).
			Find(&rows).Error; err != nil {
			t.Fatalf("查 sys_permission（api_path=%s）失败：%v", b.Obj, err)
		}
		if len(rows) == 0 {
			t.Errorf("注册处声明的借用对象 %s 在 sys_permission 里没有任何行 —— 角色授权勾不上，页面写操作对任何账号（含超管）都是 403", b.Obj)
			continue
		}
		if len(rows) > 1 {
			t.Errorf("借用对象 %s 对应多个权限点：%+v —— 判权只按 (路径, 方法) 匹配，会互相串权", b.Obj, rows)
		}
		if rows[0].APIMethod != b.Act {
			t.Errorf("权限点 %s 的 api_method=%s，注册处 enforce 的是 %s %s —— 方法不匹配时策略永不命中（294 的教训：含超管全员 403）",
				rows[0].Code, rows[0].APIMethod, b.Act, b.Obj)
		}
		if rows[0].Status != 1 {
			t.Errorf("权限点 %s 的 status=%d（非启用）—— 授权树里列不出来，勾不上", rows[0].Code, rows[0].Status)
		}
	}
}

// TestMailPageDeclaredBorrowPolicyAuthorizesPagePost 借用对象上挂策略时，页面 POST 必须放行。
func TestMailPageDeclaredBorrowPolicyAuthorizesPagePost(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("跑种子数据失败：%v", err)
	}
	borrows := seedMaterializedBorrows(t, db)
	if len(borrows) == 0 {
		t.Fatal("页面注册处声明的借用对象没有任何一条被种子物化 —— 借用消失时请连同本用例一起删除，不要让它静默空转")
	}

	if err := casbin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := casbin.InitCasbin(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = casbin.Close() })

	const (
		userID   = int64(987654322)
		roleCode = "mail_contact_borrow_probe"
	)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 中间件与页面注册处逐字同形：obj 就是注册处声明的借用对象。
	hits := map[string]int{}
	group := r.Group("/admin", func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	})
	for i, b := range borrows {
		obj, act := b.Obj, b.Act
		group.POST("/probe"+strconv.Itoa(i), builtin.CasbinMiddlewareForPathAs(obj, act), func(c *gin.Context) {
			hits[obj]++
			c.Status(http.StatusNoContent)
		})
	}

	call := func(i int) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/probe"+strconv.Itoa(i), strings.NewReader("id=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	for i, b := range borrows {
		if code := call(i); code != http.StatusForbidden || hits[b.Obj] != 0 {
			t.Fatalf("没有策略时 %s %s 应 403 且不进 handler：status=%d hits=%d", b.Act, b.Obj, code, hits[b.Obj])
		}
	}

	// 角色形态授权：策略 obj/act = 注册处声明的借用对象，code 取库存真值（模型不比对它，
	// 但空 code 的策略行不是真实形态，也给授权树反查留下可读性）。
	policies := make([][3]string, 0, len(borrows))
	for _, b := range borrows {
		var row struct {
			Code string `gorm:"column:permission_code"`
		}
		if err := db.Table("sys_permission").Select("permission_code").Where("api_path = ? AND api_method = ?", b.Obj, b.Act).Take(&row).Error; err != nil {
			t.Fatalf("取 %s %s 的权限点失败：%v", b.Act, b.Obj, err)
		}
		policies = append(policies, [3]string{b.Obj, b.Act, row.Code})
	}
	if err := casbin.ReplaceRolePermissions(roleCode, policies); err != nil {
		t.Fatal(err)
	}
	if err := casbin.ReplaceRoleUsers(roleCode, []string{"987654322"}); err != nil {
		t.Fatal(err)
	}
	if err := casbin.ActivateRole(roleCode); err != nil {
		t.Fatal(err)
	}

	for i, b := range borrows {
		if code := call(i); code == http.StatusForbidden || hits[b.Obj] == 0 {
			t.Fatalf("角色已授 %s %s 的权限，请求仍被挡：status=%d hits=%d", b.Act, b.Obj, code, hits[b.Obj])
		}
	}
}
