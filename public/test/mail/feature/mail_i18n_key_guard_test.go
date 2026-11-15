package feature

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
	mailI18nGuardProjectRoot = "../../../.."

	mailI18nGuardTemplateDir = "internal/templates/admin/mail"
	mailI18nGuardModuleDir   = "internal/module/mail"

	mailI18nGuardSeedDir = "public/schema/tables"

	mailI18nGuardMinUsedKeys = 50
)

var (
	mailI18nGuardUsedRe = regexp.MustCompile(`"(admin\.mail\.[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)"`)

	mailI18nGuardSeedRe = regexp.MustCompile(`'(admin\.mail\.[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)'`)
)

func mailI18nGuardTrim(hit string) string {
	if idx := strings.Index(hit, "admin.mail."); idx >= 0 {
		hit = hit[idx:]
	}
	return strings.Trim(strings.TrimSpace(hit), `"'`)
}

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
		t.Errorf("以下 %d 个 admin.mail.* 键在模板/模块代码里用了，但 public/schema 里没有种值：\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	t.Logf("扫描完成：取词侧 %d 个键，种值侧 %d 个键", len(used), len(seeded))
}

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

var mailI18nMigratedFallbacks = map[string]string{

	"admin.mail.marketing.contacts.empty":  "可调整筛选条件，或点右上角「导入联系人」批量导入。",
	"admin.mail.marketing.campaigns.empty": "新建活动后点「启动群发」开始发送。",
	"admin.mail.campaign.links_empty":      "启动群发后，有收件人点击邮件链接才会显示排行。",
	"admin.mail.campaign.recipients_empty": "启动群发后，收件人会分批进入投递队列。",
	"admin.mail.automation.empty":          "先新建一条，比如「新订阅 → 等 1 天 → 发欢迎邮件 → 打上 welcomed 标签」。",
	"admin.mail.automation.runs.empty":     "流程启用后，满足触发条件的人会自动进入。",

	"admin.mail.marketing.contacts.empty.initial.title": "还没有联系人",

	"admin.mail.marketing.contacts.empty.initial": "新建一位联系人手工加一条，或用「导入联系人」批量导入 CSV —— 名单建立起来才能按标签圈人群群发。",

	"admin.mail.campaign.back":                  "返回群发活动",
	"admin.mail.automation.bulk_delete_confirm": "删除选中的流程？进行中的实例会先停止。",

	"admin.mail.campaign.empty.action": "回群发活动页启动",
}

var mailI18nGuardFallbackRe = regexp.MustCompile(`"(admin\.mail\.[A-Za-z0-9_.]+)"\s*,\s*"([^"]*)"`)

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
