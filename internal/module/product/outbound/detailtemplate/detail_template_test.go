package detailtemplate

import (
	"context"
	"errors"
	"testing"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
)

type templatesStub struct {
	contenttemplatecontract.ContentTemplateService
	err error
}

func (s *templatesStub) ResolveTemplateScoped(context.Context, string, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return nil, s.err
}

type instanceStub struct {
	err error
}

func (s *instanceStub) GetByEntity(context.Context, *presentationcontract.GetByEntityReq) (*presentationcontract.InstanceResp, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &presentationcontract.InstanceResp{ID: "instance-1", TemplateID: "template-1", RenderMode: presentationcontract.RenderModeDocument}, nil
}

type modeStub struct {
	snapshotErr error
	countErr    error
}

func (s *modeStub) ListSnapshots(context.Context, *presentationcontract.ListSnapshotsReq) ([]*presentationdto.SnapshotSummary, error) {
	return nil, s.snapshotErr
}

func (s *modeStub) CountByTemplate(context.Context, *presentationcontract.CountByTemplateReq) (*presentationdto.CountByTemplateResp, error) {
	return nil, s.countErr
}

func TestDefaultTemplateDistinguishesAbsenceAndReadFailure(t *testing.T) {
	failure := errors.New("storage unavailable")
	missing := contenttemplateenums.Err(contenttemplateenums.ErrNotFound)
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{{"未配置", missing, nil}, {"故障", failure, failure}} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := New(&templatesStub{err: tc.err}, nil).DefaultDetailTemplate(context.Background(), "project-1", "product")
			if id != "" || !errors.Is(err, tc.want) {
				t.Fatalf("id=%q err=%v，期望错误=%v", id, err, tc.want)
			}
		})
	}
}

func TestBoundTemplateDistinguishesAbsenceAndReadFailure(t *testing.T) {
	failure := errors.New("storage unavailable")
	missing := presentationenums.Err(presentationenums.ErrNotFound)
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{{"未创建", missing, nil}, {"故障", failure, failure}} {
		t.Run(tc.name, func(t *testing.T) {
			bound, err := New(nil, &instanceStub{err: tc.err}).BoundDetailTemplate(context.Background(), "project-1", "product", "product-1")
			if bound.Exists || !errors.Is(err, tc.want) {
				t.Fatalf("bound=%+v err=%v，期望错误=%v", bound, err, tc.want)
			}
		})
	}
}

func TestDetailTemplatePanelPropagatesEveryReadFailure(t *testing.T) {
	failure := errors.New("storage unavailable")
	for _, tc := range []struct {
		name      string
		instances *instanceStub
		mode      *modeStub
	}{{"绑定", &instanceStub{err: failure}, &modeStub{}}, {"快照", &instanceStub{}, &modeStub{snapshotErr: failure}}, {"引用计数", &instanceStub{}, &modeStub{countErr: failure}}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(&templatesStub{}, tc.instances, tc.mode).DetailTemplatePanel(context.Background(), "project-1", "product", "product-1")
			if !errors.Is(err, failure) {
				t.Fatalf("读取错误不可降级为空事实：%v", err)
			}
		})
	}
}
