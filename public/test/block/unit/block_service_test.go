// Package unit 覆盖 block 模块 service 层业务用例（公共测试支撑 + 真实 PG 隔离 schema）。
package unit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/public/test/support"
)

// env 一次测试的隔离环境：专属 schema + 真实 project 服务（共享同一 db）。
type env struct {
	t         *testing.T
	db        *gorm.DB
	svc       *blockservice.Service
	projects  *projectservice.Service
	projectID string
}

// newEnv 创建独立 PG schema，迁移 block/project 表并装配真实服务。
// PG 不可用时 t.Skip（正常行为，不算失败）。
func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
	}
	if err := db.AutoMigrate(&blockmodel.BlockEntity{}, &projectmodel.ProjectEntity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pid, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	return &env{t: t, db: db, svc: svc, projects: projects, projectID: pid.ID}
}

// newProject 额外创建一个工程（用于跨工程隔离验证）。
func (e *env) newProject(t *testing.T) string {
	t.Helper()
	p, err := e.projects.Create(context.Background(), &projectdto.CreateReq{Name: "另一工程"})
	if err != nil {
		t.Fatalf("创建辅助工程失败: %v", err)
	}
	return p.ID
}

// createBlock 通过 service.Create 创建块（name/kind 走原始输入，文档为空）。
func (e *env) createBlock(t *testing.T, name, kind string) *blockdto.BlockResp {
	t.Helper()
	res, err := e.svc.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: e.projectID, Name: name, Kind: kind,
	})
	if err != nil {
		t.Fatalf("创建块失败 name=%q kind=%q: %v", name, kind, err)
	}
	return res
}

// errContains 断言错误非 nil 且消息包含子串。
func errContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误包含 %q，实际无错误", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("期望错误包含 %q，实际: %v", want, err)
	}
}

// docLayoutMode 提取规范化文档的 settings.layout.mode（语义断言，避免字节差异）。
func docLayoutMode(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	if !json.Valid(raw) {
		t.Fatalf("文档非法 JSON: %s", string(raw))
	}
	var v struct {
		Settings struct {
			Layout struct {
				Mode string `json:"mode"`
			} `json:"layout"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("文档无法反序列化: %v", err)
	}
	return v.Settings.Layout.Mode
}

// ---- List ----

func TestBlockListSuccess(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("EmptyProjectReturnsEmptyList", func(t *testing.T) {
		pid := e.newProject(t)
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: pid})
		if err != nil {
			t.Fatalf("空工程列表应无错误: %v", err)
		}
		if res == nil || len(res) != 0 {
			t.Fatalf("空列表应为非 nil 空切片，实际: %#v", res)
		}
	})

	t.Run("KindFilter", func(t *testing.T) {
		e.createBlock(t, "页眉", blockmodel.KindHeader)
		e.createBlock(t, "普通", blockmodel.KindBlock)
		e.createBlock(t, "页脚", blockmodel.KindFooter)
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID, Kind: blockmodel.KindHeader})
		if err != nil {
			t.Fatalf("kind 过滤应无错误: %v", err)
		}
		if len(res) != 1 || res[0].Kind != blockmodel.KindHeader || res[0].Name != "页眉" {
			t.Fatalf("kind=header 应只返回页眉块: %#v", res)
		}
	})

	t.Run("KindFilterNoMatch", func(t *testing.T) {
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID, Kind: "hero"})
		if err != nil {
			t.Fatalf("无匹配 kind 应无错误: %v", err)
		}
		if len(res) != 0 {
			t.Fatalf("无匹配时应返回空列表: %#v", res)
		}
	})

	t.Run("WhitespaceKindTrimmed", func(t *testing.T) {
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID, Kind: "  header  "})
		if err != nil {
			t.Fatalf("kind 带空白应被 TrimSpace: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("kind 空白应等价于 header，实际数量 %d: %#v", len(res), res)
		}
	})

	t.Run("WithoutKindListsAllOrderedByKind", func(t *testing.T) {
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID})
		if err != nil {
			t.Fatalf("全量列表应无错误: %v", err)
		}
		// 此时应含普通/页眉/页脚各一（前序子测试创建），按 kind ASC 排序：block < footer < header。
		if len(res) != 3 {
			t.Fatalf("应返回 3 个块: %#v", res)
		}
		if res[0].Kind != blockmodel.KindBlock || res[1].Kind != blockmodel.KindFooter || res[2].Kind != blockmodel.KindHeader {
			t.Fatalf("kind 应按字母序 block/footer/header 排列: %#v", res)
		}
	})
}

func TestBlockListInvalidRequest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// 修复语义：nil/空 projectID 是参数错误（ErrBlockParamRequired），
	// 与「工程下无块」（正常空列表）及资源不存在（ErrBlockNotFound）区分开。
	t.Run("NilRequest", func(t *testing.T) {
		_, err := e.svc.List(ctx, nil)
		errContains(t, err, blockenums.ErrBlockParamRequired)
	})
	t.Run("EmptyProjectID", func(t *testing.T) {
		_, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: ""})
		errContains(t, err, blockenums.ErrBlockParamRequired)
	})
	t.Run("WhitespaceProjectID", func(t *testing.T) {
		_, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: "   "})
		errContains(t, err, blockenums.ErrBlockParamRequired)
	})
}

// ---- Detail ----

func TestBlockDetailSuccess(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	created := e.createBlock(t, "页眉块", blockmodel.KindHeader)

	res, err := e.svc.Detail(ctx, &blockdto.DetailReq{ID: created.ID})
	if err != nil {
		t.Fatalf("Detail 应无错误: %v", err)
	}
	if res.ID != created.ID || res.ProjectID != e.projectID || res.Name != "页眉块" || res.Kind != blockmodel.KindHeader {
		t.Fatalf("Detail 字段不一致: %#v", res)
	}
	if res.CreatedAt.IsZero() || res.UpdatedAt.IsZero() {
		t.Fatalf("时间戳不应为零值: %#v", res)
	}
}

func TestBlockDetailInvalidRequest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// 修复语义：nil/空 ID 是参数错误（ErrBlockParamRequired），
	// 与「ID 对应块不存在」（ErrBlockNotFound，见 TestBlockDetailNotFound）区分开。
	t.Run("NilRequest", func(t *testing.T) {
		_, err := e.svc.Detail(ctx, nil)
		errContains(t, err, blockenums.ErrBlockParamRequired)
	})
	t.Run("EmptyID", func(t *testing.T) {
		_, err := e.svc.Detail(ctx, &blockdto.DetailReq{ID: ""})
		errContains(t, err, blockenums.ErrBlockParamRequired)
	})
	t.Run("WhitespaceID", func(t *testing.T) {
		_, err := e.svc.Detail(ctx, &blockdto.DetailReq{ID: "  "})
		errContains(t, err, blockenums.ErrBlockParamRequired)
	})
}

func TestBlockDetailNotFound(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.svc.Detail(ctx, &blockdto.DetailReq{ID: uuid.NewString()})
	errContains(t, err, blockenums.ErrBlockNotFound)
}

// ---- Create ----

func TestBlockCreateSuccess(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("DefaultDocumentAndKind", func(t *testing.T) {
		res := e.createBlock(t, "  导航块  ", "")
		if res.ID == "" {
			t.Fatal("创建后 ID 不应为空")
		}
		if res.Name != "导航块" {
			t.Fatalf("名称应 TrimSpace，实际: %q", res.Name)
		}
		if res.Kind != blockmodel.KindBlock {
			t.Fatalf("空 kind 应默认 block，实际: %q", res.Kind)
		}
		if got := docLayoutMode(t, res.Document); got != "full" {
			t.Fatalf("默认文档 layout.mode 应为 full，实际: %q", got)
		}
	})

	t.Run("ExplicitDocumentNormalized", func(t *testing.T) {
		res, err := e.svc.Create(ctx, &blockdto.CreateReq{
			ProjectID: e.projectID, Name: "显式文档",
			Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}}]}`),
		})
		if err != nil {
			t.Fatalf("显式文档创建应成功: %v", err)
		}
		if got := docLayoutMode(t, res.Document); got != "full" {
			t.Fatalf("文档 layout.mode 应为 full: %q", got)
		}
		var page struct {
			Root []struct {
				ID string `json:"id"`
			} `json:"root"`
		}
		if err := json.Unmarshal(res.Document, &page); err != nil || len(page.Root) != 1 || page.Root[0].ID != "n1" {
			t.Fatalf("文档 root 节点丢失: %s", string(res.Document))
		}
	})

	t.Run("HeaderFooterKinds", func(t *testing.T) {
		h := e.createBlock(t, "页眉候选", blockmodel.KindHeader)
		f := e.createBlock(t, "页脚候选", blockmodel.KindFooter)
		if h.Kind != blockmodel.KindHeader || f.Kind != blockmodel.KindFooter {
			t.Fatalf("kind 未按输入保存: %#v %#v", h, f)
		}
	})
}

func TestBlockCreateInvalidRequest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("NilRequest", func(t *testing.T) {
		_, err := e.svc.Create(ctx, nil)
		errContains(t, err, blockenums.ErrBlockNameRequired)
	})
	t.Run("EmptyName", func(t *testing.T) {
		_, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: ""})
		errContains(t, err, blockenums.ErrBlockNameRequired)
	})
	t.Run("WhitespaceName", func(t *testing.T) {
		_, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "   "})
		errContains(t, err, blockenums.ErrBlockNameRequired)
	})
	t.Run("ProjectNotExist", func(t *testing.T) {
		_, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: uuid.NewString(), Name: "孤儿块"})
		errContains(t, err, "工程不存在")
	})
	t.Run("EmptyProjectID", func(t *testing.T) {
		_, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: "", Name: "无归属块"})
		errContains(t, err, "工程不存在")
	})
}

func TestBlockCreateDuplicateName(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.createBlock(t, "Hero", "")

	t.Run("ExactDuplicateRejected", func(t *testing.T) {
		_, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "Hero"})
		errContains(t, err, "同名块已存在")
	})
	t.Run("CaseAndWhitespaceInsensitive", func(t *testing.T) {
		_, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "  hero  "})
		errContains(t, err, "同名块已存在")
	})
	t.Run("SameNameOtherProjectAllowed", func(t *testing.T) {
		pid := e.newProject(t)
		res, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: pid, Name: "Hero"})
		if err != nil {
			t.Fatalf("跨工程同名应允许: %v", err)
		}
		if res.Name != "Hero" || res.ProjectID != pid {
			t.Fatalf("跨工程创建结果错误: %#v", res)
		}
	})
}

// ---- Update ----

func TestBlockUpdateSuccess(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("FullUpdate", func(t *testing.T) {
		created := e.createBlock(t, "旧名", "")
		res, err := e.svc.Update(ctx, &blockdto.UpdateReq{
			ID: created.ID, Name: "新名", Kind: blockmodel.KindFooter,
			Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`),
		})
		if err != nil {
			t.Fatalf("全量更新应成功: %v", err)
		}
		if res.Name != "新名" || res.Kind != blockmodel.KindFooter {
			t.Fatalf("更新结果错误: %#v", res)
		}
		// 持久化验证：重新 Detail。
		got, err := e.svc.Detail(ctx, &blockdto.DetailReq{ID: created.ID})
		if err != nil || got.Name != "新名" || got.Kind != blockmodel.KindFooter {
			t.Fatalf("更新未持久化: %#v err=%v", got, err)
		}
	})

	t.Run("PartialNameOnly", func(t *testing.T) {
		created := e.createBlock(t, "原名称", blockmodel.KindHeader)
		res, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: created.ID, Name: "  新名称  "})
		if err != nil {
			t.Fatalf("部分更新应成功: %v", err)
		}
		if res.Name != "新名称" || res.Kind != blockmodel.KindHeader {
			t.Fatalf("只改名称时 kind 应保留: %#v", res)
		}
	})

	t.Run("PartialKindOnly", func(t *testing.T) {
		created := e.createBlock(t, "类型块", blockmodel.KindBlock)
		res, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: created.ID, Kind: " header "})
		if err != nil {
			t.Fatalf("只改 kind 应成功: %v", err)
		}
		if res.Kind != blockmodel.KindHeader || res.Name != "类型块" {
			t.Fatalf("只改 kind 时 name 应保留: %#v", res)
		}
	})

	t.Run("PartialDocumentOnly", func(t *testing.T) {
		created := e.createBlock(t, "文档块", "")
		res, err := e.svc.Update(ctx, &blockdto.UpdateReq{
			ID:       created.ID,
			Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}}]}`),
		})
		if err != nil {
			t.Fatalf("只改文档应成功: %v", err)
		}
		if res.Name != "文档块" {
			t.Fatalf("只改文档时 name 应保留: %#v", res)
		}
		var page struct {
			Root []struct {
				ID string `json:"id"`
			} `json:"root"`
		}
		if err := json.Unmarshal(res.Document, &page); err != nil || len(page.Root) != 1 || page.Root[0].ID != "n1" {
			t.Fatalf("文档未更新: %s", string(res.Document))
		}
	})

	t.Run("BoxedWithoutMaxWidthRejected", func(t *testing.T) {
		created := e.createBlock(t, "版心块", "")
		_, err := e.svc.Update(ctx, &blockdto.UpdateReq{
			ID:       created.ID,
			Document: json.RawMessage(`{"settings":{"layout":{"mode":"boxed"}},"root":[]}`),
		})
		errContains(t, err, blockenums.ErrBlockInvalidDoc)
		// 失败后数据保持不变。
		got, _ := e.svc.Detail(ctx, &blockdto.DetailReq{ID: created.ID})
		if got.Name != "版心块" || docLayoutMode(t, got.Document) != "full" {
			t.Fatalf("非法文档更新失败后数据不应变化: %#v", got)
		}
	})

	t.Run("EmptyNameKeepsExisting", func(t *testing.T) {
		created := e.createBlock(t, "保留名", "")
		res, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: created.ID, Name: "  "})
		if err != nil {
			t.Fatalf("空白名称应保留原名: %v", err)
		}
		if res.Name != "保留名" {
			t.Fatalf("空白名称不应覆盖: %#v", res)
		}
	})
}

func TestBlockUpdateInvalidRequest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("NilRequest", func(t *testing.T) {
		_, err := e.svc.Update(ctx, nil)
		errContains(t, err, blockenums.ErrBlockNotFound)
	})
	t.Run("EmptyID", func(t *testing.T) {
		_, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: ""})
		errContains(t, err, blockenums.ErrBlockNotFound)
	})
	t.Run("NotExist", func(t *testing.T) {
		_, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: uuid.NewString(), Name: "无名氏"})
		errContains(t, err, blockenums.ErrBlockNotFound)
	})
	t.Run("InvalidDocumentRejected", func(t *testing.T) {
		created := e.createBlock(t, "改文档", "")
		_, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: created.ID, Name: "不应生效", Document: json.RawMessage(`[]`)})
		errContains(t, err, blockenums.ErrBlockInvalidDoc)
		got, _ := e.svc.Detail(ctx, &blockdto.DetailReq{ID: created.ID})
		if got.Name != "改文档" {
			t.Fatalf("非法文档更新失败后名称不应变化: %#v", got)
		}
	})
}

// ---- Delete ----

func TestBlockDeleteSuccess(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	created := e.createBlock(t, "待删块", "")

	if err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: created.ID}); err != nil {
		t.Fatalf("删除应无错误: %v", err)
	}
	_, err := e.svc.Detail(ctx, &blockdto.DetailReq{ID: created.ID})
	errContains(t, err, blockenums.ErrBlockNotFound)

	res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID})
	if err != nil || len(res) != 0 {
		t.Fatalf("删除后列表应为空: %#v err=%v", res, err)
	}
}

func TestBlockDeleteNotExist(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// 修复语义：删除不存在的块应报 ErrBlockNotFound（与 Detail/Update 一致，
	// 此前 model.Delete RowsAffected=0 静默成功）。
	if err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: uuid.NewString()}); err == nil {
		t.Fatalf("删除不存在的块应报错")
	}
}

func TestBlockDeleteInvalidRequest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("NilRequest", func(t *testing.T) {
		errContains(t, e.svc.Delete(ctx, nil), blockenums.ErrBlockNotFound)
	})
	t.Run("EmptyID", func(t *testing.T) {
		errContains(t, e.svc.Delete(ctx, &blockdto.DeleteReq{ID: ""}), blockenums.ErrBlockNotFound)
	})
	t.Run("WhitespaceID", func(t *testing.T) {
		errContains(t, e.svc.Delete(ctx, &blockdto.DeleteReq{ID: "  "}), blockenums.ErrBlockNotFound)
	})
}
