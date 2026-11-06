package unit

import (
	"context"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	"go_wp/pkg/facing"
)

func TestStructureTemplateDeleteGuard(t *testing.T) {
	svc, _, projectID := newService(t)
	if svc == nil {
		return
	}
	// 本用例的引用来自模板之间的结构槽位绑定，外部端口给空集即可。
	withRefStub(svc, &stubTemplateRefs{})
	ctx := context.Background()

	newHeader := func(t *testing.T, name string) *contenttemplatedto.TemplateResp {
		t.Helper()
		res, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
			EntityType: contenttemplatemodel.EntityTypeHeader, Name: name,
			DraftDocument: []byte(headerDoc), ProjectID: projectID,
		})
		if err != nil {
			t.Fatalf("创建结构模板失败: %v", err)
		}
		return res
	}

	t.Run("被其它模板绑定为页眉时拒绝删除", func(t *testing.T) {
		header := newHeader(t, "站点页眉")
		doc := `{"settings":{"layout":{"mode":"full"},"structure":{"headerTemplateId":"` + header.ID + `"}},` +
			`"root":[{"id":"h1","type":"core.heading","props":{"text":"正文"}}]}`
		if _, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
			EntityType: "article", Name: "文章模板", DraftDocument: []byte(doc), ProjectID: projectID,
		}); err != nil {
			t.Fatalf("创建绑定结构模板的内容模板失败: %v", err)
		}

		err := svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID})
		if err == nil {
			t.Fatal("被其它模板绑定的结构模板不该删除成功")
		}
		if !strings.Contains(err.Error(), contenttemplateenums.ErrStructureTemplateInUse) {
			t.Fatalf("错误应带 %s 前缀（handler 的三件套按它取文案）: %v",
				contenttemplateenums.ErrStructureTemplateInUse, err)
		}
		// 可定位明细走结构化 BlockedItems（对外只进 data.items），不拼进主消息。
		blocked, ok := err.(interface{ BlockedItems() []facing.BlockedItem })
		if !ok || len(blocked.BlockedItems()) != 1 {
			t.Fatalf("拒绝事实应带一条可定位明细: %v", err)
		}
		if detail := blocked.BlockedItems()[0].Fallback; !strings.Contains(detail, "文章模板") || !strings.Contains(detail, "页眉") {
			t.Fatalf("明细应给出模板名与槽位: %q", detail)
		}

		if _, err := svc.Get(ctx, &contenttemplatedto.GetReq{ID: header.ID}); err != nil {
			t.Fatalf("删除被拒绝后模板应仍然存在: %v", err)
		}

		unbound := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"正文"}}]}`
		if _, err := svc.Update(ctx, &contenttemplatedto.UpdateReq{ID: mustArticleID(t, svc, ctx), DraftDocument: []byte(unbound)}); err != nil {
			t.Fatalf("解绑失败: %v", err)
		}
		if err := svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID}); err != nil {
			t.Fatalf("解绑后应可删除: %v", err)
		}
	})

	t.Run("被其它模板绑定为槽位模板时同样拒绝", func(t *testing.T) {
		header := newHeader(t, "公告条模板")
		doc := `{"settings":{"layout":{"mode":"full"},"structure":{"slotTemplates":{"announcement":"` + header.ID + `"}}},` +
			`"root":[{"id":"h1","type":"core.heading","props":{"text":"正文"}}]}`
		if _, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
			EntityType: "article", Name: "带公告条的模板", DraftDocument: []byte(doc), ProjectID: projectID,
		}); err != nil {
			t.Fatalf("创建带槽位模板的内容模板失败: %v", err)
		}
		err := svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID})
		if err == nil || !strings.Contains(err.Error(), contenttemplateenums.ErrStructureTemplateInUse) {
			t.Fatalf("槽位模板绑定同样应拒绝删除: %v", err)
		}
		blocked, ok := err.(interface{ BlockedItems() []facing.BlockedItem })
		if !ok || len(blocked.BlockedItems()) != 1 || !strings.Contains(blocked.BlockedItems()[0].Fallback, "announcement") {
			t.Fatalf("明细应指出槽位: %v", err)
		}
	})

	t.Run("未被引用时可删除", func(t *testing.T) {
		header := newHeader(t, "没人用的页眉")
		if err := svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID}); err != nil {
			t.Fatalf("未被引用的结构模板应可删除: %v", err)
		}
	})
}

func mustArticleID(t *testing.T, svc interface {
	List(ctx context.Context, req *contenttemplatedto.ListReq) ([]*contenttemplatedto.TemplateResp, error)
}, ctx context.Context) string {
	t.Helper()
	list, err := svc.List(ctx, &contenttemplatedto.ListReq{EntityType: "article"})
	if err != nil || len(list) == 0 {
		t.Fatalf("读取 article 模板失败: %v", err)
	}
	return list[0].ID
}
