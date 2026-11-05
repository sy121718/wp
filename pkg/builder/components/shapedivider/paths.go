package shapedivider

import "go_wp/pkg/builder/core"

func shapePath(shape string, variant int, driftPad bool) string {
	return core.ShapePath(shape, variant, driftPad)
}
