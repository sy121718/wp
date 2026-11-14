package routers

import (
	"fmt"
	"os"
	"testing"
)

func TestDumpRoutesForAudit(t *testing.T) {
	if os.Getenv("WP_DUMP_ROUTES") != "1" {
		t.Skip("设置 WP_DUMP_ROUTES=1 才运行（需要数据库与完整组件装配）")
	}
	for _, l := range bootstrapRouter(t) {
		fmt.Println(l)
	}

	// 契约工具使用真实处理器身份，不从资源路径猜方法名；路由快照仍只记录 METHOD+PATH。
	for _, handler := range routeDumpHandlers {
		fmt.Println(handler)
	}
	// 读本次装配自己的索引：包级默认实例不在生产装配链上。
	for _, spec := range auditRegistry.Snapshot() {
		// 同时导出真实入口、权限码与复用的鉴权对象：审计要能区分
		// 「这个路径需要哪项权限」和「这条权限被检查的是哪个对象/方法」。
		fmt.Printf("DECLARED %s %s %s |%s|%s|%s|%s\n",
			spec.Method, spec.Path, spec.Perm, spec.Kind, spec.AuthObject, spec.AuthMethod, spec.Path)
	}
	for _, e := range auditRegistry.Exempts() {
		fmt.Printf("EXEMPT %s\n", e)
	}
}
