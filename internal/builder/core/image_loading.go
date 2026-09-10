package core

// image_loading.go — 图片懒加载三态与骨架屏的共享解析（所有输出 <img> 的组件统一走这里）。
//
// 三态语义（组件 Props 的 loading 字段）：
//
//	空        → 继承主题「图片管理」默认（ThemeSettings.Images.LazyLoad）
//	on / lazy → 强制开启懒加载
//	off/eager → 强制立即加载（lazy/eager 为历史值，保留兼容旧文档）
//
// 优先级：组件设置 → 主题默认 → 内置默认（开启）。
// 骨架屏只在「本图懒加载 且 主题开启骨架」时输出，且为纯 CSS（产物零脚本约束）。

// ImageLoadingAware 由输出 <img> 的组件视图实现：接收主题「图片管理」默认后解析自身三态。
// 返回 true 表示需要骨架屏 CSS（调用方负责 AddImageSkeletonCSS，多图共享一份）。
type ImageLoadingAware interface {
	ApplyImageLoading(d ImageDefaults) bool
}

// ImageLoadingAttrs 图片加载策略解析结果（组件视图共用）。
type ImageLoadingAttrs struct {
	// IsEager 立即加载（否则懒加载）。
	IsEager bool
	// Skeleton 懒加载且主题开启骨架屏。
	Skeleton bool
}

// ResolveImageLoading 把组件级三态解析为最终加载策略。
func ResolveImageLoading(loading string, d ImageDefaults) ImageLoadingAttrs {
	lazy := d.LazyLoad
	switch loading {
	case "on", "lazy":
		lazy = true
	case "off", "eager":
		lazy = false
	}
	return ImageLoadingAttrs{IsEager: !lazy, Skeleton: lazy && d.Skeleton}
}

// ImageFetchPriority 解析后的资源提示（High/Low 互斥；均为 false 时不输出 fetchpriority 属性）。
type ImageFetchPriority struct {
	High bool
	Low  bool
}

// ResolveFetchPriority 规范化 fetchpriority 取值：仅 high / low 输出属性，
// 其余（空 / auto / 非法值）按浏览器默认处理，不输出属性。
func ResolveFetchPriority(v string) ImageFetchPriority {
	switch v {
	case "high":
		return ImageFetchPriority{High: true}
	case "low":
		return ImageFetchPriority{Low: true}
	default:
		return ImageFetchPriority{}
	}
}

// ImageSkeletonClass 把骨架类并入既有 class 串（空 class 时只留骨架类）。
func ImageSkeletonClass(class string, skeleton bool) string {
	if !skeleton {
		return class
	}
	if class == "" {
		return "is-skeleton"
	}
	return class + " is-skeleton"
}

// AddImageSkeletonCSS 输出懒加载骨架屏样式。
// CSSBuckets.Add 按规则去重，因此多张图重复调用不会产生重复 CSS。
func AddImageSkeletonCSS(b *CSSBuckets) {
	if b == nil {
		return
	}
	b.Add(BreakpointDesktop, "img.is-skeleton", []string{
		"background-image: linear-gradient(90deg, var(--sky-skeleton-a, #eef1f4) 25%, var(--sky-skeleton-b, #e2e6ea) 37%, var(--sky-skeleton-a, #eef1f4) 63%)",
		"background-size: 400% 100%",
		"animation: sky-skeleton-shimmer 1.4s ease infinite",
	})
	b.AddKeyframes("sky-skeleton-shimmer",
		"0% { background-position: 100% 50% } 100% { background-position: 0 50% }")
}
