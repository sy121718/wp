package routers

import (
	"fmt"
	"os"
	"testing"

	"go_wp/internal/permission"
)

func TestDumpRoutesForAudit(t *testing.T) {
	if os.Getenv("WP_DUMP_ROUTES") != "1" {
		t.Skip("设置 WP_DUMP_ROUTES=1 才运行（需要数据库与完整组件装配）")
	}
	for _, l := range bootstrapRouter(t) {
		fmt.Println(l)
	}

	for _, spec := range permission.Snapshot() {
		fmt.Printf("DECLARED %s %s %s\n", spec.Method, spec.Path, spec.Perm)
	}
	for _, e := range permission.Exempts() {
		fmt.Printf("EXEMPT %s\n", e)
	}
}
