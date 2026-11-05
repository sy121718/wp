package architecture

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// check-i18n-keys-seeded.sh 的「判据自身」回归。
//
// 为什么要有它：本门禁会**排除**写基座的能力标识（data-write-handler /
// data-write-builder / data-picker-fetch 与两种引号的注册点）。排除写宽了，真缺的
// 词条被一起抹掉 → 静默假绿；写窄了，每加一个写基座能力就要给代码标识符编一条中文
// 译文 → 假红，最后必然被绕过。两个方向都得有可重复的用例，且必须落在仓库里 ——
// 脚本注释里的 /tmp 探针没人会重跑，判据坏了也没人知道。
//
// 用例直接用脚本的 GOWP_ROOT 指向一个临时 root：不依赖仓库现状，也不会因为
// 别域的存量债务变红。
func TestI18nKeysSeededGateRecognisesCapabilityIdents(t *testing.T) {
	script := filepath.Join(repoRoot(t), "scripts", "check-i18n-keys-seeded.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("找不到门禁脚本 %s：%v", script, err)
	}

	root := t.TempDir()
	writeGateFixture(t, filepath.Join(root, "internal", "templates", "probe.html"), `<!doctype html>
<form data-write-handler="admin.probe.capOnly"></form>
<form data-write-handler="admin.probe.capOnSameLine">{{tr("admin.probe.realSameLine", "x")}}</form>
<form data-write-extra="admin.probe.extraNotCapability"></form>
<div data-filter-input data-row-add></div>
<p>{{tr("admin.probe.alsoReal", "y")}}</p>
`)
	writeGateFixture(t, filepath.Join(root, "internal", "templates", "probe.js"), `
ui.writeBase.registerResultHandler("admin.probe.capDoubleQuoted", function () {});
ui.writeBase.registerResultHandler('admin.probe.capSingleQuoted', function () {});
ui.writeBase.registerResultHandler("admin.probe.alsoReal", function () {});
ui.writeBase.registerPayloadBuilder('admin.probe.builderOnly', function () { return {}; });
`)
	// seed 侧：只给一条无关词条，四条能力标识都**不该**被视为词条。
	writeGateFixture(t, filepath.Join(root, "public", "migrations", "x.sql"), `INSERT INTO sys_i18n (item_key, lang) VALUES
('admin.unrelated.key', 'zh-CN'),
('admin.unrelated.key', 'en-US');
`)

	listFile := filepath.Join(t.TempDir(), "missing.txt")
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"GOWP_ROOT="+root,
		"I18N_KEYS_BASELINE="+filepath.Join(t.TempDir(), "baseline.txt"),
		"I18N_KEYS_LIST="+listFile,
		"I18N_KEYS_UNPAIRED="+filepath.Join(t.TempDir(), "unpaired.txt"),
	)
	out, _ := cmd.CombinedOutput()

	// 基线是空文件 → 有缺失必然退出 1。这里只关心「缺的是哪几条」，所以不断言退出码，
	// 但脚本崩掉（找不到目录、语法错）要能看出来。
	text := string(out)
	if strings.Contains(text, "找不到目录") {
		t.Fatalf("门禁脚本没有在临时 root 上运行：%s", text)
	}

	missing := readGateList(t, listFile)

	// ① 同一行里既有能力标识又有真 tr() key：只许抹标识，不许连带抹掉真词条。
	assertGateHas(t, missing, "admin.probe.realSameLine")
	// ② data-write-extra 不是注册名字：值与 key 形状撞车时必须照常报红。
	assertGateHas(t, missing, "admin.probe.extraNotCapability")
	// ③ 能力名与真词条同名（又出现在 registerResultHandler 里，又当词条用）：
	//    词条没 seed 就必须报红 —— 这是「排除」不能变成「整行抹掉」的界线。
	assertGateHas(t, missing, "admin.probe.alsoReal")

	// 反例：四条能力标识必须一条都不出现（两种引号都要认）。
	for _, banned := range []string{
		"admin.probe.capOnly",
		"admin.probe.capOnSameLine",
		"admin.probe.capDoubleQuoted",
		"admin.probe.capSingleQuoted",
		"admin.probe.builderOnly",
	} {
		if _, ok := missing[banned]; ok {
			t.Errorf("%q 是写基座的能力标识、不是词条，不该被要求 seed：%v", banned, keysOfGateList(missing))
		}
	}

	// 计数必须精确等于 3：多了说明排除写窄（假红），少了说明写宽（假绿）。
	if len(missing) != 3 {
		t.Fatalf("应恰好报 3 条缺失（realSameLine / extraNotCapability / alsoReal），实际 %d 条：%v",
			len(missing), keysOfGateList(missing))
	}
}

func writeGateFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录 %s 失败：%v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("写夹具 %s 失败：%v", path, err)
	}
}

// readGateList 读门禁写出的缺失清单。格式是 "<key>\t(<文件>)" 每行一条。
func readGateList(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}
		}
		t.Fatalf("读缺失清单 %s 失败：%v", path, err)
	}
	out := map[string]struct{}{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, _, _ := strings.Cut(line, "\t")
		out[strings.TrimSpace(key)] = struct{}{}
	}
	return out
}

func assertGateHas(t *testing.T, got map[string]struct{}, want string) {
	t.Helper()
	if _, ok := got[want]; !ok {
		t.Errorf("应报缺失 %q，实际清单：%v", want, keysOfGateList(got))
	}
}

func keysOfGateList(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
