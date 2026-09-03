// Package video — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / iframe↔video 双形态判定
// 保留在 Go，HTML 拼装交给 video.jet 模板。Render 方法保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package video

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出视频样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View video 渲染视图数据（供 video.jet 模板使用）。
type View struct {
	// IsEmbed 是否外链嵌入（iframe）。
	IsEmbed bool
	// EmbedSrc iframe 嵌入地址（模板输出时由 Jet 默认转义）。
	EmbedSrc string
	// Poster 封面图 URL（本地 video，模板输出时由 Jet 默认转义）。
	Poster string
	// Autoplay / Loop / Muted / Controls 播放控制布尔属性。
	Autoplay bool
	Loop     bool
	Muted    bool // 已预计算 = p.Muted || p.Autoplay
	Controls bool
	// Preload 预加载策略（默认 metadata）。
	Preload string
	// Src 本地视频地址（模板输出时由 Jet 默认转义）。
	Src string
}

// BuildView 生成视频渲染视图：外链嵌入 vs 本地 video 双形态判定。
func BuildView(p *Props) View {
	v := View{
		Poster:   p.Poster,
		Autoplay: p.Autoplay,
		Loop:     p.Loop,
		Muted:    p.Muted || p.Autoplay,
		Controls: p.Controls,
	}

	videoURL := p.URL
	if embedSrc, ok := embedURL(videoURL); ok {
		v.IsEmbed = true
		v.EmbedSrc = embedSrc
		return v
	}

	preload := p.Preload
	if preload == "" {
		preload = "metadata"
	}
	v.Preload = preload
	v.Src = videoURL
	return v
}
