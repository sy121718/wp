package templates

// keyframes_vocab_boundary_test.go — 组件私有关键帧与共享动效词汇表的边界契约（审计条目 UIK-008）。
//
// 前台有一份 63 条的动效词汇表（internal/builder/core/keyframes/*.css，由 builder 按
// Interaction / Entrance 属性**按需激活**），组件目录的 .css 里另有一批私有 @keyframes。
// 两者不是"该合并却没合并"，各自表达的东西不同：
//
//	词汇表 —— 页面级入场 / 循环效果，静态调色板，名字与帧内容固定；
//	私有帧 —— 由组件数据决定：逐卡几何（sky-cs-<id>-<n>，cardstack.go keyframesName）、
//	          按张数算百分比时间轴（sky-bg-fade-<n>）、按实例算位移（sky-marquee-<id>）、
//	          9 种加载形态（sky-loader-*，SpinKit 形态一一对应）、
//	          组件内交互反馈（sky-tabs-fade 面板切换、sky-sd-drift 漂移、sky-counter-run
//	          靠 --sky-count 自定义属性插值）。
//
// 为什么不直接改用词汇表名：产物里必须真的存在那个 @keyframes，否则 builder/css_verify.go
// 的 verifyAnimationRefs 会拒绝（"产物引用了未定义的关键帧"）；而词汇表的激活权在 Go 侧。
// 组件 CSS 单方面改名 = 让构建失败。反向合并（无条件输出一份共用定义）则给每个产物加字节，
// 并把两个语义独立的形态绑成一体。
//
// 所以本测试不"收敛"它们，只钉住三条边界：
//  ① 私有字面量名与词汇表名不得重合 —— 同名会让产物里两份 @keyframes 互相覆盖，
//     后拼接的胜出，表现为"动画按另一个组件的参数跑"这类静默错位；
//  ② 运行时生成的帧名必须有固定前缀，且该前缀不得是词汇表名的前缀（同上，只是换在运行期）；
//  ③ 新增模板变量帧名必须登记 —— 不登记就等于出现无人审过的动画命名空间。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	reCSSCommentBlock = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reKeyframeDefName = regexp.MustCompile(`@keyframes\s+([^\s]+)`)
)

// templateKeyframeNames 组件 CSS 里允许出现的模板变量帧名（运行时展开），值写明展开后的命名空间。
// 新增一项意味着承认一个新的动画命名空间，必须同时确认它与词汇表无前缀冲突。
var templateKeyframeNames = map[string]string{
	"{{card.kf}}":                "sky-cs-<id>-<n>（cardstack.go keyframesName）",
	"sky-marquee-{{id}}":         "sky-marquee-<id>（marquee 每实例一份）",
	"sky-bg-fade-{{slideCount}}": "sky-bg-fade-<n>（container 每张数一份）",
}

// runtimeKeyframePrefixes 上表展开后的固定前缀，用于与词汇表做前缀冲突检查。
var runtimeKeyframePrefixes = []string{"sky-cs-", "sky-marquee-", "sky-bg-fade-"}

// vocabKeyframeSource 共享动效词汇表的两个源文件（相对本包目录）。
var vocabKeyframeSource = []string{
	"../builder/core/keyframes/builtin.css",
	"../builder/core/keyframes/animate.css",
}

// TestComponentKeyframesDoNotShadowVocabulary 组件私有帧名不得与共享词汇表同名或同前缀。
func TestComponentKeyframesDoNotShadowVocabulary(t *testing.T) {
	vocab := readKeyframeNames(t, vocabKeyframeSource...)
	if len(vocab) != 63 {
		t.Fatalf("动效词汇表解析出 %d 条（预期 63）：解析口径可能已失效", len(vocab))
	}
	vocabSet := make(map[string]bool, len(vocab))
	for _, n := range vocab {
		vocabSet[n] = true
	}

	files, err := filepath.Glob("../builder/components/*/*.css")
	if err != nil {
		t.Fatalf("枚举组件 CSS 失败: %v", err)
	}
	if len(files) < 30 {
		t.Fatalf("只找到 %d 个组件 CSS（预期 ≥30）：路径口径可能已变", len(files))
	}

	var literal, templated []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		// 先去掉注释块：shapedivider.css 的一条说明性注释里也写着 "@keyframes"。
		body := reCSSCommentBlock.ReplaceAllString(string(raw), "")
		for _, m := range reKeyframeDefName.FindAllStringSubmatch(body, -1) {
			name := strings.TrimSuffix(m[1], "{")
			if strings.Contains(name, "{{") {
				templated = append(templated, name)
				continue
			}
			literal = append(literal, name)
		}
	}
	if len(literal)+len(templated) < 15 {
		t.Fatalf("组件私有 @keyframes 只解析出 %d 处（预期 ≥15）：解析口径可能已失效",
			len(literal)+len(templated))
	}

	// ① 字面量名与词汇表交集必须为空。
	var shadowed []string
	for _, n := range literal {
		if vocabSet[n] {
			shadowed = append(shadowed, n)
		}
	}
	sort.Strings(shadowed)
	if len(shadowed) > 0 {
		t.Errorf("组件私有帧与共享词汇表同名（产物里两份定义会互相覆盖）: %v", shadowed)
	}

	// ② 模板变量帧名必须在表里登记。
	registered := 0
	for _, n := range templated {
		ns, ok := templateKeyframeNames[n]
		if !ok {
			t.Errorf("组件 CSS 出现未登记的模板变量帧名 %q：请在 templateKeyframeNames 登记它的运行时命名空间", n)
			continue
		}
		if ns == "" {
			t.Errorf("模板变量帧名 %q 登记了空命名空间说明", n)
			continue
		}
		registered++
	}

	// ③ 运行时前缀不得与词汇表名互为前缀（运行期同名同样会覆盖）。
	for _, p := range runtimeKeyframePrefixes {
		for _, n := range vocab {
			if strings.HasPrefix(n, p) {
				t.Errorf("词汇表条目 %q 落在组件运行时命名空间 %q 里：运行期会与组件私有帧同名", n, p)
			}
		}
	}

	// 台账：私有帧数量变了要有人知道（这条不 fail，只记录）。
	t.Logf("组件私有关键帧 %d 处（字面量 %d + 模板 %d），其中已登记命名空间 %d 项；共享词汇表 %d 条",
		len(literal)+len(templated), len(literal), len(templated), registered, len(vocab))
	sort.Strings(literal)
	sort.Strings(templated)
	t.Logf("字面量私有帧: %v", literal)
	t.Logf("模板私有帧: %v", templated)
}

// readKeyframeNames 从 CSS 源里取关键帧名（去注释后按 @keyframes 边界取）。
func readKeyframeNames(t *testing.T, paths ...string) []string {
	t.Helper()
	var out []string
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读取关键帧源 %s 失败: %v", p, err)
		}
		body := reCSSCommentBlock.ReplaceAllString(string(raw), "")
		for _, m := range reKeyframeDefName.FindAllStringSubmatch(body, -1) {
			out = append(out, strings.TrimSuffix(m[1], "{"))
		}
	}
	sort.Strings(out)
	return out
}
