package productservice

import (
	"context"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
)

// stubMasterDataChanges 是只读/参数校验测试用的空审计端口：这些用例在触到写路径之前就返回，
// 真正验证审计内容的用例自己注入捕获型替身。
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
