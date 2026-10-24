// Package pluginenums 插件模块响应消息。
//
// 常量**值**是 i18n key（不是中文文案）：中文只以「出口处的中文兜底」形态存在
// （inbound/http/plugin_err.go 的 pluginMsg、service/plugin_runtime.go 的
// defaultComponentHint）。理由见 pkg/response.IsBusinessError —— 判据是值的**形态**，
// 中文值两层判据都不命中，会被当成内部错误（500 + 通用文案）。
package pluginenums

import "go_wp/pkg/facing"

// 响应消息。
const (
	MsgInstallSuccess   = "MsgInstallSuccess"   // 插件安装成功
	MsgInstallFailed    = "MsgInstallFailed"    // 插件安装失败
	MsgListSuccess      = "MsgListSuccess"      // 插件列表获取成功
	MsgToggleSuccess    = "MsgToggleSuccess"    // 插件状态更新成功
	MsgUninstallSuccess = "MsgUninstallSuccess" // 插件已卸载
	MsgDetailSuccess    = "MsgDetailSuccess"    // 插件详情获取成功

	ErrInvalidParam    = "ErrInvalidParam"    // 参数错误
	ErrPluginNotFound  = "ErrPluginNotFound"  // 插件不存在
	ErrInstallParse    = "ErrInstallParse"    // 插件包解析失败
	ErrUnsafePackage   = "ErrUnsafePackage"   // 插件包包含不安全内容，已拒绝
	ErrVersionRegress  = "ErrVersionRegress"  // 插件版本不能回退
	ErrStorageFailure  = "ErrStorageFailure"  // 插件包存储失败
	ErrToggleFailed    = "ErrToggleFailed"    // 插件状态更新失败
	ErrUninstallFailed = "ErrUninstallFailed" // 插件卸载失败
	ErrMigrationFailed = "ErrMigrationFailed" // 插件数据层迁移失败
)

// 页面出口的文案 key（Go 侧生成、会显示给运营；中文兜底在各自的出口处，不在本包）。
//
// 形态是 `模块.类别.语义`（pkg/response.IsBusinessError 判据 1），与上面的常量名形态
// （判据 2）在同一包里并存 —— 两层判据都只认 ASCII 形态的值，所以中文一律留作出口的
// fallback，不进这里。下面每条的出处与中文兜底原文见行尾注释与迁移登记表。
const (
	// ErrModuleUnwired 插件模块未装配：页面写操作拿不到契约时的失败原因。
	ErrModuleUnwired = "plugin.err.moduleUnwired" // 插件模块未装配，该操作无法执行
	// ErrInstallNoFile 上传请求里没有插件包文件。
	ErrInstallNoFile = "plugin.err.installNoFile" // 没有收到插件包文件
	// ErrPackageUnreadable 插件包读取失败或超过体积上限。
	ErrPackageUnreadable = "plugin.err.packageUnreadable" // 插件包读取失败，或文件超过 52MB 上限
	// ErrListFailed 插件列表取数失败（只进页面提示条，不经 ?err= 回带）。
	ErrListFailed = "plugin.err.listFailed" // 插件列表加载失败
	// NoticeInstallReselect 安装失败回跳后的补充提示：浏览器不会重传已选文件。
	NoticeInstallReselect = "plugin.notice.installReselect" // 浏览器不会重传已选文件，请重新选择文件后再提交
	// TitlePlugins 插件管理页标题（浏览器标题栏 / 面包屑；页面 h1 另有 admin.plugins.* 词条）。
	TitlePlugins = "plugin.title.plugins" // 插件管理
	// HintComponentDefault 插件组件在组件库里的默认提示（manifest 未声明 hint 时）。
	HintComponentDefault = "plugin.hint.componentDefault" // 插件组件
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrInvalidParam` 这样的裸 key
// （这批常量的值就是常量名本身，旧式形态）。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrModuleUnwired:     "插件模块未装配，该操作无法执行",
	ErrInstallNoFile:     "没有收到插件包文件",
	ErrPackageUnreadable: "插件包读取失败，或文件超过 52MB 上限",
	ErrListFailed:        "插件列表加载失败",
	ErrInvalidParam:      "请求参数无效",
	ErrPluginNotFound:    "插件不存在",
	ErrInstallParse:      "插件包解析失败",
	ErrUnsafePackage:     "插件包包含不安全内容，已拒绝",
	ErrVersionRegress:    "插件版本不能回退",
	ErrStorageFailure:    "插件包存储失败",
	ErrToggleFailed:      "插件状态更新失败",
	ErrUninstallFailed:   "插件卸载失败",
	ErrMigrationFailed:   "插件数据层迁移失败",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
