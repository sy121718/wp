package core

import "strconv"

const (
	ShapeVBW      = 1440
	ShapeVBH      = 120
	ShapeWaveHalf = 360
)

const (
	ShapeKindWave     = "wave"
	ShapeKindCurve    = "curve"
	ShapeKindSlope    = "slope"
	ShapeKindTilt     = "tilt"
	ShapeKindTriangle = "triangle"
	ShapeKindRound    = "round"
)

func ShapePath(shape string, variant int, driftPad bool) string {
	switch shape {
	case ShapeKindWave:
		return shapeWavePath(variant, driftPad)
	case ShapeKindCurve:
		switch variant {
		case 1:
			return "M0 120C280 44 1160 44 1440 120H0z"
		case 2:
			return "M0 120C220 78 1220 78 1440 120H0z"
		default:
			return "M0 120C360 0 1080 0 1440 120H0z"
		}
	case ShapeKindSlope:
		return "M0 120L1440 0v120H0z"
	case ShapeKindTilt:
		return "M0 120C360 92 1080 28 1440 0v120H0z"
	case ShapeKindTriangle:
		return "M0 120L720 0l720 120H0z"
	case ShapeKindRound:
		return "M0 120A720 120 0 0 1 1440 120H0z"
	default:

		return shapeWavePath(0, driftPad)
	}
}

func shapeWavePath(variant int, driftPad bool) string {
	var dy, y0 int
	switch variant {
	case 1:
		dy, y0 = 60, 30
	case 2:
		dy, y0 = 36, 42
	default:
		dy, y0 = 88, 16
	}
	segments := 4
	start := 0
	if driftPad {

		start = -2 * ShapeWaveHalf
		segments = 6
	}
	d := "M" + strconv.Itoa(start) + " " + strconv.Itoa(y0) +
		" c130 0 230 " + strconv.Itoa(dy) + " " + strconv.Itoa(ShapeWaveHalf) + " " + strconv.Itoa(dy)
	alt := -dy
	for i := 1; i < segments; i++ {
		d += " s240 " + strconv.Itoa(alt) + " " + strconv.Itoa(ShapeWaveHalf) + " " + strconv.Itoa(alt)
		alt = -alt
	}
	return d + "V" + strconv.Itoa(ShapeVBH) + "H0z"
}
