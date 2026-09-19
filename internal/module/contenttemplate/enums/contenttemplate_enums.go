// Package contenttemplateenums contenttemplate 模块响应消息。
package contenttemplateenums

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
