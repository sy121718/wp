// theme_schema.go — Theme JSON 存储规范（审计 VIS-002，YG 拍板的边界）。
//
// 主题数据由两个正交分区组成，必须分离存储、分离读写：
//
//	"tokens"    设计令牌：colors / typography / button / surface / motion / images。
//	            展示层，换主题时**整体替换**；页面级覆盖走 settings.themeOverride，
//	            深合并键级生效（见 theme_merge.go）。
//	"structure" 结构绑定：headerBlockId / footerBlockId / slots（槽位名 → 全局块 ID）。
//	            结构层，独立读写、独立失效传播（ListThemesByBlockID 按 blockID 命中）。
//
// 落地位置与历史兼容：
//   - themes.settings（project 模块）：历史数据把两分区平铺在同一 JSONB 顶层
//     （colors 与 headerBlockId 并列）。本批不改其存储形状（project 模块在批外），
//     读取侧沿用既有双键查询；后续如需重排，按「tokens / structure 两个顶层键」
//     迁移，读取侧同时接受平铺与分区两种形状即可平滑过渡。
//   - 页面文档（page 模块）：快照已按分区分离 —— settings.theme 只存令牌快照、
//     settings.structure 只存结构绑定快照，两键互不包含对方的字段。
//
// 主题标识（themeId）不属于任何分区：它是只读元数据，由存储层（page service）
// 写 settings.theme 快照时注入，随快照进构建产物（:root 的 --sky-theme-id 变量，
// 导航栏等组件经该变量与当前主题保持一致）。接口入参出现的 themeId 一律忽略，
// 不接受调用方伪造。
//
// 兼容性死线：令牌与结构分离后，任何一侧的字段增删都不得要求另一侧跟着写快照；
// 读取侧对未知键只透传不丢弃（MergeThemeRawJSON 的保真语义）。
package builder
