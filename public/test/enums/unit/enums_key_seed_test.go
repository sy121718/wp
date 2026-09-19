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

// enumsFiles 模块 → enums 文件（只列**要检查**的模块）。
//
// 判据是「全库 seed 里有没有这个 key」，所以这里不再写「该模块的 seed 文件」。
// 原来写的是 (enums 文件, seed 文件) 配对，而那个配对从第二批起就在腐烂：
// 每个模块后来又各自加了归口文案与补漏词条的迁移（mail 的 276、navigation 的 269、
// admin 的 268/271…），列表里却只有一个文件 —— 于是**后续批次新增的 key 一律被判成漏配**。
// 实测：`mail.err.internal`（276 seed 得很完整，中英各一行）在这一版里就是红的，
// 而这个门禁从那时起一直在报假红。假红的门禁比没有门禁更糟：它会被改宽或被忽略。
//
// sys_i18n 是全局表，key 落在哪个迁移文件里不影响「有没有词条」；
// 真正要抓的漏配是「任何地方都没有」—— 那用全库扫描判就够了，且不需要维护。
var enumsFiles = map[string]string{
	"cart":  "internal/module/cart/enums/cart_enums.go",
	"order": "internal/module/order/enums/order_enums.go",
	"user":  "internal/module/user/enums/user_enums.go",
	"mail":  "internal/module/mail/enums/mail_enums.go",
}

func TestEnumsKeysHaveSeedEntries(t *testing.T) {
	root := moduleTestRoot(t)
	enumsRe := regexp.MustCompile(`(?m)^\s*\w+\s*=\s*"([a-z][a-z0-9]*\.(?:msg|err|test)\.[A-Za-z0-9_.]+)"`)
	seedRe := regexp.MustCompile(`(?m)^\s*\(\s*'([^']+)'`)

	// 全库 seed 的 key 集合：一次扫完 public/migrations 下全部 .sql。
	// 单独抽出来只扫一遍，顺带让「扫到了几个 key」可断言 —— 扫到 0 个说明判据坏了，
	// 而那种情况下所有模块都会「全部漏配」或「全部通过」，两种都看不出判据已经失效。
	have := map[string]bool{}
	seedFiles, err := filepath.Glob(filepath.Join(root, "public", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("枚举 seed 文件失败: %v", err)
	}
	for _, sf := range seedFiles {
		src, rerr := os.ReadFile(sf)
		if rerr != nil {
			t.Fatalf("读取 seed %s 失败: %v", sf, rerr)
		}
		for _, m := range seedRe.FindAllStringSubmatch(string(src), -1) {
			have[m[1]] = true
		}
	}
	if len(have) == 0 {
		t.Fatalf("全库 seed 里没扫到任何 key（共 %d 个 .sql）—— 判据或目录结构变了", len(seedFiles))
	}

	for module, enumsFile := range enumsFiles {
		enumsSrc, err := os.ReadFile(filepath.Join(root, enumsFile))
		if err != nil {
			t.Fatalf("读取 %s 的 enums 失败: %v", module, err)
		}
		want := map[string]bool{}
		for _, m := range enumsRe.FindAllStringSubmatch(string(enumsSrc), -1) {
			want[m[1]] = true
		}
		if len(want) == 0 {
			t.Fatalf("%s：enums 里没扫到任何 key，判据可能已失效", module)
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
