package cardstack

import "fmt"

// i18n.go — 卡片环 / 卡片堆叠的无障碍与控件文案取词（审计 I18N-010）。

// 文案 key（sys_i18n）：site.component.cardStack.<语义>。
const (
	TextKeyDragAria      = "site.component.cardStack.dragAria"
	TextKeyDeckAria      = "site.component.cardStack.deckAria"
	TextKeyZoomAria      = "site.component.cardStack.zoomAria"
	TextKeyPrev          = "site.component.cardStack.prev"
	TextKeyNext          = "site.component.cardStack.next"
	TextKeyCloseZoomAria = "site.component.cardStack.closeZoomAria"
	TextKeyClose         = "site.component.cardStack.close"
	// TextKeyZoomCardAria 单张卡片放大按钮的无障碍名，%s 是卡片序号。
	TextKeyZoomCardAria = "site.component.cardStack.zoomCardAria"
)

// 中文兜底（与抽 key 前的产物逐字一致）。
const (
	fallbackDragAria      = "可拖拽旋转的卡片环（左右方向键也可旋转）"
	fallbackDeckAria      = "可滑动切换的卡片堆叠（左右方向键也可切换）"
	fallbackZoomAria      = "放大这张卡片"
	fallbackPrev          = "上一页"
	fallbackNext          = "下一页"
	fallbackCloseZoomAria = "关闭放大的卡片"
	fallbackClose         = "关闭"
	// 占位符只允许 %s（pkg/i18n 约定）。
	fallbackZoomCardAria = "放大第 %s 张卡片"
)

// CardstackLabels 卡片环 / 堆叠的固定文案（审计 I18N-010）。
type CardstackLabels struct {
	DragAria      string
	DeckAria      string
	ZoomAria      string
	Prev          string
	Next          string
	CloseZoomAria string
	Close         string
	// ZoomCardAria 单张卡片的放大按钮（%s = 卡片序号）。
	ZoomCardAria string
}

// defaultCardstackLabels 中文兜底值。
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

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
//
// 这些文案全部落在 aria-label 上 —— 访客看不见，读屏器会念。它们最容易漏接
// 多语言：截图上看不出问题，验收时也「页面上没有中文」，只有用读屏器或看产物源码
// 才会发现英文站点里念的是中文。
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

// refreshCardAriaLabels 用当前语言的模板重算每张卡片的放大按钮名。
//
// 卡片由 BuildView 构造、ApplyI18n 在其后运行，所以逐卡文案必须在这里重算 ——
// 只在 BuildView 里生成的话，换语言后卡片名仍是中文（容器已经翻译了，卡还是中文）。
// 卡片 Label 为空时跳过：空序号拼出来的 "放大第  张卡片" 是伪文案。
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
