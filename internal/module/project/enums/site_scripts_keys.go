package projectenums

// site_scripts_keys.go — 站点自定义注入代码（PIPE-8）的**就地提示**文案 key。
//
// 为什么与 project_enums.go 分开、且单开一个文件：这两条文案**不进 ?err= 通道**。
// ?err= 的候选集（project_err.go 的 projectPageErrKeys）有一条硬约束 ——
// 其中每个 key 都必须在迁移 SQL 里登记，由
// TestProjectPageErrKeysAreRegisteredInMigrations 逐条钉住；
// 而本批的约束是「不新增数据库迁移」。
//
// 于是这里的校验失败走**就地回渲染**（与语言清单的 LocaleError 同一条路：
// 回显用户输入 + 页面上给提示），模板经 .["t"](key, 中文兜底) 取词 ——
// 词条未登记时回落调用点写的中文兜底（pkg/i18n.Translate 的第 3 级兜底链），
// 页面绝不会显示裸 key（那种症状只有人眼能发现）。
//
// 词条待补清单（item_key，需与 zh-CN / en-US 两条一起 seed）见本批报告；
// 补词条时用「本批自己的对象」枚举式 ConditionSQL，不要用 LIKE 前缀（AGENTS.md §数据库）。
const (
	// SiteSettingsHeadScriptsInvalid 自定义 Head 代码不合法：
	// 含结构性标签（<!DOCTYPE> / <html> / <head> / <body>）或超过 16 KiB。
	SiteSettingsHeadScriptsInvalid = "admin.settings.scripts.head_invalid"
	// SiteSettingsBodyScriptsInvalid 自定义 Body 代码不合法（判据同 Head）。
	SiteSettingsBodyScriptsInvalid = "admin.settings.scripts.body_invalid"
)
