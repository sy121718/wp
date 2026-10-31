package core

type ImageLoadingAware interface {
	ApplyImageLoading(d ImageDefaults) bool
}

type ImageLoadingAttrs struct {
	IsEager bool

	Skeleton bool
}

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

type ImageFetchPriority struct {
	High bool
	Low  bool
}

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

func ImageSkeletonClass(class string, skeleton bool) string {
	if !skeleton {
		return class
	}
	if class == "" {
		return "is-skeleton"
	}
	return class + " is-skeleton"
}

func AddImageSkeletonCSS(b *CSSBuckets) {
	if b == nil {
		return
	}
	b.Add(BreakpointDesktop, "img.is-skeleton", []string{
		"background-image: linear-gradient(90deg, var(--sky-skeleton-a, #eef1f4) 25%, var(--sky-skeleton-b, #e2e6ea) 37%, var(--sky-skeleton-a, #eef1f4) 63%)",
		"background-size: 400% 100%",
		"animation: sky-skeleton-shimmer 1.4s ease infinite",
	})

	b.AddKeyframesDecls("sky-skeleton-shimmer", []string{
		"0% { background-position: 100% 50% }",
		"100% { background-position: 0 50% }",
	})
}
