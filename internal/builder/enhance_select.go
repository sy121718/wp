package builder

// enhance_select.go — 增强脚本的按需拼装。
//
// 背景：enhance.js 有 8 个互不相关的增强（轮播 / 图集 / 计数器 / 倒计时 / 灯箱 /
// 卡片环 / 堆叠轮播 / 全屏分页），合计 22KB 左右，而每个产物都全量内联 ——
// 一个只用了 slide 的页面要背其余 85% 的用不到代码，纯内容页也背着 22KB。
//
// 做法：构建期按产物里实际出现的 data-* 特征挑选要注入的增强块，只拼这一份。
// 特征扫描前必须剥掉 script 块 —— enhance.js 自身内联在页面里，含全部 data-* 字样，
// 不剥离会让每个特征都「被检测到」，等于没裁。

import (
	"regexp"
	"strings"
)

// enhanceBlock 一个可独立注入的增强块。
type enhanceBlock struct {
	// fn 块内的初始化函数名（拼装时据此保留 onReady 的调用项）。
	fn string
	// feats 触发注入的产物特征（任一命中即注入）。
	feats []string
}

// enhanceBlocks 增强块清单，顺序即输出顺序（与 enhance.js 内的一致）。
var enhanceBlocks = []enhanceBlock{
	{fn: "initCounters", feats: []string{"data-counter"}},
	{fn: "initSliders", feats: []string{"data-slider"}},
	{fn: "initCarousels", feats: []string{"data-carousel"}},
	{fn: "initCountdowns", feats: []string{"data-countdown"}},
	{fn: "initLightboxes", feats: []string{"data-lightbox"}},
	{fn: "initCardStacks", feats: []string{"data-cardstack-drag"}},
	{fn: "initCardDecks", feats: []string{"data-cardstack-deck"}},
	{fn: "initSlideStacks", feats: []string{"data-cardstack-slide"}},
}

// blockSep 增强块之间的分隔（enhance.js 里统一用这排横线作注释前缀）。
const blockSep = "// ----------"

// scriptTagRe script 标签（含内容）；特征扫描前剥离，避免内联脚本自我误报。
var scriptTagRe = regexp.MustCompile("(?s)<script[^>]*>.*?</script>")

// enhanceScriptFor 按产物 HTML 拼装需要的增强块。
//
// 八个增强的触发特征全部登记在 enhanceBlocks 里，所以「一个都没命中」= 页面确实不需要
// 任何增强（纯内容页），此时只注入框架骨架 + 空的 onReady 调用。
// 新增增强时必须同时登记特征 —— 漏登记会让用到它的页面静默失去交互。
func enhanceScriptFor(html string) string {
	probe := scriptTagRe.ReplaceAllString(html, "")

	want := make(map[string]bool, len(enhanceBlocks))
	for _, b := range enhanceBlocks {
		for _, f := range b.feats {
			if strings.Contains(probe, f) {
				want[b.fn] = true
				break
			}
		}
	}
	return assembleEnhance(want)
}

// assembleEnhance 保留头部、命中的块、以及只引用命中函数的 onReady 调用段。
func assembleEnhance(want map[string]bool) string {
	head, blocks, tail := splitEnhance(enhanceScript)

	var sb strings.Builder
	sb.WriteString(head)
	fns := make([]string, 0, len(enhanceBlocks))
	for i, b := range enhanceBlocks {
		if !want[b.fn] {
			continue
		}
		fns = append(fns, b.fn)
		if i < len(blocks) {
			sb.WriteString(blocks[i])
		}
	}
	sb.WriteString(joinOnReady(tail, fns))
	return sb.String()
}

// splitEnhance 把 enhance.js 拆成「头部 / 各增强块 / 尾部（含 onReady 调用）」。
// 以 blockSep 注释行切分；块数与 enhanceBlocks 一一对应（顺序一致）。
func splitEnhance(src string) (head string, blocks []string, tail string) {
	nl := "\n"
	lines := strings.Split(src, nl)
	cut := make([]int, 0, len(enhanceBlocks)+1)
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), blockSep) {
			cut = append(cut, i)
		}
	}
	if len(cut) == 0 {
		return src, nil, ""
	}
	head = strings.Join(lines[:cut[0]], nl) + nl
	for i := 0; i < len(cut); i++ {
		end := len(lines)
		if i+1 < len(cut) {
			end = cut[i+1]
		}
		blocks = append(blocks, strings.Join(lines[cut[i]:end], nl))
	}
	// 最后一块里含 onReady 调用段：从它开始切给 tail。
	last := blocks[len(blocks)-1]
	if idx := strings.Index(last, "onReady(function () {"); idx >= 0 {
		tail = last[idx:]
		blocks[len(blocks)-1] = last[:idx]
	}
	return head, blocks, tail
}

// joinOnReady 把 onReady 的调用列表替换为只含命中函数的版本（保留原有包装与注释）。
func joinOnReady(tail string, fns []string) string {
	start := strings.Index(tail, "[")
	if start < 0 {
		return tail
	}
	end := strings.Index(tail[start:], "].forEach")
	if end < 0 {
		return tail
	}
	end += start
	var list strings.Builder
	list.WriteString("[")
	for i, fn := range fns {
		if i > 0 {
			list.WriteString(",")
		}
		list.WriteString(fn)
	}
	list.WriteString("]")
	return tail[:start] + list.String() + tail[end+1:]
}
