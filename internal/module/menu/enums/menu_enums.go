// Package menuenums menu 模块的业务消息。
package menuenums

const (
	ErrMenuNotFound        = "菜单不存在"
	ErrMenuHasChildren     = "该菜单下有子菜单，无法删除"
	ErrMenuIsSystem        = "系统内置菜单不可删除或修改类型"
	ErrMenuCircle          = "不能将菜单移动到自身或其子级下"
	ErrCodeNotBindable     = "目录、iframe 和外链不能绑定权限编码"
	ErrCodeRequired        = "菜单和按钮类型必须绑定权限编码"
	ErrCodeNotEnabled      = "绑定的权限编码不存在或未启用"
	ErrComponentRequired   = "菜单类型必须填写组件路径"
	ErrComponentNotAllowed = "只有菜单类型可以填写组件路径"
	ErrComponentInvalid    = "组件路径必须是 /src/views/ 下的完整 Vue 文件路径"
)

const (
	MsgSuccess    = "success"
	MsgBadRequest = "请求参数错误"
)
