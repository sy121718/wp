package unit

import (
	"reflect"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	maildto "go_wp/internal/module/mail/dto"
	userdto "go_wp/internal/module/user/dto"
	"go_wp/public/test/support"
)

func TestDTOsExposeNoCredentialFields(t *testing.T) {
	support.AssertNoCredentialFieldsInDTOs(t, support.DTOExposureOptions{})
}

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
