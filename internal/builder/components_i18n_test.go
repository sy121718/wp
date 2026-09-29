package builder

// components_i18n_test.go — 组件文案与多语言声明的一致性契约（审计 I18N-019）。
//
// 多语言有两套机制，管的是不同的东西：
//   - Translatable 白名单：Props 里**作者填的**文案，经内容翻译（sys_translation）取译文；
//   - I18nAware：组件**自带的固定文案**（按钮文字 / aria-label / 空态提示），构建期取词。
//
// 两套机制缺一个都不会报错：缺 Translatable 是「作者填的文案翻不了」，
// 缺 I18nAware 是「按钮永远是中文」—— 都只在非默认语言的产物里才看得出来，
// 而不看非默认语言的站点太多了。
//
// 本测试扫全部组件模板里的**用户可见硬编码中文**（排除 Jet 注释与 HTML 注释），
// 要求该组件要么实现 I18nAware，要么在下面两张表里明确登记。
//
// 只看模板、不看 Go：Go 里的中文常常正是 I18nAware 的**兜底常量**
//（text 为 nil 时用），把它算成硬编码会得到一堆假阳性。模板里的中文文案
// 没有这层歧义 —— 它就是会烘进产物、访客会读到的那串字。
//
// 「扫描逻辑本身还有效」由两部分各自负责，见文件末尾两处断言的说明：
// 覆盖面看 TestComponentI18nDeclarations 的计数，正则在 TestComponentI18nScanLogic 里自检。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// i18nExemptions 无需 I18nAware 的组件 —— 理由必须写清，不能只写「暂不处理」。
//
// 当前为空。唯一疑似需要豁免的两例（globalref / layoutslot 模板里的
// `<!-- 全局块引用 -->`）在扫描阶段就被 HTML 注释过滤器排除了：注释不是访客可见
// 文案，进不了候选集就不需要豁免。机制保留是因为下一类豁免（纯图形组件）随时会出现。
var i18nExemptions = map[string]string{}

// i18nPending 已确认需要、尚未实现 I18nAware 的组件（归 I18N-010 逐组件补齐）。
//
// 测试断言「实际缺口 ⊆ 本表」：新增组件带文案却不实现会立刻失败，
// 而存量缺口保持**可见**（写在表里并注明归属）而不是被一条豁免悄悄盖住。
var i18nPending = map[string]string{}

var (
	cjkRe         = regexp.MustCompile(`[\p{Han}]`)
	jetCommentRe  = regexp.MustCompile(`(?s)\{\*.*?\*\}`)
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	applyI18nRe   = regexp.MustCompile(`func \([^)]*\) ApplyI18n\(`)
)

// visibleText 去掉注释后剩下的正文（注释里的中文不是访客可见文案）。
func visibleText(tpl string) string {
	tpl = jetCommentRe.ReplaceAllString(tpl, "")
	return htmlCommentRe.ReplaceAllString(tpl, "")
}

// hasVisibleCJK 模板里是否存在**访客可见**的中文（即「硬编码文案」的唯一判据）。
//
// 抽成函数是为了让它可被 TestComponentI18nScanLogic 用自造输入直接验证：
// 原来这层逻辑内联在主测试的循环里，只能靠「扫到了东西」间接推断它没坏 ——
// 而那个推断在 i18n 做完之后不再成立（见主测试末尾的说明）。
func hasVisibleCJK(tpl string) bool {
	return cjkRe.MatchString(visibleText(tpl))
}

// TestComponentI18nScanLogic 扫描逻辑的自检：判据不依赖生产模板的当前状态。
//
// 为什么需要它：主测试原先用「扫到的组件数 > 0」来证明扫描逻辑有效，但那个数会随
// i18n key 化的推进**趋近于零**（目标就是零），于是判据在改造完成时必然变红
// （实测：HEAD 版 2 个 → 组件模板全部 key 化后 0 个）。换成本测试之后，
// 「正则写坏 / 注释过滤失效」仍然会被抓住，且与生产模板有多少文案**无关**。
//
// 正负例覆盖三个过滤层：CJK 命中、Jet 注释 `{* *}` 排除、HTML 注释 `<!-- -->` 排除。
// 少测任何一层，那层的失效都会退化成「候选集变小」而无人察觉。
func TestComponentI18nScanLogic(t *testing.T) {
	cases := []struct {
		name string
		tpl  string
		want bool
	}{
		{"可见中文命中", `<button class="btn">提交</button>`, true},
		{"中文属性值也命中", `<input placeholder="请输入关键词">`, true},
		{"纯英文不命中", `<button class="btn">Submit</button>`, false},
		{"Jet 注释里的中文不命中", `{* 提交按钮：说明 *}<span>Submit</span>`, false},
		{"HTML 注释里的中文不命中", `<!-- 提交按钮 --><span>Submit</span>`, false},
		{"多行 Jet 注释里的中文不命中", "{*\n第一行说明\n第二行说明\n*}\n<p>ok</p>", false},
		{"注释之外有中文则命中", "{* 注释 *}<p>确定</p>", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasVisibleCJK(tc.tpl); got != tc.want {
				t.Fatalf("hasVisibleCJK(%q) = %v，期望 %v", tc.tpl, got, tc.want)
			}
		})
	}
}

func TestComponentI18nDeclarations(t *testing.T) {
	entries, err := os.ReadDir("components")
	if err != nil {
		t.Fatalf("读取组件目录失败: %v", err)
	}
	var checked, implemented, gaps []string
	// dirCount / jetCount 是**扫描覆盖面**的自检计数：它们把原判据里「路径有没有读对」
	// 这一半独立出来（见末尾断言）。结果集非空做不到这件事 —— 它无法区分
	//「目录读错了」与「文案已全部搬走」。
	var dirCount, jetCount int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirCount++
		dir := filepath.Join("components", e.Name())
		files, derr := os.ReadDir(dir)
		if derr != nil {
			continue
		}
		var hasText, hasI18n bool
		for _, f := range files {
			data, rerr := os.ReadFile(filepath.Join(dir, f.Name()))
			if rerr != nil {
				continue
			}
			switch {
			case strings.HasSuffix(f.Name(), ".jet"):
				jetCount++
				if hasVisibleCJK(string(data)) {
					hasText = true
				}
			case strings.HasSuffix(f.Name(), ".go"):
				if applyI18nRe.MatchString(string(data)) {
					hasI18n = true
				}
			}
		}
		if !hasText {
			continue // 模板里没有硬编码文案：不需要 I18nAware
		}
		checked = append(checked, e.Name())
		if hasI18n {
			implemented = append(implemented, e.Name())
			continue
		}
		if _, ok := i18nExemptions[e.Name()]; ok {
			continue
		}
		if _, ok := i18nPending[e.Name()]; ok {
			continue
		}
		gaps = append(gaps, e.Name())
	}
	// 覆盖面：扫不到组件目录或 .jet 文件 = 路径 / 工作目录变了，此时后面所有断言
	// 都会「因为没东西可查」而通过 —— 这正是要 fail-fast 的那种假绿。
	if dirCount == 0 || jetCount == 0 {
		t.Fatalf("组件扫描覆盖面为零：读到 %d 个组件目录、%d 个 .jet 模板（路径或工作目录变了）",
			dirCount, jetCount)
	}
	if len(checked) == 0 {
		// 不是故障，是**目标状态**：模板里的硬编码文案已被搬进 I18nAware 的取词常量。
		// 「扫描逻辑还有效」由上面的计数与 TestComponentI18nScanLogic 的正负例保证。
		t.Logf("全部 %d 个组件（%d 个 .jet 模板）均无访客可见中文 —— i18n key 化已完成，这是目标状态",
			dirCount, jetCount)
	}
	if len(gaps) > 0 {
		sort.Strings(gaps)
		for _, name := range gaps {
			t.Errorf("组件 %s 的模板里有访客可见的硬编码中文，但既没实现 I18nAware、也不在豁免/待办表里", name)
		}
		t.Fatalf("补齐 I18nAware（推荐）或登记到 i18nExemptions / i18nPending 并写明归属")
	}
	// 登记表的反向检查：两张表里的条目必须**仍然是缺口**。
	// 组件补齐 I18nAware 之后没人删表，登记就会变成僵尸 —— 后来者看到「这里还有活」，
	// 而实际上它只是没人清理。缺口消失比缺口存在更难被发现，所以让它在测试里响。
	done := map[string]bool{}
	for _, name := range implemented {
		done[name] = true
	}
	withText := map[string]bool{}
	for _, name := range checked {
		withText[name] = true
	}
	for name := range i18nPending {
		if done[name] {
			t.Errorf("i18nPending 里的 %s 已实现 I18nAware，请从待办表删掉", name)
		}
		// 扫描根本没发现文案 = 这个登记是假的：要么文案已经被搬走、要么它本来就没有
		// 硬编码文案。两种情况下留在表里都会让人以为「这里还有活」。
		if !withText[name] {
			t.Errorf("i18nPending 里的 %s 扫描不到硬编码文案，请从待办表删掉", name)
		}
	}
	for name := range i18nExemptions {
		if done[name] {
			t.Errorf("i18nExemptions 里的 %s 已实现 I18nAware，豁免可以撤掉了", name)
		}
	}
	t.Logf("含硬编码文案的组件 %d 个（其中 %d 个已实现 I18nAware，%d 个待办，%d 个豁免）",
		len(checked), len(implemented), len(i18nPending), len(i18nExemptions))
}
