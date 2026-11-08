// 外部测试包：生产代码里 pkg/view/jet → pkg/view（引擎适配层反向装上自己），
// 因此包内测试（package view）不能再 import jet —— 那会构成测试期的 import cycle。
// 这里用 package view_test，与包内测试同编进一个测试二进制，TestMain 对两者都生效。
package view_test

import (
	"os"
	"testing"

	jetpkg "go_wp/pkg/view/jet"
)

// TestMain 给 pkg/view 的整包测试装上真实 Jet 渲染作用域。
// 装配入口与生产一致（jet.InstallRenderScope），出口行为不是桩出来的。
func TestMain(m *testing.M) {
	jetpkg.InstallRenderScope()
	os.Exit(m.Run())
}
