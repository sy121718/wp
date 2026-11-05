package feature

import (
	"context"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
)

// stubMasterDataChanges 是本 fixture 的空审计端口：product.NewService 的第三个
// 参数是必需依赖（nil 会 panic，让装配期就报出没接 masterdata），而这里的用例
// 不触写路径、也不校验审计内容，所以给一个全部返回零值的实现。
//
// 跨包用不了 product 包里的同名类型（那是 package productservice 的私有测试类型），
// 所以在本包按契约重写一份；方法集以 masterdatacontract.MasterDataService 为准，
// 下面的编译期断言会拦住契约变动漏改。
type stubMasterDataChanges struct{}

var _ masterdatacontract.MasterDataService = stubMasterDataChanges{}

func (stubMasterDataChanges) RecordChanges(context.Context, []*masterdatacontract.ChangeInput) error {
	return nil
}

func (stubMasterDataChanges) RecordChangesTx(context.Context, *gorm.DB, []*masterdatacontract.ChangeInput) error {
	return nil
}

func (stubMasterDataChanges) ListChanges(context.Context, *masterdatadto.ListChangeReq) ([]*masterdatadto.ChangeResp, error) {
	return nil, nil
}

func (stubMasterDataChanges) CountChanges(context.Context, *masterdatadto.ListChangeReq) (int64, error) {
	return 0, nil
}

func (stubMasterDataChanges) ListEntities(context.Context, *masterdatadto.ListEntityReq) ([]*masterdatadto.EntityHistoryResp, error) {
	return nil, nil
}

func (stubMasterDataChanges) CountEntities(context.Context, *masterdatadto.ListEntityReq) (int64, error) {
	return 0, nil
}

func (stubMasterDataChanges) EntityTimeline(context.Context, *masterdatadto.EntityTimelineReq) (*masterdatadto.EntityTimelineResp, error) {
	return nil, nil
}
