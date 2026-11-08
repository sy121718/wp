package middleware

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// 两个装配实例各持一份索引：一个 Reset/Freeze 不影响另一个。
// 这是「测试按实例重新装配」的核心保证 —— 包级默认实例不在生产装配链上。
func TestRegistryInstancesAreIsolated(t *testing.T) {
	first := NewRegistry()
	second := NewRegistry()

	first.Declare(http.MethodGet, "/api/first", "first:read", RouteProtected)
	second.Declare(http.MethodGet, "/api/second", "second:read", RouteProtected)

	if got := first.Snapshot(); len(got) != 1 || got[0].Path != "/api/first" {
		t.Fatalf("第一个实例应只有自己的声明：%+v", got)
	}
	if got := second.Snapshot(); len(got) != 1 || got[0].Path != "/api/second" {
		t.Fatalf("第二个实例应只有自己的声明：%+v", got)
	}

	first.Reset()
	if got := second.Snapshot(); len(got) != 1 {
		t.Fatalf("重置一个实例不能清掉另一个实例的索引：%+v", got)
	}
}

// 一个实例冻结后，另一个实例仍可登记；冻结只约束本实例。
func TestRegistryFreezeIsPerInstance(t *testing.T) {
	first := NewRegistry()
	second := NewRegistry()

	first.Declare(http.MethodGet, "/api/frozen", "frozen:read", RouteProtected)
	first.Freeze()
	if !first.Frozen() {
		t.Fatal("冻结后 Frozen 应为 true")
	}
	if second.Frozen() {
		t.Fatal("冻结只约束本实例，另一个实例不应被冻结")
	}
	second.Declare(http.MethodGet, "/api/live", "live:read", RouteProtected)
	if len(second.Snapshot()) != 1 {
		t.Fatalf("未冻结实例应能继续登记：%+v", second.Snapshot())
	}
}

// 路由组用装配索引登记，子组继承同一个索引。
func TestRouteGroupSharesRegistryWithChildren(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	reg := NewRegistry()
	group := NewRouteGroupIn(reg, engine.Group("/api"), RouteProtected)
	group.POST("/parent", "parent:create", func(c *gin.Context) { c.Status(http.StatusOK) })
	child := group.Group("/child")
	child.GET("/item", "child:read", func(c *gin.Context) { c.Status(http.StatusOK) })

	if len(reg.Snapshot()) != 2 {
		t.Fatalf("父组与子组应登记进同一个索引：%+v", reg.Snapshot())
	}
	if got := reg.RoutesOf("child:read"); len(got) != 1 || got[0].Path != "/api/child/item" {
		t.Fatalf("子组登记的路径应为完整路径：%+v", got)
	}
}

// 包级默认实例的 Reset 不影响新建实例：生产装配的索引不来自默认实例。
func TestDefaultRegistryResetDoesNotAffectNewInstance(t *testing.T) {
	reg := NewRegistry()
	reg.Declare(http.MethodGet, "/api/service", "service:read", RouteProtected)
	Reset()
	if len(reg.Snapshot()) != 1 {
		t.Fatalf("默认实例的 Reset 不能清掉新实例的索引：%+v", reg.Snapshot())
	}
}
