package builder

// css_verify.go — 产物自洽校验：样式里引用的动画名，必须在同一份产物里有定义。
//
// 为什么需要这一层：动效名有三个来源 ——
//   ① core 按动效词汇表拼（EffectKeyframeName）；
//   ② 组件写死的字面量（cardstack 的逐卡错落效果）；
//   ③ 将来插件注册的词汇。
// 任何一处与关键帧源脱钩，产物都会变成「animation 引用了不存在的 @keyframes」：
// 构建成功、字节合法、样式表解析也不报错，页面上那个动效就是不动。
// 三者各自的单元测试只能覆盖自己，产物级校验是唯一一次覆盖全部来源的位置。
//
// 只查 `sky-` 前缀：那是本项目的动画命名空间（自产自用）。插件经 extraCSS 带进来的
// 自家动画名不在这个命名空间里，不在此列，避免误报。

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// animationNamespace 本项目的动画命名空间前缀。
const animationNamespace = "sky-"

var (
	reKeyframeDef  = regexp.MustCompile(`@keyframes\s+([A-Za-z0-9_-]+)`)
	reAnimationRef = regexp.MustCompile(`animation(?:-name)?:\s*([^;}]+)`)
)

// verifyAnimationRefs 校验产物 CSS 自洽：每个被引用的 sky-* 动画都要有对应关键帧。
func verifyAnimationRefs(css string) error {
	defined := make(map[string]bool)
	for _, m := range reKeyframeDef.FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	var missing []string
	seen := make(map[string]bool)
	for _, m := range reAnimationRef.FindAllStringSubmatch(css, -1) {
		// 简写里可以有多个动画（逗号并接：入场 + 循环），一格一格看。
		// 名字不一定是第一个 token（CSS 允许 `2s ease sky-x` 这种顺序），
		// 所以取段内第一个 sky- 开头的 token 而不是位置写死。
		for _, seg := range strings.Split(m[1], ",") {
			for _, tok := range strings.Fields(seg) {
				if !strings.HasPrefix(tok, animationNamespace) {
					continue
				}
				if !defined[tok] && !seen[tok] {
					seen[tok] = true
					missing = append(missing, tok)
				}
				break // 一段只取一个名字
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing) // 稳定顺序：同一份输入给出同一份报错
	return fmt.Errorf("产物引用了未定义的关键帧: %s（动画名来自动效词汇表，对应关键帧应写在 core/keyframes/ 里）",
		strings.Join(missing, ", "))
}
