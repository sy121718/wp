package userdto

// user_req.go — 用户模块请求（issue #36：注册 / 验证 / 密码重置 / 登录 / 账号中心）。

import "time"

// RegisterReq 注册。
//
// 注意这里**没有 role 字段**：用户侧的注册不接受调用方指定身份
// （注册即普通用户），身份只能由管理侧授予。
type RegisterReq struct {
	Username string
	Email    string
	Password string
	Nickname string
	// RegisterIP / RegisterLocation 由 inbound 覆盖写入，客户端不可伪造。
	RegisterIP       string
	RegisterLocation string
	// Locale 决定用哪套语言模板（空 = 通用模板）。
	Locale string
}

// ActivateEmailReq 邮箱验证。
type ActivateEmailReq struct {
	Key string
	// Locale 决定欢迎邮件用哪套语言模板（空 = 通用模板）。
	Locale string
}

// ResendActivationReq 重发验证邮件。
type ResendActivationReq struct {
	Email  string
	Locale string
}

// PasswordResetReqRequest 申请重置密码。
type PasswordResetReqRequest struct {
	Email  string
	Locale string
}

// ResetPasswordReq 用重置码改密。
type ResetPasswordReq struct {
	Email       string
	Key         string
	NewPassword string
}

// LoginReq 密码登录。
//
// Account 同时接受用户名与邮箱：用户记不清自己当初用哪个注册的很常见，
// 强制二选一只是把「登录失败」变成「登录失败而且我不知道该填哪个」。
type LoginReq struct {
	Account    string
	Password   string
	RememberMe bool
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
	ExpiresAt time.Time
}

// SessionItem 登录设备（账号中心的「我的设备」一行）。
type SessionItem struct {
	// ID 是会话令牌的 sha256（Redis 索引里的成员，也是撤销设备时的入参）。
	// 它不能反推出令牌，所以可以出现在页面里；会话状态本身只在 Redis，没有数据库行 id。
	ID           string
	UserAgent    string
	IP           string
	Location     string
	LastActiveAt *time.Time
	CreatedAt    *time.Time
	// LastActiveText / CreatedText 是给页面直接显示的时间文本。
	//
	// 模板里对 *time.Time 调 Format 要先判空指针，判空与格式化会散落在每个模板里；
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
	UserID    uint64
	Nickname  string
	FirstName string
	LastName  string
	Gender    int
	Birthday  string
	Bio       string
	Website   string
	Locale    string
	Timezone  string
	Country   string
	Province  string
	City      string
	Address   string
	Postcode  string
	Phone     string
	Company   string
}

// UpdatePreferenceReq 保存前台偏好。
type UpdatePreferenceReq struct {
	UserID            uint64
	Theme             string
	Locale            string
	Timezone          string
	PageSize          int
	EmailNotify       bool
	SmsNotify         bool
	ProfileVisibility string
	ShowOnline        bool
}

// ChangePasswordReq 修改密码（已登录用户，需验证旧密码）。
type ChangePasswordReq struct {
	UserID      uint64
	OldPassword string
	NewPassword string
}
