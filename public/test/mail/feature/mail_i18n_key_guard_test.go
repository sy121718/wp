package feature

// mail_i18n_key_guard_test.go — 邮件域 i18n 键「用了但没种」的常驻守卫。
//
// 这一类缺陷（2026-10 的 P2）长这样：模板里 `.["t"]("admin.mail.x.y", "兜底文案")`
// 只在第二参数里有文案、sys_i18n 里没有对应词条。页面看着完全正常 —— 走 fallback，
// 中文用户看不出问题；但切到 en-US 就露出中文兜底，而且系统翻译页里搜不到这个键，
// 运营没有任何入口能维护它。它不是渲染错，所以页面测试抓不到。
//
// 判据一条：
//
//	used   = internal/templates/admin/mail/ 与 internal/module/mail/ 里出现的字面量 admin.mail.* 键
//	seeded = public/migrations/*.sql 里种过的 'admin.mail.*' 键
//	used - seeded 必须为空
//
// 为什么在 Go 里重写而不是调 grep/comm：comm 要求两侧同一 locale 排序，两侧排序不同
// 会给出**假阴性**（上一批踩过一次）；而守卫必须在 go test 里独立跑，不能依赖 shell。
//
// 与 CI 门禁的关系（两条都留着、互补，不要再新建第三个脚本）：
//
//	scripts/check-i18n-keys-seeded.sh（scripts/check-all.sh:35 调用，基线
//	scripts/i18n-keys-seeded-baseline.txt）口径更严：只认 INSERT INTO sys_i18n 的
//	VALUES 元组 —— 键写进 SQL 注释、或写进 register 的 ConditionSQL 判据列表，都不算
//	已种值；范围是全仓 admin.* / workbench.*。本文件的价值是**包级快速反馈**（跟着
//	邮件模块的改动一起红在 go test 里），但它按「迁移文件里出现过该键文本」计数，
//	挡不住「键只写在注释里」这一形态 —— 那一条由上面那个 shell 门禁兜。

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// mailI18nGuardProjectRoot 工程根：go test 的 CWD 是包目录 public/test/mail/feature。
	mailI18nGuardProjectRoot = "../../../.."
	// mailI18nGuardUsedDirs 取词侧：模板与模块代码（模板是运行时解析的，模块代码里也有拼 key 的地方）。
	mailI18nGuardTemplateDir = "internal/templates/admin/mail"
	mailI18nGuardModuleDir   = "internal/module/mail"
	// mailI18nGuardSeedDir 种值侧：所有迁移（含 *.sql 里的 INSERT 与 UPDATE 的键）。
	mailI18nGuardSeedDir = "public/migrations"
	// mailI18nGuardMinUsedKeys 取词侧的最小键数：低于它说明扫描本身失败了（目录改名 / walk 出错），
	// 而不是「真的没有键」—— 空转的守卫比没有守卫更危险。
	mailI18nGuardMinUsedKeys = 50
)

var (
	// 双引号形态：模板的 .["t"]("key", "兜底")、Go 代码里的 map 键。
	// 键必须紧跟在引号后、逐段非空、且**以闭合引号收尾** —— 这三点缺一不可：
	// 允许前缀或允许中间出现引号，会让正则跨越整段注释（注释里写的 `"admin.mail.x` 示例
	// 曾把一大段 Go 注释当成一个「键」报出来）。
	mailI18nGuardUsedRe = regexp.MustCompile(`"(admin\.mail\.[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)"`)
	// 单引号形态：迁移 SQL 里的 'key'。
	mailI18nGuardSeedRe = regexp.MustCompile(`'(admin\.mail\.[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)'`)
)

// mailI18nGuardTrim 从匹配片段里截出纯键（去掉引号与可能的前缀）。
func mailI18nGuardTrim(hit string) string {
	if idx := strings.Index(hit, "admin.mail."); idx >= 0 {
		hit = hit[idx:]
	}
	return strings.Trim(strings.TrimSpace(hit), `"'`)
}

// mailI18nGuardUsedKeys 从一段源码里抽出全部取词侧的字面量键（纯函数，便于自检）。
func mailI18nGuardUsedKeys(src string) []string {
	seen := map[string]bool{}
	var out []string
	for _, hit := range mailI18nGuardUsedRe.FindAllString(src, -1) {
		key := mailI18nGuardTrim(hit)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// mailI18nGuardSeededKeys 从一段迁移 SQL 里抽出全部已种值的键（纯函数，便于自检）。
func mailI18nGuardSeededKeys(src string) []string {
	seen := map[string]bool{}
	var out []string
	for _, hit := range mailI18nGuardSeedRe.FindAllString(src, -1) {
		key := mailI18nGuardTrim(hit)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// mailI18nGuardCollect 递归收集一个目录下所有匹配文件里的键。
func mailI18nGuardCollect(t *testing.T, dir string, wantExt map[string]bool, extract func(string) []string) []string {
	t.Helper()

	abs := filepath.Join(mailI18nGuardProjectRoot, dir)
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("扫描目录 %s 不存在（守卫会静默空转）: %v", dir, err)
	}

	seen := map[string]bool{}
	var out []string
	files := 0
	err := filepath.WalkDir(abs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !wantExt[filepath.Ext(path)] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for _, key := range extract(string(src)) {
			if !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败: %v", dir, err)
	}
	if files == 0 {
		t.Fatalf("%s 下没有可扫描的文件（后缀过滤写错会让守卫空转）", dir)
	}
	sort.Strings(out)
	return out
}

// TestMailI18nUsedKeysAreSeeded 常驻守卫：模板与模块代码里用到的每个 admin.mail.* 键，
// 都必须在迁移里种过值。缺一个就是「切语言露中文兜底、运营搜不到键」。
func TestMailI18nUsedKeysAreSeeded(t *testing.T) {
	used := mailI18nGuardCollect(t, mailI18nGuardTemplateDir, map[string]bool{".html": true}, mailI18nGuardUsedKeys)
	used = append(used, mailI18nGuardCollect(t, mailI18nGuardModuleDir, map[string]bool{".go": true}, mailI18nGuardUsedKeys)...)

	seeded := mailI18nGuardCollect(t, mailI18nGuardSeedDir, map[string]bool{".sql": true}, mailI18nGuardSeededKeys)
	seededSet := map[string]bool{}
	for _, key := range seeded {
		seededSet[key] = true
	}

	if len(used) < mailI18nGuardMinUsedKeys {
		t.Fatalf("取词侧只扫到 %d 个键（阈值 %d）：扫描范围或后缀过滤出错了，守卫会空转",
			len(used), mailI18nGuardMinUsedKeys)
	}

	var missing []string
	for _, key := range used {
		if !seededSet[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("以下 %d 个 admin.mail.* 键在模板/模块代码里用了，但 public/migrations 里没有种值：\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	t.Logf("扫描完成：取词侧 %d 个键，种值侧 %d 个键", len(used), len(seeded))
}

// TestMailI18nKeyGuardIsNotVacuous 守卫自检：抽取逻辑要能分辨「用了」与「种了」，
// 并且不许把人造键误判成已种值 —— 否则上面那条用例永远通过。
func TestMailI18nKeyGuardIsNotVacuous(t *testing.T) {
	const usedSample = `
		<h1>{{ .["t"]("admin.mail.accounts.heading", "发信账号") }}</h1>
		<p>{{ .["t"]("admin.mail.never.seeded.key", "只有兜底") }}</p>
	`
	const seededSample = `
		INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
		('admin.mail.accounts.heading', 'zh-CN', '发信账号');
	`

	usedKeys := mailI18nGuardUsedKeys(usedSample)
	if len(usedKeys) != 2 {
		t.Fatalf("取词侧样本应抽出 2 个键，实际 %v", usedKeys)
	}
	seededKeys := mailI18nGuardSeededKeys(seededSample)
	if len(seededKeys) != 1 || seededKeys[0] != "admin.mail.accounts.heading" {
		t.Fatalf("种值侧样本应抽出 1 个键，实际 %v", seededKeys)
	}

	seededSet := map[string]bool{}
	for _, key := range seededKeys {
		seededSet[key] = true
	}
	if !seededSet["admin.mail.accounts.heading"] {
		t.Error("已种值的键被判成缺失：抽取逻辑错了")
	}
	if seededSet["admin.mail.never.seeded.key"] {
		t.Error("人造键被判成已种值：差集判据失效，守卫会永远通过")
	}
}

// mailI18nMigratedFallbacks 「本轮在迁移里统一过文案」的键 → 模板 fallback 的应有值。
//
// 只守这几个键、不做全量 DB↔fallback 比对，是有意的取舍：
//   - fallback 与 DB 值在一般情况下**允许**不同（DB 值运营可改，fallback 只是
//     键缺失/新库时的兜底），全量比对要先跑完整条迁移链算出每键终值，
//     成本高，而且与各迁移自己的终态用例重复；
//   - 反过来，「刚在迁移里统一过的文案又在模板里漂回去」是纯静态可判的，
//     而本轮三次 P2（437 → 522 → 523）正是这个形态：模板改了、库里没跟，
//     或者库里改了、模板漏改 —— 两个方向都会让「切语言时露出别的页名」复发。
//
// 值取各键的**最终**状态（437 的 marketing.contacts.empty 后来被 522 覆盖）。
var mailI18nMigratedFallbacks = map[string]string{
	// 437（空态描述统一）
	"admin.mail.marketing.contacts.empty":  "可调整筛选条件，或点右上角「导入联系人」批量导入。",
	"admin.mail.marketing.campaigns.empty": "新建活动后点「启动群发」开始发送。",
	"admin.mail.campaign.links_empty":      "启动群发后，有收件人点击邮件链接才会显示排行。",
	"admin.mail.campaign.recipients_empty": "启动群发后，收件人会分批进入投递队列。",
	"admin.mail.automation.empty":          "先新建一条，比如「新订阅 → 等 1 天 → 发欢迎邮件 → 打上 welcomed 标签」。",
	"admin.mail.automation.runs.empty":     "流程启用后，满足触发条件的人会自动进入。",
	// 441（联系人无筛选空态）
	"admin.mail.marketing.contacts.empty.initial.title": "还没有联系人",
	// 526 覆盖 441 的描述：页面新增了「新建联系人」入口，旧文案只指向导入。
	"admin.mail.marketing.contacts.empty.initial": "新建一位联系人手工加一条，或用「导入联系人」批量导入 CSV —— 名单建立起来才能按标签圈人群群发。",
	// 522（拆页后的页名与补种）
	"admin.mail.campaign.back":                  "返回群发活动",
	"admin.mail.automation.bulk_delete_confirm": "删除选中的流程？进行中的实例会先停止。",
	// 523（活动空态动作的旧页名）
	"admin.mail.campaign.empty.action": "回群发活动页启动",
}

// mailI18nGuardFallbackRe 抽 (key, fallback) 对：模板里 `.`/`tr` 两种取词形态
// 都是「键字符串后紧跟逗号再跟兜底字符串」，`\s*` 允许中间换行。
var mailI18nGuardFallbackRe = regexp.MustCompile(`"(admin\.mail\.[A-Za-z0-9_.]+)"\s*,\s*"([^"]*)"`)

// mailI18nGuardFallbackPairs 从源码里抽「键 → 出现过的全部 fallback」（去重，保持出现顺序）。
func mailI18nGuardFallbackPairs(src string) map[string][]string {
	out := map[string][]string{}
	for _, m := range mailI18nGuardFallbackRe.FindAllStringSubmatch(src, -1) {
		key, fallback := m[1], m[2]
		found := false
		for _, existed := range out[key] {
			if existed == fallback {
				found = true
				break
			}
		}
		if !found {
			out[key] = append(out[key], fallback)
		}
	}
	return out
}

// TestMailI18nTemplateFallbackMatchesMigratedValue 常驻守卫：迁移统一过的文案，
// 模板 fallback 必须与之一致；同一个键在模板里出现多种 fallback 也是缺陷
// （两处取词文案不同，用户看到的取决于渲染到哪一处）。
func TestMailI18nTemplateFallbackMatchesMigratedValue(t *testing.T) {
	pairs := map[string][]string{}
	files := 0
	err := filepath.WalkDir(filepath.Join(mailI18nGuardProjectRoot, mailI18nGuardTemplateDir),
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".html" {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files++
			for key, fallbacks := range mailI18nGuardFallbackPairs(string(src)) {
				for _, fallback := range fallbacks {
					seen := false
					for _, existed := range pairs[key] {
						if existed == fallback {
							seen = true
							break
						}
					}
					if !seen {
						pairs[key] = append(pairs[key], fallback)
					}
				}
			}
			return nil
		})
	if err != nil {
		t.Fatalf("遍历模板目录失败: %v", err)
	}
	if files == 0 {
		t.Fatal("没有扫到任何模板文件（守卫会空转）")
	}

	checked := 0
	for key, want := range mailI18nMigratedFallbacks {
		got, ok := pairs[key]
		if !ok {
			t.Errorf("%s 在模板里没有取词调用（迁移改了文案，页面却不再引用这个键）", key)
			continue
		}
		if len(got) > 1 {
			t.Errorf("%s 在模板里出现多种 fallback：%q（同一键两处文案不同）", key, got)
			continue
		}
		if got[0] != want {
			t.Errorf("%s 的模板 fallback = %q，期望 %q（迁移已统一过，模板漂回去了）", key, got[0], want)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("白名单里的键一个都没校验到：守卫空转")
	}
	t.Logf("校验了 %d 个迁移统一过的键（扫描 %d 个模板）", checked, files)
}

// TestMailI18nFallbackGuardIsNotVacuous 守卫自检：抽取函数必须能取出键与文案，
// 并且能分辨「漂回去的旧文案」。
func TestMailI18nFallbackGuardIsNotVacuous(t *testing.T) {
	pairs := mailI18nGuardFallbackPairs(
		`{{ .["t"]("admin.mail.campaign.back", "返回营销页") }}` + "\n" +
			`{{ tr := .["t"] }}{{ tr("admin.mail.campaign.empty.action", "回营销页启动群发") }}`)

	if got := pairs["admin.mail.campaign.back"]; len(got) != 1 || got[0] != "返回营销页" {
		t.Fatalf("抽取结果 = %v，期望一条「返回营销页」", got)
	}
	if got := pairs["admin.mail.campaign.empty.action"]; len(got) != 1 || got[0] != "回营销页启动群发" {
		t.Fatalf("tr 形态抽取结果 = %v，期望一条「回营销页启动群发」", got)
	}
	if pairs["admin.mail.campaign.back"][0] == mailI18nMigratedFallbacks["admin.mail.campaign.back"] {
		t.Error("旧文案被人造样本判成合法值：比对判据失效")
	}
}
