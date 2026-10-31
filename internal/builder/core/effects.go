package core

const (
	SurfaceGlass    = "glass"
	SurfaceLiquid   = "liquid"
	SurfaceNeumorph = "neumorph"
)

var allowedSurface = map[string]bool{"": true, SurfaceGlass: true, SurfaceLiquid: true, SurfaceNeumorph: true}

func CompileSurface(sel string, surface string, b *CSSBuckets) {
	switch surface {
	case SurfaceGlass:
		b.Add(BreakpointDesktop, sel, []string{
			"backdrop-filter: blur(12px)",
			"background: var(--sky-glass-bg, rgba(255,255,255,.55))",
			"border: 1px solid var(--sky-glass-border, rgba(255,255,255,.45))",
		})
	case SurfaceLiquid:
		b.Add(BreakpointDesktop, sel, []string{
			"backdrop-filter: blur(16px) saturate(1.6)",
			"background: var(--sky-glass-bg, rgba(255,255,255,.45))",
			"border: 1px solid var(--sky-glass-border, rgba(255,255,255,.55))",
			"box-shadow: inset 0 1px 1px rgba(255,255,255,.65), inset 0 -1px 2px rgba(0,0,0,.06), 0 8px 32px rgba(0,0,0,.12)",
		})
	case SurfaceNeumorph:

		b.Add(BreakpointDesktop, sel, []string{
			"background: var(--sky-neu-bg, #e9edf2)",
			"box-shadow: 8px 8px 16px var(--sky-neu-dark, rgba(163,177,198,.6)), -8px -8px 16px var(--sky-neu-light, rgba(255,255,255,.9))",
			"border: 1px solid transparent",
		})
	}

	if surface == SurfaceNeumorph {
		b.Add(BreakpointMobile, sel, []string{
			"box-shadow: 4px 4px 8px var(--sky-neu-dark, rgba(163,177,198,.6)), -4px -4px 8px var(--sky-neu-light, rgba(255,255,255,.9))",
		})
	}

	if surface == SurfaceLiquid {
		b.Add(BreakpointMobile, sel, []string{"backdrop-filter: blur(6px) saturate(1.3)"})
	} else if surface == SurfaceGlass {
		b.Add(BreakpointMobile, sel, []string{"backdrop-filter: blur(8px)"})
	}
}

func FocusRingDecls() []string {
	return []string{
		"border-color: var(--sky-c-primary, #2563eb)",
		"box-shadow: 0 0 0 3px var(--sky-focus-ring, rgba(37,99,235,.15))",
		"outline: none",
	}
}

func FocusTransitionDecl() string {
	return "transition: border-color .2s ease, box-shadow .2s ease"
}

func BackgroundFlowDecls() []string {
	return []string{
		"background-size: 200% 200%",
		"animation: sky-bg-flow 8s ease infinite",
	}
}

const BorderFlowAngleProperty = "@property --sky-flow-angle {\n  syntax: \"<angle>\"\n  initial-value: 0deg\n  inherits: false\n}"

func TextGradientDecls(gradient string) []string {
	return []string{
		"background-image: " + gradient,
		"-webkit-background-clip: text",
		"background-clip: text",
		"color: transparent",
		"-webkit-text-fill-color: transparent",
	}
}

func BackgroundPatternDecls(kind, color string) []string {
	c := color
	if c == "" || !IsSafeCSSValue(c) {
		c = "rgba(0,0,0,.08)"
	}
	switch kind {
	case "grid":
		return []string{
			"background-image: linear-gradient(" + c + " 1px, transparent 1px), linear-gradient(90deg, " + c + " 1px, transparent 1px)",
			"background-size: 24px 24px",
		}
	case "overlay":
		return []string{
			"background-image: linear-gradient(" + c + " 1px, transparent 1px), linear-gradient(90deg, " + c + " 1px, transparent 1px)",
			"background-size: 8px 8px",
		}
	case "diagonal-stripes":
		return []string{
			"background-image: repeating-linear-gradient(45deg, " + c + " 0 2px, transparent 2px 10px)",
		}
	case "diagonal-lines":
		return []string{
			"background-image: repeating-linear-gradient(-45deg, " + c + " 0 1px, transparent 1px 12px)",
		}
	case "vertical-stripes":
		return []string{
			"background-image: repeating-linear-gradient(90deg, " + c + " 0 2px, transparent 2px 12px)",
		}
	case "horizontal-stripes":
		return []string{
			"background-image: repeating-linear-gradient(0deg, " + c + " 0 2px, transparent 2px 12px)",
		}
	case "zigzag":
		return []string{
			"background-image: linear-gradient(135deg, " + c + " 25%, transparent 25%), linear-gradient(225deg, " + c + " 25%, transparent 25%)",
			"background-size: 20px 20px",
			"background-position: 0 0, 10px 0",
		}
	case "checkerboard":
		return []string{
			"background-image: conic-gradient(" + c + " 25%, transparent 0 50%, " + c + " 0 75%, transparent 0)",
			"background-size: 20px 20px",
		}
	case "triangles":
		return []string{
			"background-image: linear-gradient(45deg, " + c + " 25%, transparent 25%), linear-gradient(-45deg, " + c + " 25%, transparent 25%)",
			"background-size: 24px 24px",
		}
	case "diamond":
		return []string{
			"background-image: conic-gradient(from 45deg, " + c + " 25%, transparent 0 50%, " + c + " 0 75%, transparent 0)",
			"background-size: 24px 24px",
		}
	case "crosses":
		return []string{
			"background-image: radial-gradient(" + c + " 2px, transparent 2px), radial-gradient(" + c + " 2px, transparent 2px)",
			"background-size: 24px 24px",
			"background-position: 0 0, 12px 12px",
		}
	case "plus":
		return []string{
			"background-image: linear-gradient(" + c + " 2px, transparent 2px), linear-gradient(90deg, " + c + " 2px, transparent 2px)",
			"background-size: 24px 24px",
			"background-position: 0 11px, 11px 0",
		}
	case "squares":
		return []string{
			"background-image: linear-gradient(" + c + " 1px, transparent 1px), linear-gradient(90deg, " + c + " 1px, transparent 1px)",
			"background-size: 16px 16px",
			"background-position: center",
		}
	case "circles":
		return []string{
			"background-image: radial-gradient(" + c + " 3px, transparent 3px)",
			"background-size: 24px 24px",
		}
	case "polka":
		return []string{
			"background-image: radial-gradient(" + c + " 3px, transparent 3px), radial-gradient(" + c + " 3px, transparent 3px)",
			"background-size: 28px 28px",
			"background-position: 0 0, 14px 14px",
		}
	case "ripple":
		return []string{
			"background-image: repeating-radial-gradient(circle at 0 0, " + c + " 0 2px, transparent 2px 16px)",
		}
	case "bricks":
		return []string{
			"background-image: linear-gradient(" + c + " 1px, transparent 1px), linear-gradient(90deg, " + c + " 1px, transparent 1px)",
			"background-size: 24px 12px",
			"background-position: 0 0, 12px 0",
		}
	case "rain":
		return []string{
			"background-image: repeating-linear-gradient(70deg, " + c + " 0 1px, transparent 1px 9px)",
		}
	case "honeycomb":
		return []string{
			"background-image: conic-gradient(from 30deg, " + c + " 0 60deg, transparent 60deg 120deg, " + c + " 120deg 180deg, transparent 180deg 240deg, " + c + " 240deg 300deg, transparent 300deg)",
			"background-size: 20px 20px",
		}
	default:
		return []string{
			"background-image: radial-gradient(" + c + " 1px, transparent 1px)",
			"background-size: 16px 16px",
		}
	}
}

func TextStrokeDecls(width, color string) []string {
	return []string{
		"-webkit-text-stroke: " + width + " " + color,
		"paint-order: stroke fill",
	}
}

const (
	SpringStandardCurve = "linear(0, 0.0088 2.86%, 0.0347 5.83%, 0.1413 12.1%, 0.2981 18.6%, 0.4629 24.9%, 0.7415 35%, 0.8683 41.4%, 0.9727 49.5%, 1.0072 53.7%, 1.0209 59.7%, 1.0161 66%, 0.9885 75.4%, 0.9829 80.9%, 0.9854 86.9%, 0.9967 95.1%, 1)"

	SpringSoftCurve = "linear(0, 0.0183 3.18%, 0.0704 6.66%, 0.2247 13.7%, 0.4064 21.4%, 0.5811 29.8%, 0.7642 39.8%, 0.8794 48.9%, 0.9591 58.3%, 1.0015 66.1%, 1.0173 76.1%, 1.0126 84.3%, 1.0017 92.9%, 0.9995 97.1%, 1)"

	SpringBouncyCurve = "linear(0, 0.0272 4.03%, 0.1137 9.06%, 0.2601 14.5%, 0.4021 19.9%, 0.6259 28.8%, 0.7605 36.4%, 0.8379 44.4%, 0.9145 54.1%, 0.9569 62.9%, 0.9836 73.1%, 1.0039 84.4%, 1.0083 89.9%, 1.0011 97.4%, 1)"
)

const (
	EaseOutCirc = "cubic-bezier(0.08, 0.82, 0.17, 1)"

	EaseInOutCirc = "cubic-bezier(0.78, 0.14, 0.15, 0.86)"

	EaseOutBack = "cubic-bezier(0.12, 0.4, 0.29, 1.46)"

	EaseInOutQuint = "cubic-bezier(0.86, 0, 0.07, 1)"
)

func SafeAreaDecls(edge string) []string {

	switch edge {
	case "top", "bottom", "left", "right":
	default:
		edge = "bottom"
	}
	return []string{
		"padding-" + edge + ": env(safe-area-inset-" + edge + ", 0px)",
	}
}
