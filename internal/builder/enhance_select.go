package builder

// enhance_select.go — 增强脚本的按需拼装。
//
// 背景：客户端增强此前集中在 538 行的 enhance.js 里（8 个互不相关的增强），而每个产物都全量内联 ——
// 一个只用了 slide 的页面要背其余 85% 的用不到代码，纯内容页也背着 22KB。
//
// 做法：构建期按产物里实际出现的 data-* 特征挑选要注入的增强块，只拼这一份。
// 特征扫描前必须剥掉 script 块 —— 增强脚本自身内联在页面里，含全部 data-* 字样，
// 不剥离会让每个特征都「被检测到」，等于没裁。
//
// 块有两个来源：
//  1. **组件自带**（组件目录 //go:embed enhance.js，经 core.RegisterEnhanceBlock 注册）—— 优先；
//  2. **存量**：enhance.js 里按 // ---------- 切出的剩余块（legacyEnhanceBlocks 与之一一对应）。
// 迁到组件目录的块要从 legacyEnhanceBlocks 里移除，否则两边都算「登记了一份」。

import (
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/pkg/logger"
)

// enhanceBlock 一个可独立注入的增强块（统一视图：组件自带与存量都归一到它）。
type enhanceBlock struct {
	// fn 块内的初始化函数名（拼装时据此保留 onReady 的调用项）。
	fn string
	// feats 触发注入的产物特征（任一命中即注入）。
	feats []string
	// source 组件自带的块源码；存量为空串（届时按 legacy 段落取）。
	source string
}

// legacyEnhanceBlocks 存量块清单（与 enhance.js 里按 // ---------- 切出的段落一一对应，顺序一致）。
//
// 迁走的块从这里删除 —— 它们改由 core 注册表提供。顺序即输出顺序。
var legacyEnhanceBlocks = []enhanceBlock{
	{fn: "initSliders", feats: []string{"data-slider"}},
	{fn: "initCarousels", feats: []string{"data-carousel"}},
	{fn: "initCountdowns", feats: []string{"data-countdown"}},
	{fn: "initLightboxes", feats: []string{"data-lightbox"}},
	{fn: "initCardStacks", feats: []string{"data-cardstack-drag"}},
	{fn: "initCardDecks", feats: []string{"data-cardstack-deck"}},
	{fn: "initSlideStacks", feats: []string{"data-cardstack-slide"}},
}

// allEnhanceBlocks 全部增强块：组件自带的（优先，注册顺序）+ 存量。
//
// 顺序即产物内联顺序。新增增强时必须同时登记特征 ——
// 漏登记会让用到它的页面静默失去交互（「轮播不能拖、灯箱点不开」）。
func allEnhanceBlocks() []enhanceBlock {
	owned := core.OwnedEnhanceBlocks()
	out := make([]enhanceBlock, 0, len(owned)+len(legacyEnhanceBlocks))
	for _, ob := range owned {
		out = append(out, enhanceBlock{fn: ob.Fn, feats: ob.Feats, source: ob.Source})
	}
	return append(out, legacyEnhanceBlocks...)
}

// blockSep 增强块之间的分隔（enhance.js 里统一用这排横线作注释前缀）。
const blockSep = "// ----------"

// scriptTagRe script 标签（含内容）；特征扫描前剥离，避免内联脚本自我误报。
var scriptTagRe = regexp.MustCompile("(?s)<script[^>]*>.*?</script>")

// enhanceScriptFor 按产物 HTML 拼装需要的增强块。
//
// 全部增强的触发特征都登记在 allEnhanceBlocks 里，所以「一个都没命中」= 页面确实不需要
// 任何增强（纯内容页），此时只注入框架骨架 + 空的 onReady 调用。
func enhanceScriptFor(html, src string) string {
	if strings.TrimSpace(src) == "" {
		// 未注入增强源码：产物不含交互脚本（页面照常渲染）。不静默——装配层漏注入
		// 会让「轮播不能拖、灯箱点不开」这类问题在产物里无声发生，所以这里留痕。
		logger.Scene("build").Warn("未注入客户端增强源码（WithEnhanceSource），产物将不含交互脚本")
		return ""
	}
	probe := scriptTagRe.ReplaceAllString(html, "")

	blocks := allEnhanceBlocks()
	want := make(map[string]bool, len(blocks))
	for _, b := range blocks {
		for _, f := range b.feats {
			if strings.Contains(probe, f) {
				want[b.fn] = true
				break
			}
		}
	}
	return assembleEnhance(want, src)
}

// assembleEnhance 保留头部、命中的块、以及只引用命中函数的 onReady 调用段。
func assembleEnhance(want map[string]bool, src string) string {
	head, legacyParagraphs, tail := splitEnhance(src)

	var sb strings.Builder
	sb.WriteString(head)
	blocks := allEnhanceBlocks()
	fns := make([]string, 0, len(blocks))
	legacyIdx := 0
	for _, b := range blocks {
		// 组件自带的块不占 enhance.js 的段落位。
		var paragraph string
		if b.source != "" {
			paragraph = b.source
		} else {
			if legacyIdx < len(legacyParagraphs) {
				paragraph = legacyParagraphs[legacyIdx]
			}
			legacyIdx++
		}
		if !want[b.fn] {
			continue
		}
		fns = append(fns, b.fn)
		sb.WriteString(paragraph)
	}
	sb.WriteString(joinOnReady(tail, fns))
	return sb.String()
}

// splitEnhance 把 enhance.js 拆成「头部 / 各增强段落 / 尾部（含 onReady 调用）」。
// 以 blockSep 注释行切分；段落数与 legacyEnhanceBlocks 一一对应（顺序一致）。
func splitEnhance(src string) (head string, blocks []string, tail string) {
	nl := "\n"
	lines := strings.Split(src, nl)
	cut := make([]int, 0, len(legacyEnhanceBlocks)+1)
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
