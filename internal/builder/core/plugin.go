package core

// 插件组件的编译内核契约（docs/06-plugin-system.md §5/§7）。
//
// 共享形状（PluginPropControl / CollectionBinding / PluginComponentSpec /
// PluginResolver）住在 internal/builder/source —— 零依赖的共享形状包。
// 插件契约包（internal/module/plugin/contract）与内核指向**同一份定义**，
// 契约包因此不必反向 import core（AGENTS.md 不变量 7：反向即成环
// core → 契约 → core，core 再也无法持有业务契约；它现在正持有 content / product
// 的受限数据源接口，见 core/render.go）。
//
// 这里只留别名：内核内部、内置组件与装配层沿用 core.Xxx 写法，类型却是同一份 ——
// 不存在两份形状需要同步。新增插件形状请加在 source 侧并在此补别名。
//
// 插件组件 = 数据驱动组件：manifest 声明 props schema + Jet 模板 + styles 规则，
// 不含 Go 代码。内核只消费中性的 PluginComponentSpec；manifest 解析、样式规则
// 编译（internal/builder/style）由装配层（plugin 模块/plugincomp）完成，
// 经 CompileStyles 闭包注入——core 零新增依赖，无循环 import。

import "go_wp/internal/builder/source"

// PluginPropControl 插件组件的单个检查器控件声明，见 source.PluginPropControl。
type PluginPropControl = source.PluginPropControl

// CollectionBinding 插件组件集合绑定，见 source.CollectionBinding。
type CollectionBinding = source.CollectionBinding

// PluginComponentSpec 编译内核消费的插件组件规格，见 source.PluginComponentSpec。
type PluginComponentSpec = source.PluginComponentSpec

// PluginResolver 插件组件解析契约，见 source.PluginResolver。
type PluginResolver = source.PluginResolver
