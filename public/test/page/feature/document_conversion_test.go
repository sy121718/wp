package feature

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go_wp/pkg/builder"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	json "go_wp/pkg/jsonx"
	"gorm.io/gorm"
)

type conversionFailurePort struct {
	contenttemplatecontract.DraftDocumentWriterTx
}

func (p conversionFailurePort) SaveDraftDocumentTx(ctx context.Context, tx *gorm.DB, projectID, id string, version int64, doc json.RawMessage) error {
	if err := p.DraftDocumentWriterTx.SaveDraftDocumentTx(ctx, tx, projectID, id, version, doc); err != nil {
		return err
	}
	return errors.New("injected after template version write")
}

func TestDocumentConversionAtomicConflictAndReplay(t *testing.T) {
	db, api, projectID := newPageService(t)
	svc := api.(*pageservice.Service)
	templates := injectStructureTemplatePort(t, db, api)
	ctx := context.Background()
	page, err := api.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/convert", DraftDocument: json.RawMessage(pageDocument)})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.Create(ctx, &contenttemplatedto.CreateReq{ProjectID: projectID, EntityType: "header", Name: "待转换", DraftDocument: json.RawMessage(pageDocument)})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := builder.DocumentDigest(page.DraftDocument)
	if err != nil {
		t.Fatal(err)
	}
	tplHash, err := builder.DocumentDigest(tpl.DraftDocument)
	if err != nil {
		t.Fatal(err)
	}
	plan := []pagecontract.DocumentConversion{
		{Kind: "page", ID: page.ID, ExpectedVersion: 1, ExpectedHash: digest, Document: json.RawMessage(plainActionDocument)},
		{Kind: "template", ID: tpl.ID, ExpectedVersion: 1, ExpectedHash: tplHash, Document: json.RawMessage(plainActionDocument)},
	}
	assertVersions := func(want int64) {
		t.Helper()
		gotPage, e := api.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
		if e != nil {
			t.Fatal(e)
		}
		gotTemplate, e := templates.GetScoped(ctx, projectID, tpl.ID)
		if e != nil {
			t.Fatal(e)
		}
		if gotPage.DraftVersion != want || gotTemplate.DraftVersion != want {
			t.Fatalf("页面与模板版本必须同进退: page=%d template=%d want=%d", gotPage.DraftVersion, gotTemplate.DraftVersion, want)
		}
		var pageCount, templateCount int64
		if e := db.Table("page_revisions").Where("page_id = ?", page.ID).Count(&pageCount).Error; e != nil {
			t.Fatal(e)
		}
		if e := db.Table("content_template_versions").Where("template_id = ?", tpl.ID).Count(&templateCount).Error; e != nil {
			t.Fatal(e)
		}
		if pageCount != want || templateCount != want {
			t.Fatalf("不可变快照须与版本等量: page=%d template=%d want=%d", pageCount, templateCount, want)
		}
	}
	if records, e := svc.ApplyDocumentConversions(ctx, projectID, plan, conversionFailurePort{templates}); e == nil || records != nil {
		t.Fatalf("模板版本写后失败必须整笔回滚: records=%+v err=%v", records, e)
	}
	assertVersions(1)
	conflict := append([]pagecontract.DocumentConversion(nil), plan...)
	conflict[1].ExpectedHash = digest[:63] + "x"
	if records, e := svc.ApplyDocumentConversions(ctx, projectID, conflict, templates); e == nil || records != nil {
		t.Fatalf("摘要不符必须拒绝整批: %+v %v", records, e)
	}
	assertVersions(1)
	records, err := svc.ApplyDocumentConversions(ctx, projectID, plan, templates)
	if err != nil {
		t.Fatal(err)
	}
	assertVersions(2)
	replayed, err := svc.ApplyDocumentConversions(ctx, projectID, plan, templates)
	if err != nil || !reflect.DeepEqual(records, replayed) {
		t.Fatalf("重放记录必须相等: %+v %+v %v", records, replayed, err)
	}
	assertVersions(2)
	if _, err = api.SaveDraft(ctx, &pagedto.SaveDraftReq{ID: page.ID, ExpectedVersion: 2, DraftPath: "/convert", DraftDocument: json.RawMessage(plainActionDocument)}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApplyDocumentConversions(ctx, projectID, plan, templates); err == nil {
		t.Fatal("批次之后的作者新版本不能覆盖")
	}
}
