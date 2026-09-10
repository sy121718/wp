package core

// keyframes_animate.go — Animate.css v4（MIT License, https://animate.style）效果拆解配方。
//
// 版权与致谢：帧结构与缓动曲线源自 Animate.css（Daniel Eden 及社区贡献者，MIT）。
// 按本项目规范重写：词汇挂 sky- 前缀、位移改自身尺度百分比（H5 适配：原库固定
// 像素大位移在移动端会飞出屏幕；退回机制见下方各 back-in-* 注释）、循环帧适配
// CompileInteraction 的 2.4s 无限循环节奏。
//
// 拆解决策（不搬的部分）：
//   - Exits 退出系（约 30 个）：零 JS 静态站无「消失时机」，无消费方，不搬；
//     未来模态/抽屉组件立项时随状态机补充。
//   - *Big 大位移系（2000px）：H5 会飞出视口，与移动端优先矛盾，不搬；
//     方向入场由标准系（自身尺度）覆盖。
//   - fadeIn/slideIn 基础系：与既有 sky-fade-in / sky-slide-* 语义重合，不重复。
//
// 合并进通用表：css.go keyframesCatalog（joinKeyframes 拼接，重名 fail-fast）；
// 本切片顺序即输出顺序（无需独立顺序表）。

var keyframesAnimate = []Keyframe{
	// —— 循环（attention seekers，挂 LoopEffect）——
	{Name: "sky-loop-flash", CSS: `@keyframes sky-loop-flash {
  0%, 10%, 20%, 100% { opacity: 1 }
  25%, 50% { opacity: 0 }
}}`},
	{Name: "sky-loop-rubber-band", CSS: `@keyframes sky-loop-rubber-band {
  from { transform: scale(1, 1) }
  30% { transform: scale(1.25, 0.75) }
  40% { transform: scale(0.75, 1.25) }
  50% { transform: scale(1.15, 0.85) }
  65% { transform: scale(0.95, 1.05) }
  75% { transform: scale(1.05, 0.95) }
  to { transform: scale(1, 1) }
}}`},
	{Name: "sky-loop-swing", CSS: `@keyframes sky-loop-swing {
  0%, 100% { transform-origin: top center; transform: rotate(0) }
  20% { transform-origin: top center; transform: rotate3d(0, 0, 1, 15deg) }
  40% { transform-origin: top center; transform: rotate3d(0, 0, 1, -10deg) }
  60% { transform-origin: top center; transform: rotate3d(0, 0, 1, 5deg) }
  80% { transform-origin: top center; transform: rotate3d(0, 0, 1, -5deg) }
}}`},
	{Name: "sky-loop-tada", CSS: `@keyframes sky-loop-tada {
  from { transform: scale(1) rotate(0) }
  10%, 20% { transform: scale(0.9) rotate(-3deg) }
  30%, 50%, 70%, 90% { transform: scale(1.1) rotate(3deg) }
  40%, 60%, 80% { transform: scale(1.1) rotate(-3deg) }
  to { transform: scale(1) rotate(0) }
}}`},
	{Name: "sky-loop-wobble", CSS: `@keyframes sky-loop-wobble {
  from { transform: translate3d(0, 0, 0) }
  15% { transform: translate3d(-25%, 0, 0) rotate3d(0, 0, 1, -5deg) }
  30% { transform: translate3d(20%, 0, 0) rotate3d(0, 0, 1, 3deg) }
  45% { transform: translate3d(-15%, 0, 0) rotate3d(0, 0, 1, -3deg) }
  60% { transform: translate3d(10%, 0, 0) rotate3d(0, 0, 1, 2deg) }
  75% { transform: translate3d(-5%, 0, 0) rotate3d(0, 0, 1, -1deg) }
  to { transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-loop-head-shake", CSS: `@keyframes sky-loop-head-shake {
  0% { transform: translateX(0) }
  6.5% { transform: translateX(-6px) rotateY(-9deg) }
  18.5% { transform: translateX(5px) rotateY(7deg) }
  31.5% { transform: translateX(-3px) rotateY(-5deg) }
  43.5% { transform: translateX(2px) rotateY(3deg) }
  50% { transform: translateX(0) }
}}`},
	{Name: "sky-loop-bounce", CSS: `@keyframes sky-loop-bounce {
  0%, 100% { transform: translateY(0); animation-timing-function: cubic-bezier(0.445, 0.05, 0.55, 0.95) }
  50% { transform: translateY(-12px); animation-timing-function: cubic-bezier(0.55, 0.085, 0.68, 0.53) }
}}`},
	// —— 入场（back 回弹系：位移用自身尺度百分比，H5 适配原库 1200px 固定值）——
	{Name: "sky-back-in-up", CSS: `@keyframes sky-back-in-up {
  from { transform: translate3d(0, 100%, 0) scale3d(0.7, 0.7, 0.7); opacity: 0; animation-timing-function: cubic-bezier(0.34, 1.56, 0.64, 1) }
  to { transform: translate3d(0, 0, 0) scale3d(1, 1, 1); opacity: 1 }
}}`},
	{Name: "sky-back-in-down", CSS: `@keyframes sky-back-in-down {
  from { transform: translate3d(0, -100%, 0) scale3d(0.7, 0.7, 0.7); opacity: 0; animation-timing-function: cubic-bezier(0.34, 1.56, 0.64, 1) }
  to { transform: translate3d(0, 0, 0) scale3d(1, 1, 1); opacity: 1 }
}}`},
	{Name: "sky-back-in-left", CSS: `@keyframes sky-back-in-left {
  from { transform: translate3d(100%, 0, 0) scale3d(0.7, 0.7, 0.7); opacity: 0; animation-timing-function: cubic-bezier(0.34, 1.56, 0.64, 1) }
  to { transform: translate3d(0, 0, 0) scale3d(1, 1, 1); opacity: 1 }
}}`},
	{Name: "sky-back-in-right", CSS: `@keyframes sky-back-in-right {
  from { transform: translate3d(-100%, 0, 0) scale3d(0.7, 0.7, 0.7); opacity: 0; animation-timing-function: cubic-bezier(0.34, 1.56, 0.64, 1) }
  to { transform: translate3d(0, 0, 0) scale3d(1, 1, 1); opacity: 1 }
}}`},
	// —— 入场（bounce 方向系）——
	{Name: "sky-bounce-in-down", CSS: `@keyframes sky-bounce-in-down {
  from, 60%, 75%, 90%, to { animation-timing-function: cubic-bezier(0.215, 0.61, 0.355, 1) }
  0% { opacity: 0; transform: translate3d(0, -3000px, 0) }
  60% { opacity: 1; transform: translate3d(0, 25px, 0) }
  75% { transform: translate3d(0, -10px, 0) }
  90% { transform: translate3d(0, 5px, 0) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-bounce-in-up", CSS: `@keyframes sky-bounce-in-up {
  from, 60%, 75%, 90%, to { animation-timing-function: cubic-bezier(0.215, 0.61, 0.355, 1) }
  0% { opacity: 0; transform: translate3d(0, 3000px, 0) }
  60% { opacity: 1; transform: translate3d(0, -20px, 0) }
  75% { transform: translate3d(0, 10px, 0) }
  90% { transform: translate3d(0, -5px, 0) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-bounce-in-left", CSS: `@keyframes sky-bounce-in-left {
  from, 60%, 75%, 90%, to { animation-timing-function: cubic-bezier(0.215, 0.61, 0.355, 1) }
  0% { opacity: 0; transform: translate3d(-3000px, 0, 0) }
  60% { opacity: 1; transform: translate3d(25px, 0, 0) }
  75% { transform: translate3d(-10px, 0, 0) }
  90% { transform: translate3d(5px, 0, 0) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-bounce-in-right", CSS: `@keyframes sky-bounce-in-right {
  from, 60%, 75%, 90%, to { animation-timing-function: cubic-bezier(0.215, 0.61, 0.355, 1) }
  0% { opacity: 0; transform: translate3d(3000px, 0, 0) }
  60% { opacity: 1; transform: translate3d(-25px, 0, 0) }
  75% { transform: translate3d(10px, 0, 0) }
  90% { transform: translate3d(-5px, 0, 0) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	// —— 入场（fade 角向系；位移 100% 自身尺度，H5 不超屏）——
	{Name: "sky-fade-in-top-left", CSS: `@keyframes sky-fade-in-top-left {
  from { opacity: 0; transform: translate3d(-100%, -100%, 0) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-fade-in-top-right", CSS: `@keyframes sky-fade-in-top-right {
  from { opacity: 0; transform: translate3d(100%, -100%, 0) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-fade-in-bottom-left", CSS: `@keyframes sky-fade-in-bottom-left {
  from { opacity: 0; transform: translate3d(-100%, 100%, 0) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-fade-in-bottom-right", CSS: `@keyframes sky-fade-in-bottom-right {
  from { opacity: 0; transform: translate3d(100%, 100%, 0) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	// —— 入场（特色：lightSpeed / roll / jack）——
	{Name: "sky-light-speed-in-left", CSS: `@keyframes sky-light-speed-in-left {
  from { transform: translate3d(100%, 0, 0) skewX(-30deg); opacity: 0 }
  60% { transform: skewX(20deg); opacity: 1 }
  80% { transform: skewX(-5deg) }
  to { transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-light-speed-in-right", CSS: `@keyframes sky-light-speed-in-right {
  from { transform: translate3d(-100%, 0, 0) skewX(30deg); opacity: 0 }
  60% { transform: skewX(-20deg); opacity: 1 }
  80% { transform: skewX(5deg) }
  to { transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-roll-in", CSS: `@keyframes sky-roll-in {
  from { opacity: 0; transform: translate3d(-100%, 0, 0) rotate3d(0, 0, 1, -120deg) }
  to { opacity: 1; transform: translate3d(0, 0, 0) }
}}`},
	{Name: "sky-jack-in-the-box", CSS: `@keyframes sky-jack-in-the-box {
  from { opacity: 0; transform: scale(0.1) rotate(30deg); transform-origin: center bottom; animation-timing-function: ease-in }
  50% { transform: scale(1.5) rotate(-10deg); opacity: 1; animation-timing-function: ease-out }
  70% { transform: scale(0.9) rotate(5deg); animation-timing-function: ease-out }
  to { transform: scale(1) rotate(0) }
}}`},
	// —— 入场（zoom 方向系）——
	{Name: "sky-zoom-in-down", CSS: `@keyframes sky-zoom-in-down {
  from { opacity: 0; transform: scale3d(0.1, 0.1, 0.1) translate3d(0, -3000px, 0); animation-timing-function: cubic-bezier(0.55, 0.055, 0.675, 0.19) }
  60% { opacity: 1; transform: scale3d(0.475, 0.475, 0.475) translate3d(0, 60px, 0); animation-timing-function: cubic-bezier(0.175, 0.885, 0.32, 1) }
}}`},
	{Name: "sky-zoom-in-up", CSS: `@keyframes sky-zoom-in-up {
  from { opacity: 0; transform: scale3d(0.1, 0.1, 0.1) translate3d(0, 3000px, 0); animation-timing-function: cubic-bezier(0.55, 0.055, 0.675, 0.19) }
  60% { opacity: 1; transform: scale3d(0.475, 0.475, 0.475) translate3d(0, -60px, 0); animation-timing-function: cubic-bezier(0.175, 0.885, 0.32, 1) }
}}`},
	{Name: "sky-zoom-in-left", CSS: `@keyframes sky-zoom-in-left {
  from { opacity: 0; transform: scale3d(0.1, 0.1, 0.1) translate3d(-3000px, 0, 0); animation-timing-function: cubic-bezier(0.55, 0.055, 0.675, 0.19) }
  60% { opacity: 1; transform: scale3d(0.475, 0.475, 0.475) translate3d(60px, 0, 0); animation-timing-function: cubic-bezier(0.175, 0.885, 0.32, 1) }
}}`},
	{Name: "sky-zoom-in-right", CSS: `@keyframes sky-zoom-in-right {
  from { opacity: 0; transform: scale3d(0.1, 0.1, 0.1) translate3d(3000px, 0, 0); animation-timing-function: cubic-bezier(0.55, 0.055, 0.675, 0.19) }
  60% { opacity: 1; transform: scale3d(0.475, 0.475, 0.475) translate3d(-60px, 0, 0); animation-timing-function: cubic-bezier(0.175, 0.885, 0.32, 1) }
}}`},
	// —— 入场（rotate 方向系）——
	{Name: "sky-rotate-in-down-left", CSS: `@keyframes sky-rotate-in-down-left {
  from { transform-origin: left bottom; transform: rotate3d(0, 0, 1, -45deg); opacity: 0 }
  to { transform-origin: left bottom; transform: translate3d(0, 0, 0) rotate3d(0, 0, 1, 0); opacity: 1 }
}}`},
	{Name: "sky-rotate-in-down-right", CSS: `@keyframes sky-rotate-in-down-right {
  from { transform-origin: right bottom; transform: rotate3d(0, 0, 1, 45deg); opacity: 0 }
  to { transform-origin: right bottom; transform: translate3d(0, 0, 0) rotate3d(0, 0, 1, 0); opacity: 1 }
}}`},
	{Name: "sky-rotate-in-up-left", CSS: `@keyframes sky-rotate-in-up-left {
  from { transform-origin: left top; transform: rotate3d(0, 0, 1, 45deg); opacity: 0 }
  to { transform-origin: left top; transform: translate3d(0, 0, 0) rotate3d(0, 0, 1, 0); opacity: 1 }
}}`},
	{Name: "sky-rotate-in-up-right", CSS: `@keyframes sky-rotate-in-up-right {
  from { transform-origin: right top; transform: rotate3d(0, 0, 1, -45deg); opacity: 0 }
  to { transform-origin: right top; transform: translate3d(0, 0, 0) rotate3d(0, 0, 1, 0); opacity: 1 }
}}`},
	// —— 入场（flip 完整 3D 版；既有 sky-flip-x/y 为 -12deg 微翻简版，并存）——
	{Name: "sky-flip-in-x", CSS: `@keyframes sky-flip-in-x {
  from { transform: perspective(400px) rotate3d(1, 0, 0, 90deg); opacity: 0; animation-timing-function: ease-in }
  40% { transform: perspective(400px) rotate3d(1, 0, 0, -20deg); animation-timing-function: ease-in }
  60% { transform: perspective(400px) rotate3d(1, 0, 0, 10deg); opacity: 1; animation-timing-function: ease-in }
  80% { transform: perspective(400px) rotate3d(1, 0, 0, -5deg) }
  to { transform: perspective(400px) }
}}`},
	{Name: "sky-flip-in-y", CSS: `@keyframes sky-flip-in-y {
  from { transform: perspective(400px) rotate3d(0, 1, 0, 90deg); opacity: 0; animation-timing-function: ease-in }
  40% { transform: perspective(400px) rotate3d(0, 1, 0, -20deg); animation-timing-function: ease-in }
  60% { transform: perspective(400px) rotate3d(0, 1, 0, 10deg); opacity: 1; animation-timing-function: ease-in }
  80% { transform: perspective(400px) rotate3d(0, 1, 0, -5deg) }
  to { transform: perspective(400px) }
}}`},
}
