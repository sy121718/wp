package builder

import (
	"strings"

	"go_wp/pkg/builder/core"
	"go_wp/pkg/logger"
)

type enhanceBlock struct {
	fns []string

	feats []string

	source string
}

func allEnhanceBlocks() []enhanceBlock {
	owned := core.OwnedEnhanceBlocks()
	out := make([]enhanceBlock, 0, len(owned))
	for _, ob := range owned {
		out = append(out, enhanceBlock{fns: ob.Fns, feats: ob.Feats, source: ob.Source})
	}
	return out
}

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

func assembleEnhance(hit []bool, blocks []enhanceBlock, src string) string {
	head, tail := splitEnhance(src)

	var sb strings.Builder
	sb.WriteString(head)
	fns := make([]string, 0, len(blocks))
	for i, b := range blocks {
		if !hit[i] {
			continue
		}

		sb.WriteString(b.source)
		fns = append(fns, b.fns...)
	}
	sb.WriteString(joinOnReady(tail, fns))
	return sb.String()
}

func splitEnhance(src string) (head, tail string) {
	idx := strings.Index(src, "onReady(function () {")
	if idx < 0 {
		return src, ""
	}

	lineStart := strings.LastIndex(src[:idx], "\n") + 1
	return src[:lineStart], src[lineStart:]
}

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
