package adminui

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/view"
)

func PageSlot(tpl string) gin.HandlerFunc {
	return view.Slot(tpl, RenderPage)
}

func BareSlot(tpl string) gin.HandlerFunc {
	return view.Slot(tpl, RenderBare)
}
