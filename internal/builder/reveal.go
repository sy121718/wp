package builder

import (
	"go_wp/internal/builder/core"
)

func applyScrollReveal(inter *core.InteractionProps, defaultEntrance string) {
	if inter.Entrance != "" || inter.ScrollStory != "" || inter.ScrollReveal != "" {
		return
	}
	inter.Entrance = defaultEntrance
	inter.ScrollReveal = "reveal"
}

func revealActive(ctx *core.RenderContext) bool {
	return ctx != nil && ctx.RevealInherit == "on" && ctx.RevealDefaultEntrance != ""
}
