package unit

// structure_template_delete_guard_test.go — 结构模板的**删除保护**。
//
// 结构模板的引用写在 JSONB 文档里（settings.structure），数据库外键管不到：
// 没有这道拦截，删掉一套正被别的模板当作页眉用的结构模板会一路成功，
// 绑定静默丢失 —— 表现为「站点的页眉在某次重建后没了」，只在构建日志里留一行 Warn。
//
// 判据：被引用 → 拒绝删除，且错误里给出**可定位数据**（哪套模板、哪个槽位）；
// 未引用 → 正常删除（不能因为加了拦截就把正常的删除也堵住）。

import (
	"context"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

func TestStructureTemplateDeleteGuard(t *testing.T) {
	svc, _, projectID := newService(t)
	if svc == nil {
		return
	}
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
		if !strings.Contains(err.Error(), "文章模板") || !strings.Contains(err.Error(), "页眉") {
			t.Fatalf("错误应给出可定位数据（模板名 + 槽位）: %v", err)
		}
		// 拒绝删除必须真的没删：模板仍在。
		if _, err := svc.Get(ctx, &contenttemplatedto.GetReq{ID: header.ID}); err != nil {
			t.Fatalf("删除被拒绝后模板应仍然存在: %v", err)
		}

		// 解绑之后可以正常删除（拦截不能把正常路径也堵住）。
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
		if !strings.Contains(err.Error(), "announcement") {
			t.Fatalf("错误应指出是哪个槽位: %v", err)
		}
	})

	t.Run("未被引用时可删除", func(t *testing.T) {
		header := newHeader(t, "没人用的页眉")
		if err := svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID}); err != nil {
			t.Fatalf("未被引用的结构模板应可删除: %v", err)
		}
	})
}

// mustArticleID 取本工程里第一套 article 模板的 id（用例内只有一套；List 内部按唯一工程解析）。
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
