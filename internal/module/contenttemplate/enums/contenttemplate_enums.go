// Package contenttemplateenums contenttemplate 模块响应消息与错误兜底。
//
// 错误的对外文案跟**声明处**走（同 presentation / membership / content 的形态）：常量值就是
// i18n key，中文兜底在 errFallbacks，出口只调一次共享判据 pkg/facing.FactOf。
package contenttemplateenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess" // 模板创建成功
	MsgUpdateSuccess = "MsgUpdateSuccess" // 模板更新成功
	MsgListSuccess   = "MsgListSuccess"   // 模板列表获取成功
	MsgDetailSuccess = "MsgDetailSuccess" // 模板详情获取成功

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // 模板不存在
	// ErrTemplateInUse 模板仍被自动发布实例引用，不能删除（外键拒绝）。
	ErrTemplateInUse = "ErrTemplateInUse"
	// ErrStructureTemplateInUse 结构模板（页眉 / 页脚）仍被其它模板绑定为结构槽位，不能删除。
	//
	// 与外键拒绝的 ErrTemplateInUse 区分开：那条说的是「实例在用这套模板」，
	// 这条说的是「别的模板把这张页眉套在了自己头上」—— 处置方式不同
	//（前者先处理实例，后者先到那个模板里解绑）。
	ErrStructureTemplateInUse = "ErrStructureTemplateInUse"
	ErrInvalidType            = "ErrInvalidType" // 不支持的内容类型
	ErrDataInvalid            = "ErrDataInvalid" // 模板文档格式非法
	// ErrFieldBindingInvalid 文档内声明的字段绑定越界（不在数据源字段白名单内，
	// 或绑定了非本模板数据源的字段，issue #6）。白名单唯一来源 = 实体类型注册表。
	ErrFieldBindingInvalid = "ErrFieldBindingInvalid"
	// ErrProjectRequired 未指定工程且无法从唯一工程推导（0 个或多个工程）。
	ErrProjectRequired = "ErrProjectRequired"
	// ErrProjectNotFound 显式指定的工程不存在。
	ErrProjectNotFound = "ErrProjectNotFound"
)

// errFallbacks 每个错误常量的中文兜底（与声明的 key 一起交给 pkg/facing.New）。
var errFallbacks = map[string]string{
	ErrInvalidParam:           "参数不合法：请检查名称、类型与文档后重试。",
	ErrNotFound:               "模板不存在，可能已被删除。",
	ErrTemplateInUse:          "这套模板仍被自动发布实例引用，不能删除。",
	ErrStructureTemplateInUse: "这张结构模板仍被其它模板绑定为结构槽位，先到那里解绑再删。",
	ErrInvalidType:            "不支持的内容类型。",
	ErrDataInvalid:            "模板文档格式非法：请回到工作台重新保存后再试。",
	ErrFieldBindingInvalid:    "模板里的字段绑定越界：只能用本模板数据源内的字段。",
	ErrProjectRequired:        "请先选择站点工程。",
	ErrProjectNotFound:        "选择的站点工程不存在。",
}

// Err 声明一条自带对外文案的业务错误（pkg/facing.New：key + 中文兜底）。
func Err(key string) error {
	return facing.New(key, errFallbacks[key])
}

// ErrDetail 声明一条带明细的业务错误：明细拼在半角冒号之后（判据只认 ": "）。
func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

// FallbackFor 取一个 key 的中文兜底（调用方只有 key、没有事实时用）。
func FallbackFor(key string) string {
	return errFallbacks[key]
}

// —— 点分 key 常量（新式）——
//
// 值是 sys_i18n 的 item_key，命名按「去掉模块子域前缀（`admin.content.templates.`）
// 后的语义路径」；中文兜底留在调用点。与上面那批 `ErrXxx = "ErrXxx"` 分开成组：
// 老式形态的值就是常量名本身（sys_i18n 里存同名 key）。
//
// 为什么词条 key 必须提成常量：控制器里写 key 字面量时，词条改名没有编译期保护，
// 静默失配的表现是「英文界面回落成中文」，不报错也不记日志；
// 门禁 scripts/check-controller-i18n-literals.sh（inbound/http/*.go 里出现字面量即失败）。
const (
	// ImpactLoadFailed 本次没取到引用面（工程列表未读到）—— 与「能力未装配」分开的另一条归因。
	ImpactLoadFailed = "admin.content.templates.impact.loadFailed"
	// Heading 模板列表页标题；回跳提示页的「立即前往」链接文字复用同一条词条（不新增全站词条）。
	Heading = "admin.content.templates.heading"
)
