package userdto

// user_req.go — 用户模块请求（issue #36：注册 / 验证 / 密码重置 / 登录 / 账号中心）。

import (
	"net/url"
	"strconv"
	"strings"

	"go_wp/pkg/utils"
)

// RegisterReq 注册。
//
// 注意这里**没有 role 字段**：用户侧的注册不接受调用方指定身份
// （注册即普通用户），身份只能由管理侧授予。
type RegisterReq struct {
	Username string `form:"username"`
	Email    string `form:"email"`
	Password string `form:"password"`
	Nickname string `form:"nickname"`
	// RegisterIP / RegisterLocation 由 inbound 覆盖写入，客户端不可伪造。
	RegisterIP       string `form:"-" json:"-"`
	RegisterLocation string `form:"-" json:"-"`
	// Locale 决定用哪套语言模板（空 = 通用模板）。
	Locale string `form:"-" json:"-"`
}

// ActivateEmailReq 邮箱验证。
type ActivateEmailReq struct {
	Key string `form:"key" json:"key"`
	// Locale 决定欢迎邮件用哪套语言模板（空 = 通用模板）。
	Locale string `form:"-" json:"-"`
}

// ResendActivationReq 重发验证邮件。
type ResendActivationReq struct {
	Email  string `form:"email"`
	Locale string `form:"-" json:"-"`
}

// PasswordResetReqRequest 申请重置密码。
type PasswordResetReqRequest struct {
	Email  string `form:"email"`
	Locale string `form:"-" json:"-"`
}

// ResetPasswordReq 用重置码改密。
type ResetPasswordReq struct {
	Email       string `form:"email"`
	Key         string `form:"key"`
	NewPassword string `form:"password"`
}

// LoginReq 密码登录。
//
// Account 同时接受用户名与邮箱：用户记不清自己当初用哪个注册的很常见，
// 强制二选一只是把「登录失败」变成「登录失败而且我不知道该填哪个」。
type LoginReq struct {
	Account  string `form:"account"`
	Password string `form:"password"`
	// RememberMe 由复选框产生，取值是 "on"（勾选时才提交）—— gin 的 bool 绑定
	// 走 strconv.ParseBool，不认 "on"，所以这一档由 BindForm 宽容解析。
	RememberMe bool `form:"-"`
}

// BindForm 见 user_form.go 的宽容取值口径。
func (r *LoginReq) BindForm(values url.Values) error {
	r.RememberMe = formBool(values, "remember")
	return nil
}

// LoginResp 登录结果。
//
// **只有令牌，没有用户资料以外的凭据**：令牌本身是凭据，其余字段是渲染当前页要用的快照。
type LoginResp struct {
	Token     string
	UserID    uint64
	Username  string
	Nickname  string
	Avatar    string
	Email     string
	ExpiresAt utils.JSONTime
}

// SessionItem 登录设备（账号中心的「我的设备」一行）。
type SessionItem struct {
	// ID 是会话令牌的 sha256（Redis 索引里的成员，也是撤销设备时的入参）。
	// 它不能反推出令牌，所以可以出现在页面里；会话状态本身只在 Redis，没有数据库行 id。
	ID           string
	UserAgent    string
	IP           string
	Location     string
	LastActiveAt *utils.JSONTime
	CreatedAt    *utils.JSONTime
	// LastActiveText / CreatedText 是给页面直接显示的时间文本。
	//
	// 模板里对 *utils.JSONTime 调 Format 要先判空指针，判空与格式化会散落在每个模板里；
	// 集中在这一层做，口径（格式、空值显示成什么）就只有一处。
	LastActiveText string
	CreatedText    string
	// Current 标记「这一台就是我现在用的」—— 界面上不给当前设备「踢出」按钮。
	Current bool
}

// UpdateProfileReq 保存扩展资料。
//
// 全部字段可选：账号中心是「改了哪个字段就提交哪个」的形态，
// 用零值区分「没传」与「传了空」在这里做不到，所以空串一律按「清空该字段」处理
// （见 service 的说明）。
type UpdateProfileReq struct {
	UserID    uint64 `form:"-" json:"-"`
	Nickname  string `form:"nickname"`
	FirstName string `form:"firstName"`
	LastName  string `form:"lastName"`
	// Gender 是下拉框产生的整数；解析失败按 0（未指定），见 BindForm。
	Gender   int    `form:"-"`
	Birthday string `form:"birthday"`
	Bio      string `form:"bio"`
	Website  string `form:"website"`
	Locale   string `form:"locale"`
	Timezone string `form:"timezone"`
	Country  string `form:"country"`
	Province string `form:"province"`
	City     string `form:"city"`
	Address  string `form:"address"`
	Postcode string `form:"postcode"`
	Phone    string `form:"phone"`
	Company  string `form:"company"`
}

// BindForm 只补反射绑不了的两档：整数下拉框与复选框。口径与前端一致 ——
// 这些值由页面上的固定选项产生，能收到非法值只可能是手工构造的请求，
// 用默认值即可（真正的合法性由 service 兜底）。
func (r *UpdateProfileReq) BindForm(values url.Values) error {
	r.Gender = formInt(values, "gender", 0)
	return nil
}

// UpdatePreferenceReq 保存前台偏好。
type UpdatePreferenceReq struct {
	UserID            uint64 `form:"-" json:"-"`
	Theme             string `form:"theme"`
	Locale            string `form:"locale"`
	Timezone          string `form:"timezone"`
	PageSize          int    `form:"-"`
	EmailNotify       bool   `form:"-"`
	SmsNotify         bool   `form:"-"`
	ProfileVisibility string `form:"profileVisibility"`
	ShowOnline        bool   `form:"-"`
}

// BindForm 补整数与复选框（每页条数默认 20，越界由 service 拒）。
func (r *UpdatePreferenceReq) BindForm(values url.Values) error {
	r.PageSize = formInt(values, "pageSize", 20)
	r.EmailNotify = formBool(values, "emailNotify")
	r.SmsNotify = formBool(values, "smsNotify")
	r.ShowOnline = formBool(values, "showOnline")
	return nil
}

// ChangePasswordReq 修改密码（已登录用户，需验证旧密码）。
type ChangePasswordReq struct {
	UserID      uint64 `form:"-" json:"-"`
	OldPassword string `form:"oldPassword"`
	NewPassword string `form:"newPassword"`
}

// ResetLinkReq 重置密码页的链接参数（GET：?email=…&key=…）。
//
// 单独一个类型而不是复用 ResetPasswordReq：页面上还**没有**新密码，
// 复用会让「链接校验」与「提交新密码」共用一个必填集，两边都会写错。
type ResetLinkReq struct {
	Email string `form:"email"`
	Key   string `form:"key"`
}

// RevokeSessionReq 踢掉某台设备：入参是会话令牌的 sha256，不是数据库行 id
// （会话不落库，见账号中心「我的设备」）。
type RevokeSessionReq struct {
	ID string `form:"id"`
}

// formText 取文本字段并去空白。
//
// 这三档解析放在 dto 而不是控制器：它们是**表单字段的取值口径**（访客页面的
// 输入一律按文本、复选框没勾选就不提交、整数字段非法时用默认值），
// 与字段定义放在一起才不会在两处各写一份。
func formText(values url.Values, key string) string {
	return strings.TrimSpace(values.Get(key))
}

// formBool 解析复选框：HTML 未勾选时不提交该字段，勾选时提交 "on"。
func formBool(values url.Values, key string) bool {
	switch strings.ToLower(strings.TrimSpace(values.Get(key))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// formInt 解析整数字段，解析失败用默认值。
func formInt(values url.Values, key string, def int) int {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}
