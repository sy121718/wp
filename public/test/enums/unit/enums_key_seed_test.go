package unit

// enums_key_seed_test.go — enums 的 key 与词条 seed 必须一一对应（审计 I18N-002）。
//
// 这条 finding 把模块 enums 的值从中文文案改成了 i18n key。改造本身是机械的，
// 风险全在**漏配**：加一个 ErrXxx 却忘了同批 seed，响应层查不到就原样吐出 key，
// 用户看到的是「cart.err.someNewCase」—— 在默认语言站点上尤其刺眼，
// 而这类漏配不会让任何编译或测试失败。
//
// 所以这里做纯文件级核对：把 enums 里出现的全部 key 与同模块的 seed SQL 里的 key 求差集。
// 不依赖数据库、不依赖 i18n 缓存，跑一次几毫秒。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// enumsSeedPairs 模块 → (enums 文件, seed 文件)。
var enumsSeedPairs = map[string][2]string{
	"cart":  {"internal/module/cart/enums/cart_enums.go", "public/migrations/179_i18n_seed_cart.sql"},
	"order": {"internal/module/order/enums/order_enums.go", "public/migrations/180_i18n_seed_order.sql"},
	"user":  {"internal/module/user/enums/user_enums.go", "public/migrations/181_i18n_seed_user.sql"},
	"mail":  {"internal/module/mail/enums/mail_enums.go", "public/migrations/182_i18n_seed_mail.sql"},
}

func TestEnumsKeysHaveSeedEntries(t *testing.T) {
	root := moduleTestRoot(t)
	enumsRe := regexp.MustCompile(`(?m)^\s*\w+\s*=\s*"([a-z][a-z0-9]*\.(?:msg|err|test)\.[A-Za-z0-9_.]+)"`)
	seedRe := regexp.MustCompile(`(?m)^\s*\(\s*'([^']+)'`)

	for module, files := range enumsSeedPairs {
		enumsSrc, err := os.ReadFile(filepath.Join(root, files[0]))
		if err != nil {
			t.Fatalf("读取 %s 的 enums 失败: %v", module, err)
		}
		seedSrc, err := os.ReadFile(filepath.Join(root, files[1]))
		if err != nil {
			t.Fatalf("读取 %s 的 seed 失败: %v", module, err)
		}
		want := map[string]bool{}
		for _, m := range enumsRe.FindAllStringSubmatch(string(enumsSrc), -1) {
			want[m[1]] = true
		}
		if len(want) == 0 {
			t.Fatalf("%s：enums 里没扫到任何 key，判据可能已失效", module)
		}
		have := map[string]bool{}
		for _, m := range seedRe.FindAllStringSubmatch(string(seedSrc), -1) {
			have[m[1]] = true
		}
		var missing []string
		for k := range want {
			if !have[k] {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s：这些 key 在 enums 里但 seed 里没有词条（用户会直接看到 key）：%s",
				module, strings.Join(missing, ", "))
		}
	}
}

// moduleTestRoot 定位仓库根（测试工作目录是包目录）。
func moduleTestRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd 失败: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("未找到仓库根（go.mod）")
	return ""
}
