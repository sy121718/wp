package cardstack

import "fmt"

const (
	TextKeyDragAria      = "site.component.cardStack.dragAria"
	TextKeyDeckAria      = "site.component.cardStack.deckAria"
	TextKeyZoomAria      = "site.component.cardStack.zoomAria"
	TextKeyPrev          = "site.component.cardStack.prev"
	TextKeyNext          = "site.component.cardStack.next"
	TextKeyCloseZoomAria = "site.component.cardStack.closeZoomAria"
	TextKeyClose         = "site.component.cardStack.close"

	TextKeyZoomCardAria = "site.component.cardStack.zoomCardAria"
)

const (
	fallbackDragAria      = "可拖拽旋转的卡片环（左右方向键也可旋转）"
	fallbackDeckAria      = "可滑动切换的卡片堆叠（左右方向键也可切换）"
	fallbackZoomAria      = "放大这张卡片"
	fallbackPrev          = "上一页"
	fallbackNext          = "下一页"
	fallbackCloseZoomAria = "关闭放大的卡片"
	fallbackClose         = "关闭"

	fallbackZoomCardAria = "放大第 %s 张卡片"
)

type CardstackLabels struct {
	DragAria      string
	DeckAria      string
	ZoomAria      string
	Prev          string
	Next          string
	CloseZoomAria string
	Close         string

	ZoomCardAria string
}

func defaultCardstackLabels() CardstackLabels {
	return CardstackLabels{
		DragAria:      fallbackDragAria,
		DeckAria:      fallbackDeckAria,
		ZoomAria:      fallbackZoomAria,
		Prev:          fallbackPrev,
		Next:          fallbackNext,
		CloseZoomAria: fallbackCloseZoomAria,
		Close:         fallbackClose,
		ZoomCardAria:  fallbackZoomCardAria,
	}
}

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.Labels = defaultCardstackLabels()
		refreshCardAriaLabels(v)
		return
	}
	base := defaultCardstackLabels()
	v.Labels = CardstackLabels{
		DragAria:      text(TextKeyDragAria, base.DragAria),
		DeckAria:      text(TextKeyDeckAria, base.DeckAria),
		ZoomAria:      text(TextKeyZoomAria, base.ZoomAria),
		Prev:          text(TextKeyPrev, base.Prev),
		Next:          text(TextKeyNext, base.Next),
		CloseZoomAria: text(TextKeyCloseZoomAria, base.CloseZoomAria),
		Close:         text(TextKeyClose, base.Close),
		ZoomCardAria:  text(TextKeyZoomCardAria, base.ZoomCardAria),
	}
	refreshCardAriaLabels(v)
}

func refreshCardAriaLabels(v *View) {
	if v == nil || v.Labels.ZoomCardAria == "" {
		return
	}
	for i := range v.Cards {
		if v.Cards[i].Label == "" {
			continue
		}
		v.Cards[i].AriaLabel = fmt.Sprintf(v.Labels.ZoomCardAria, v.Cards[i].Label)
	}
}
