package builder

// enhance_select.go — 增强脚本的按需拼装。
//
// 背景：客户端增强此前集中在 538 行的 enhance.js 里（8 个互不相关的增强），而每个产物都全量内联 ——
// 一个只用了 slide 的页面要背其余 85% 的用不到代码，纯内容页也背着 22KB。
//
// 做法：与公共控件共用最终 HTML 的能力属性集合，精确挑选增强块。
// 文本、注释和脚本中的示例不触发增强；没有命中任何能力时不输出框架。
//
// 块现在**全部来自组件目录**（组件 //go:embed enhance.js，经 core.RegisterEnhanceBlock 注册）：
// counter / slider / gallery / countdown / cardstack。enhance.js 只剩框架（IIFE + onReady 包装）。
//
// 块的内联顺序 = 组件包的 init 顺序（即 builder.go 的 blank import 顺序）。
// 块之间彼此独立、onReady 逐个调用，所以顺序不影响行为与产物语义。

import (
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/pkg/logger"
)

// enhanceBlock 一个可独立注入的增强块（统一视图）。
type enhanceBlock struct {
	// fns 块内的初始化函数名（拼装时据此保留 onReady 的调用项）。
	fns []string
	// feats 触发注入的产物特征（任一命中即注入整块）。
	feats []string
	// source 块源码。
	source string
}

// allEnhanceBlocks 全部增强块（组件自带的，注册顺序即内联顺序）。
//
// 新增增强时必须同时登记特征 —— 漏登记会让用到它的页面静默失去交互
// （「轮播不能拖、灯箱点不开」），页面照常渲染，很难归因。
func allEnhanceBlocks() []enhanceBlock {
	owned := core.OwnedEnhanceBlocks()
	out := make([]enhanceBlock, 0, len(owned))
	for _, ob := range owned {
		out = append(out, enhanceBlock{fns: ob.Fns, feats: ob.Feats, source: ob.Source})
	}
	return out
}

// enhanceScriptFor 按 HTML 真实属性拼装需要的增强块。
// 纯内容页不输出框架或初始化入口；仅命中能力时才需要增强源码。
func enhanceScriptFor(attrs htmlFeatures, src string) string {
	blocks := allEnhanceBlocks()
	hit := make([]bool, len(blocks))
	anyHit := false
	for i, b := range blocks {
		for _, attr := range b.feats {
			if _, ok := attrs[attr]; ok {
				hit[i] = true
				anyHit = true
				break
			}
		}
	}
	if !anyHit {
		return ""
	}
	if strings.TrimSpace(src) == "" {
		logger.Scene("build").Warn("未注入客户端增强源码（WithEnhanceSource），产物将不含交互脚本")
		return ""
	}
	return assembleEnhance(hit, blocks, src)
}

// assembleEnhance 保留框架头、命中的块、以及只引用命中函数的 onReady 调用段。
func assembleEnhance(hit []bool, blocks []enhanceBlock, src string) string {
	head, tail := splitEnhance(src)

	var sb strings.Builder
	sb.WriteString(head)
	fns := make([]string, 0, len(blocks))
	for i, b := range blocks {
		if !hit[i] {
			continue
		}
		// 块整体注入；onReady 只调用命中块里的函数。
		sb.WriteString(b.source)
		fns = append(fns, b.fns...)
	}
	sb.WriteString(joinOnReady(tail, fns))
	return sb.String()
}

// splitEnhance 把 enhance.js 拆成「框架头」与「onReady 调用尾」。
//
// 尾段优先切：它在源文件末尾，块全部迁到组件目录后源里只剩框架，
// 若还按块分隔符切会切不出尾段（onReady 丢失 → 所有增强都不执行）。
func splitEnhance(src string) (head, tail string) {
	idx := strings.Index(src, "onReady(function () {")
	if idx < 0 {
		return src, ""
	}
	// 按**行**切，不能按 `src[:idx]` 切：那样 head 会以 onReady 前面的行内缩进（几个空格）结尾，
	// 块紧接其后写入就会整体多缩进一级；而无块命中时产物又会丢掉那几格缩进。
	// 行首切分让两端都与迁移前的字节一致。
	lineStart := strings.LastIndex(src[:idx], "\n") + 1
	return src[:lineStart], src[lineStart:]
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
