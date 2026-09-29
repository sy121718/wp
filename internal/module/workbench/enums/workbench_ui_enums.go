package workbenchenums

// workbench_ui_enums.go — 工作台「Go 侧拼 HTML / 结构化面板」的 UI 文案 key（迁移 452 seed 中英词条）。
//
// 为什么这些 key 不用模板取词：结构树（outline_handle.go）与检查器（inspector_*.go）的
// HTML 是 Go 拼串产出的（模板只做一个 `|unsafe` 插槽），字段的 Label / Placeholder /
// Options 也在 Go 侧按组件 schema 分派 —— 模板层看不到这些句子，所以 key 与中文兜底
// 都登记在这里，取词统一走 shell.TranslateFor(c)。
//
// 与 workbench/err 那批的分工：err.* 是**失败**文案（帮作者判断下一步），这一批是
// 画布控件上的**固定标签**。命名沿用「模块前缀 + 面名 + 语义」，与库内既有的
// workbench.nav.*（导航菜单项）一致。

// —— 结构树（outline_handle.go 拼 HTML）——

// OutlineToggle 节点折叠按钮的提示。
const OutlineToggle = "workbench.outline.toggle"

// OutlineHiddenHint 隐藏徽标的悬停说明。
const OutlineHiddenHint = "workbench.outline.hiddenHint"

// OutlineHiddenBadge 隐藏徽标上的字。
const OutlineHiddenBadge = "workbench.outline.hiddenBadge"

// OutlineLockedHint 锁定徽标的悬停说明。
const OutlineLockedHint = "workbench.outline.lockedHint"

// OutlineLockedBadge 锁定徽标上的字。
const OutlineLockedBadge = "workbench.outline.lockedBadge"

// OutlineOpUp 节点操作「上移」的悬停说明。
const OutlineOpUp = "workbench.outline.op.up"

// OutlineOpDown 节点操作「下移」的悬停说明。
const OutlineOpDown = "workbench.outline.op.down"

// OutlineOpDup 节点操作「复制」的悬停说明。
const OutlineOpDup = "workbench.outline.op.dup"

// OutlineOpDel 节点操作「删除」的悬停说明。
const OutlineOpDel = "workbench.outline.op.del"

// —— 检查器分组标题（inspector_handle.go 的 inspectorSectionOrder）——

// InspectorSectionContent 分组「内容」。
const InspectorSectionContent = "workbench.inspector.section.content"

// InspectorSectionStyle 分组「基础」。
const InspectorSectionStyle = "workbench.inspector.section.style"

// InspectorSectionLayout 分组「布局」。
const InspectorSectionLayout = "workbench.inspector.section.layout"

// InspectorSectionBackground 分组「背景」。
const InspectorSectionBackground = "workbench.inspector.section.background"

// InspectorSectionBorder 分组「边框」。
const InspectorSectionBorder = "workbench.inspector.section.border"

// InspectorSectionTransform 分组「变换」。
const InspectorSectionTransform = "workbench.inspector.section.transform"

// InspectorSectionMotion 分组「动效」。
const InspectorSectionMotion = "workbench.inspector.section.motion"

// InspectorSectionHover 分组「悬停」。
const InspectorSectionHover = "workbench.inspector.section.hover"

// InspectorSectionResponsive 分组「响应式」。
const InspectorSectionResponsive = "workbench.inspector.section.responsive"

// InspectorSectionAdvanced 分组「高级」。
const InspectorSectionAdvanced = "workbench.inspector.section.advanced"

// —— 检查器字段标签与占位符（inspector_field_handle.go / inspector_navigation.go）——

// InspectorCorners 四角圆角控件的默认标签。
const InspectorCorners = "workbench.inspector.corners"

// InspectorCornerTopLeft 圆角子输入「左上」。
const InspectorCornerTopLeft = "workbench.inspector.corner.topLeft"

// InspectorCornerTopRight 圆角子输入「右上」。
const InspectorCornerTopRight = "workbench.inspector.corner.topRight"

// InspectorCornerBottomRight 圆角子输入「右下」。
const InspectorCornerBottomRight = "workbench.inspector.corner.bottomRight"

// InspectorCornerBottomLeft 圆角子输入「左下」。
const InspectorCornerBottomLeft = "workbench.inspector.corner.bottomLeft"

// InspectorBpDesktop 响应式断点「桌面」。
const InspectorBpDesktop = "workbench.inspector.bp.desktop"

// InspectorBpTablet 响应式断点「平板」。
const InspectorBpTablet = "workbench.inspector.bp.tablet"

// InspectorBpMobile 响应式断点「手机」。
const InspectorBpMobile = "workbench.inspector.bp.mobile"

// InspectorDirTop 边距方向「上」。
const InspectorDirTop = "workbench.inspector.dir.top"

// InspectorDirRight 边距方向「右」。
const InspectorDirRight = "workbench.inspector.dir.right"

// InspectorDirBottom 边距方向「下」。
const InspectorDirBottom = "workbench.inspector.dir.bottom"

// InspectorDirLeft 边距方向「左」。
const InspectorDirLeft = "workbench.inspector.dir.left"

// InspectorPhClasses classes 控件的占位符。
const InspectorPhClasses = "workbench.inspector.ph.classes"

// InspectorPhCSSDecls cssdecls 控件的占位符。
const InspectorPhCSSDecls = "workbench.inspector.ph.cssDecls"

// InspectorPhDimension dimension 控件的占位符。
const InspectorPhDimension = "workbench.inspector.ph.dimension"

// —— 导航选择器（inspector_navigation.go）——

// InspectorNavAny 下拉的「（不限）」空选项。
const InspectorNavAny = "workbench.inspector.nav.any"

// InspectorNavKindHeader 导航位置「页眉」。
const InspectorNavKindHeader = "workbench.inspector.nav.kind.header"

// InspectorNavKindHeaderMobile 导航位置「页眉（移动端）」。
const InspectorNavKindHeaderMobile = "workbench.inspector.nav.kind.headerMobile"

// InspectorNavKindFooter 导航位置「页脚」。
const InspectorNavKindFooter = "workbench.inspector.nav.kind.footer"

// InspectorNavKindFooterMobile 导航位置「页脚（移动端）」。
const InspectorNavKindFooterMobile = "workbench.inspector.nav.kind.footerMobile"

// —— 重复项面板（inspector_repeater.go 拼 HTML）——

// InspectorRepeaterMoveUp 重复项上移按钮的提示。
const InspectorRepeaterMoveUp = "workbench.inspector.repeater.moveUp"

// InspectorRepeaterMoveDown 重复项下移按钮的提示。
const InspectorRepeaterMoveDown = "workbench.inspector.repeater.moveDown"

// InspectorRepeaterRemove 重复项删除按钮的提示（占位符 {noun} = 项名）。
const InspectorRepeaterRemove = "workbench.inspector.repeater.remove"

// InspectorRepeaterMatched 数量一致时的说明（占位符 {noun} / {count}）。
const InspectorRepeaterMatched = "workbench.inspector.repeater.matched"

// InspectorRepeaterMismatch 数量不一致时的说明（占位符 {noun} / {rows} / {panels}）。
const InspectorRepeaterMismatch = "workbench.inspector.repeater.mismatch"

// —— 画布桥接脚本（editor_bridge.go 注入 iframe 的 JS 文案）——
//
// 这批串拼进 <script> 后由 iframe 内的 JS 输出（浮标按钮文字、右键菜单项、
// 快捷条 title），与模板取词同性质 —— 都是给人看的文案，所以走同一套词条。
// 脚本以占位符 {{bridge.*}} 书写，注入时按请求语言替换（见 editorBridgeTexts）。

// BridgeInsert 「+ 插入组件」浮标的文字（"+" 是符号，留在脚本里）。
const BridgeInsert = "workbench.bridge.insert"

// BridgeEditText 右键菜单与快捷条的「编辑文本」（点它即进入就地编辑）。
const BridgeEditText = "workbench.bridge.editText"

// BridgeCopy 右键菜单与快捷条的「复制」。
const BridgeCopy = "workbench.bridge.copy"

// BridgeCut 右键菜单的「剪切」。
const BridgeCut = "workbench.bridge.cut"

// BridgePasteInside 右键菜单的「粘贴到内部」。
const BridgePasteInside = "workbench.bridge.pasteInside"

// BridgeMoveUp 右键菜单的「上移」。
const BridgeMoveUp = "workbench.bridge.moveUp"

// BridgeMoveDown 右键菜单的「下移」。
const BridgeMoveDown = "workbench.bridge.moveDown"

// BridgeDelete 右键菜单与快捷条的「删除」。
const BridgeDelete = "workbench.bridge.delete"

// BridgeEntranceGroup 右键菜单里「入场动画」分组标题。
const BridgeEntranceGroup = "workbench.bridge.entranceGroup"

// BridgeHoverLift 右键菜单的「悬浮上浮」。
const BridgeHoverLift = "workbench.bridge.hoverLift"
