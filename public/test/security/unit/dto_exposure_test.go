package unit

// dto_exposure_test.go — 对外响应 DTO 的凭据字段守卫（SEC-009）。
//
// 两条一起用：
//   · TestDTOsExposeNoCredentialFields —— AST 全量扫描 internal 下所有 dto 包，
//     新增文件自动纳入，零维护；
//   · TestCriticalResponseDTOExposure —— 反射断言重点类型（审计点名的 mail / admin / user），
//     类型被改名或删除时编译期就报错，不会静默少测一块。

import (
	"reflect"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	maildto "go_wp/internal/module/mail/dto"
	userdto "go_wp/internal/module/user/dto"
	"go_wp/public/test/support"
)

// TestDTOsExposeNoCredentialFields 全量守卫。
func TestDTOsExposeNoCredentialFields(t *testing.T) {
	support.AssertNoCredentialFieldsInDTOs(t, support.DTOExposureOptions{})
}

// TestCriticalResponseDTOExposure 重点类型钉死。
//
// 为什么这三处：mail 的发信账号实体带 SMTP 密码、admin 带密码哈希与登录失败计数、
// user 的客户管理是这条守卫最初的由来。三者都是「实体字段直接搬运就会泄露」的形状，
// 而它们的响应恰好都长得像实体（字段名一一对应）。
func TestCriticalResponseDTOExposure(t *testing.T) {
	support.AssertNoCredentialFields(t,
		reflect.TypeOf(maildto.AccountItem{}),
		reflect.TypeOf(maildto.TestSendResp{}),
		reflect.TypeOf(admindto.AdminItem{}),
		reflect.TypeOf(admindto.AdminDetailResp{}),
		reflect.TypeOf(admindto.AdminProfileResp{}),
		reflect.TypeOf(admindto.AdminLoginResp{}),
		reflect.TypeOf(userdto.CustomerResp{}),
		reflect.TypeOf(userdto.CustomerListResp{}),
		reflect.TypeOf(userdto.CustomerCounters{}),
		reflect.TypeOf(userdto.CustomerStatusResp{}),
		reflect.TypeOf(userdto.CustomerUnlockResp{}),
	)
}
