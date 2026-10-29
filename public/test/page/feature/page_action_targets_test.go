package feature

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/pipeline"
	json "go_wp/pkg/jsonx"
)

const plainActionDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"t1","props":{"text":"PLAIN-PAGE"}}]}`

func TestFirstPublishSelfActionUsesFrozenPathAndOtherDraftFails(t *testing.T) {
	_, api, projectID := newPageService(t)
	ctx := context.Background()
	self, err := api.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/self-checkout", DraftDocument: json.RawMessage(plainActionDocument)})
	if err != nil {
		t.Fatal(err)
	}
	doc := func(target string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"cart","type":"core.cartIcon","props":{"mode":"inline"},"actions":{"checkout":{"kind":"page","pageId":%q}}}]}`, target))
	}
	if _, err := api.SaveDraft(ctx, &pagedto.SaveDraftReq{ID: self.ID, ExpectedVersion: self.DraftVersion, DraftPath: "/self-checkout", DraftDocument: doc(self.ID)}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Build(ctx, &pagedto.BuildReq{ID: self.ID}); err != nil {
		t.Fatalf("自指首次构建应使用冻结路径: %v", err)
	}
	if _, err := api.Publish(ctx, &pagedto.PublishReq{ID: self.ID}); err != nil {
		t.Fatal(err)
	}
	key := pipeline.PageTargetKey(self.ID)
	refs, err := api.(*pageservice.Service).FindPagesByDependency(ctx, projectID, key.Kind, key.Key)
	if err != nil || len(refs) != 1 || refs[0].ID != self.ID {
		t.Fatalf("自身也须登记真实目标依赖: %+v %v", refs, err)
	}
	draft, err := api.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "search", ContentTargetType: "none", DraftPath: "/other-draft", DraftDocument: json.RawMessage(plainActionDocument)})
	if err != nil {
		t.Fatal(err)
	}
	source, err := api.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/other-consumer", DraftDocument: doc(draft.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Build(ctx, &pagedto.BuildReq{ID: source.ID}); err == nil {
		t.Fatal("其它未发布目标仍必须拒绝")
	}
}

func TestPublishedPageActionTargetIsScopedExactAndReadable(t *testing.T) {
	_, svc, projectID := newPageService(t)
	ctx := context.Background()
	target, err := svc.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/chosen-checkout", DraftDocument: json.RawMessage(plainActionDocument)})
	if err != nil {
		t.Fatal(err)
	}
	locator := svc.(pagecontract.PublishedPageLocator)
	if path, err := locator.PublishedPagePath(ctx, projectID, target.ID, "zh-CN"); err != nil || path != "" {
		t.Fatalf("草稿不能作为发布目标: %s %v", path, err)
	}
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: target.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: target.ID}); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: target.ID})
	if err != nil || len(detail.Publications) == 0 {
		t.Fatalf("发布必须有精确语言账本: %+v %v", detail, err)
	}
	pub := detail.Publications[0]
	if got, err := locator.PublishedPagePath(ctx, projectID, target.ID, pub.Lang); err != nil || got != pub.ActivePath {
		t.Fatalf("路径必须与真实Publication相等: got=%s pub=%+v err=%v", got, pub, err)
	}
	if got, err := locator.PublishedPagePath(ctx, projectID, target.ID, "missing-language"); err != nil || got != "" {
		t.Fatalf("不能回退另一语言: %s %v", got, err)
	}
	if _, err := locator.PublishedPagePath(ctx, uuid.NewString(), target.ID, pub.Lang); err == nil {
		t.Fatal("跨工程必须拒绝")
	}
	sourceDoc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"cart","type":"core.cartIcon","props":{"mode":"inline"},"actions":{"checkout":{"kind":"page","pageId":%q}}}]}`, target.ID)
	source, err := svc.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "search", ContentTargetType: "none", DraftPath: "/cart-action-source", DraftDocument: json.RawMessage(sourceDoc)})
	if err != nil {
		t.Fatal(err)
	}
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: source.ID, Lang: pub.Lang})
	if err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join(pipeline.DefaultArtifactRoot(), "artifacts", built.StagedHash, "index.html"))
	if err != nil || !strings.Contains(string(html), `href="`+pub.ActivePath+`"`) {
		t.Fatalf("真实构建须输出目标Publication路径: err=%v", err)
	}
	if err := os.Remove(filepath.Join(pipeline.ActiveRoot(), strings.TrimPrefix(pub.ActivePath, "/"), "index.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := locator.PublishedPagePath(ctx, projectID, target.ID, pub.Lang); err == nil {
		t.Fatal("缺失产物不能作为已发布目标")
	}
}
