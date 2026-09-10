package shapedivider

import "go_wp/internal/builder/core"

// 形状 path 生成已提升至 core 通用素材库（单一定义，core/shapes.go）：
// 本包仅保留组件内取形入口的薄委托，形状词汇表经 core.ShapeKind* 常量对齐。
// container 内置装饰（viewBox 1440×64，字节冻结的历史行为）不经过素材库。
// 组件统一 viewBox 为 1440×120（core.ShapeVBW / core.ShapeVBH）。

// shapePath 组件内取形入口（转发 core 通用素材库）。
func shapePath(shape string, variant int, driftPad bool) string {
	return core.ShapePath(shape, variant, driftPad)
}
