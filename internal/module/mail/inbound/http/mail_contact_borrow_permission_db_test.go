package mailhttp_test

import (
	"context"
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
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

var mailDeclareDBRe = regexp.MustCompile(`g\.POST\("(/[^"]+)",[^)\n]*?`)

type mailDeclaredBorrow struct {
	Act string
	Obj string
}

func mailPageDeclaredBorrows(t *testing.T) []mailDeclaredBorrow {
	t.Helper()
	raw, err := os.ReadFile("mail_router.go")
	if err != nil {
		t.Fatalf("读 mail_router.go 失败：%v", err)
	}
	seen := map[string]bool{}
	out := make([]mailDeclaredBorrow, 0)
	for _, m := range mailDeclareDBRe.FindAllStringSubmatch(string(raw), -1) {
		obj := "/api/mail" + m[1]
		key := "POST " + obj
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, mailDeclaredBorrow{Act: "POST", Obj: obj})
	}
	if len(out) == 0 {
		t.Fatal("没有扫到任何 API 写路由 —— 正则与注册写法脱节，本用例会静默变成空转")
	}
	return out
}

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

func TestMailPageDeclaredBorrowsHaveEnabledPermissionRows(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := schema.Apply(db); err != nil {
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

func TestMailPageDeclaredBorrowPolicyAuthorizesPagePost(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := schema.Apply(db); err != nil {
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
	// 启用角色现在是事务内原子写（ActivateRoleTx），提交后与生产一样重载策略：
	// 不重载时内存副本仍认为角色未启用，后面的请求会一律 403。
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, aerr := casbin.ActivateRoleTx(context.Background(), tx, roleCode)
		return aerr
	}); err != nil {
		t.Fatal(err)
	}
	if err := casbin.ReloadPolicy(); err != nil {
		t.Fatal(err)
	}

	for i, b := range borrows {
		if code := call(i); code == http.StatusForbidden || hits[b.Obj] == 0 {
			t.Fatalf("角色已授 %s %s 的权限，请求仍被挡：status=%d hits=%d", b.Act, b.Obj, code, hits[b.Obj])
		}
	}
}
