package core

// i18n.go — 构建期组件文案取词的共享契约（多语言 P4，docs/06-D §10）。
//
// 与 ImageLoadingAware（image_loading.go）同形：组件视图实现 ApplyI18n，
// 渲染层（builder/jetview.go）在 BuildView 之后统一回填文案字段。
// 组件包因此不需要依赖 pkg/i18n，也不需要感知语言从哪来。

// I18nAware 由「产物含访客可见固定文案」的组件视图实现：接收构建期取词函数后
// 按当前语言回填自身文案字段（aria-label / title / 按钮文字 / 单元标签等）。
//
// 取词函数签名 func(key, fallback string) string，由 builder 注入
// （默认 pkg/i18n.TranslateFunc(RenderContext.Lang)）；实现方必须遵守：
//   - 只填充「未由用户填写」的缺省文案（用户配置优先）；
//   - text 为 nil 时直接用包内中文兜底常量；
//   - 绝不写空串（空串会让 aria-label/title 退化为无效值）。
type I18nAware interface {
	ApplyI18n(text func(key, fallback string) string)
}
