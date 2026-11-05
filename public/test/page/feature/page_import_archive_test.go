package feature

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/core"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
)

type importedArchiveProducts struct{}

func (importedArchiveProducts) ResolveCollection(context.Context, string, map[string]string) ([]map[string]any, error) {
	return nil, nil
}

func TestImportedArchiveTargetsExistingPublishedShop(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("未安装一次性导入器依赖Python3")
	}
	_, svc, projectID := newPageService(t)
	ctx := context.Background()
	shop, err := svc.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/import-existing-shop", DraftDocument: json.RawMessage(pageDocument)})
	if err != nil {
		t.Fatal(err)
	}
	module, err := filepath.Abs("../../../../scripts/wp-import/site_build.py")
	if err != nil {
		t.Fatal(err)
	}

	generate := func(detail *pagedto.PageResp) ([]byte, error) {
		t.Helper()
		raw, err := json.Marshal(detail)
		if err != nil {
			t.Fatal(err)
		}
		code := `import importlib.util,json,sys
s=importlib.util.spec_from_file_location("archive_import",sys.argv[1]);m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
m.DEFAULT_LANG="zh-CN"
def read(path,payload=None,method="POST"):
 assert path.startswith("/api/page/detail?") and method=="GET" and payload is None
 return json.loads(sys.argv[3])
m.call=read
m.require_published_shop(sys.argv[2])
print(json.dumps(m.archive_doc("category","分类",sys.argv[2]),ensure_ascii=False))`
		return exec.Command(python, "-c", code, module, shop.ID, string(raw)).CombinedOutput()
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: shop.ID})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := generate(detail); err == nil || !strings.Contains(string(out), "请先构建并发布该已有页面") {
		t.Fatalf("未发布目标应在导入写入前拒绝并给下一步: err=%v out=%s", err, out)
	}
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: shop.ID, Lang: "zh-CN"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, &pagedto.PublishReq{ID: shop.ID, Lang: "zh-CN"}); err != nil {
		t.Fatal(err)
	}
	detail, err = svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: shop.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := generate(detail)
	if err != nil {
		t.Fatalf("已发布原Shop应生成归档文档: %v %s", err, raw)
	}
	page, err := builder.ParsePage(raw)
	if err != nil {
		t.Fatal(err)
	}
	set, err := templates.NewComponentSet("../../../../internal/templates/components")
	if err != nil {
		t.Fatal(err)
	}
	locator := svc.(pagecontract.PublishedPageLocator)
	compile := func(resolve func(string) (string, error)) (*builder.CompiledPage, error) {
		return builder.Compile(page, builder.WithComponentSet(set), builder.WithProjectID(projectID), builder.WithLanguage("zh-CN"), builder.WithCompileMode(builder.CompileModePublish), builder.WithCollectionResolver(importedArchiveProducts{}), builder.WithPublishedActionPaths(resolve, nil))
	}
	compiled, err := compile(func(id string) (string, error) {
		if id != shop.ID {
			t.Fatalf("导入器不能替换明确目标: %s", id)
		}
		return locator.PublishedPagePath(ctx, projectID, id, "zh-CN")
	})
	path, pathErr := locator.PublishedPagePath(ctx, projectID, shop.ID, "zh-CN")
	if err != nil || pathErr != nil || !strings.Contains(compiled.HTML, `href="`+path+`"`) {
		t.Fatalf("真实导入文档Compile须输出该已发布目标: err=%v pathErr=%v path=%s", err, pathErr, path)
	}
	if _, err := compile(func(string) (string, error) { return "", nil }); !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("目标未发布时正式Compile仍必须拒绝: %v", err)
	}
}
