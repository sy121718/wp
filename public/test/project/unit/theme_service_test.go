package unit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	projectdto "go_wp/internal/module/project/dto"
	projectservice "go_wp/internal/module/project/service"
)

func newProjectID() string { return uuid.NewString() }

func mustCreateTheme(t *testing.T, svc *projectservice.Service, projectID, name string, settings json.RawMessage) *projectdto.ThemeResp {
	t.Helper()
	res, err := svc.CreateTheme(context.Background(), &projectdto.ThemeCreateReq{ProjectID: projectID, Name: name, Settings: settings})
	if err != nil {
		t.Fatalf("预置主题 %q 失败: %v", name, err)
	}
	return res
}

// TestThemeCreateEdge CreateTheme：nil/空名/空工程/同名/首个激活/settings 兜底。
func TestThemeCreateEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()
	pid := newProjectID()

	t.Run("Nil请求", func(t *testing.T) {
		_, err := svc.CreateTheme(ctx, nil)
		if !errors.Is(err, projectservice.ErrThemeNameRequired) {
			t.Fatalf("应返回 ErrThemeNameRequired，实际: %v", err)
		}
	})

	t.Run("空名", func(t *testing.T) {
		for _, name := range []string{"", "   "} {
			_, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: pid, Name: name})
			if !errors.Is(err, projectservice.ErrThemeNameRequired) {
				t.Fatalf("空名 %q 应返回 ErrThemeNameRequired，实际: %v", name, err)
			}
		}
	})

	t.Run("空ProjectID缺校验", func(t *testing.T) {
		// 观察：service 层未校验 ProjectID 必填；"" 直接落到 uuid 列报 PG 原始错误。
		_, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: "", Name: "孤儿主题"})
		if err == nil {
			t.Fatalf("空 ProjectID 落 uuid 列应报错（校验缺失，错误来自 DB 层）")
		}
		t.Logf("空 ProjectID 报错来源为 DB 层: %v", err)
	})

	t.Run("同名拒绝大小写不敏感", func(t *testing.T) {
		mustCreateTheme(t, svc, pid, "Default", json.RawMessage(`{}`))
		for _, dup := range []string{"default", "DEFAULT", "  Default  "} {
			_, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: pid, Name: dup})
			if err == nil || !strings.Contains(err.Error(), "同名主题已存在") {
				t.Fatalf("同名 %q 应被拒绝，实际: %v", dup, err)
			}
		}
	})

	t.Run("首个自动激活", func(t *testing.T) {
		// 使用新工程，首个主题自动激活。
		pid2 := newProjectID()
		first := mustCreateTheme(t, svc, pid2, "T1", nil)
		if !first.IsActive {
			t.Fatalf("工程首个主题应自动激活: %+v", first)
		}
		second := mustCreateTheme(t, svc, pid2, "T2", nil)
		if second.IsActive {
			t.Fatalf("非首个主题不应激活: %+v", second)
		}
	})

	t.Run("Settings空兜底", func(t *testing.T) {
		for _, raw := range []json.RawMessage{nil, json.RawMessage("")} {
			res := mustCreateTheme(t, svc, newProjectID(), "兜底主题", raw)
			if string(res.Settings) != "{}" {
				t.Fatalf("空 settings 应兜底 {}，实际: %s", string(res.Settings))
			}
		}
	})

	t.Run("Settings为null字面量不兜底", func(t *testing.T) {
		// 观察：兜底仅判断 len==0；json 字面量 "null" 原样入库（非 JSON 对象）。
		res := mustCreateTheme(t, svc, newProjectID(), "null设置", json.RawMessage("null"))
		assertJSONEqual(t, res.Settings, `null`)
	})

	t.Run("Settings非法JSON返回DB错误", func(t *testing.T) {
		// 观察：CreateTheme 无 settings JSON 校验，非法 JSON 落到 jsonb 列，
		// 返回 PG 原始错误而非业务错误。
		_, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: newProjectID(), Name: "坏设置", Settings: json.RawMessage(`{bad`)})
		if err == nil {
			t.Fatalf("非法 JSON 应被拒绝")
		}
		t.Logf("非法 JSON 报错来源为 DB 层: %v", err)
	})
}

// TestThemeUpdateEdge UpdateTheme：空 ID/不存在/空字段保留/正常更新/settings 未校验。
func TestThemeUpdateEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()
	pid := newProjectID()
	th := mustCreateTheme(t, svc, pid, "原始主题", json.RawMessage(`{"color":"red"}`))

	t.Run("Nil请求", func(t *testing.T) {
		_, err := svc.UpdateTheme(ctx, nil)
		if !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("应返回 ErrThemeNotFound，实际: %v", err)
		}
	})

	t.Run("空ID", func(t *testing.T) {
		_, err := svc.UpdateTheme(ctx, &projectdto.ThemeUpdateReq{ID: "  ", Name: "新名"})
		if !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("应返回 ErrThemeNotFound，实际: %v", err)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		_, err := svc.UpdateTheme(ctx, &projectdto.ThemeUpdateReq{ID: uuid.NewString(), Name: "新名"})
		if !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("应返回 ErrThemeNotFound，实际: %v", err)
		}
	})

	t.Run("空Name保留原名", func(t *testing.T) {
		res, err := svc.UpdateTheme(ctx, &projectdto.ThemeUpdateReq{ID: th.ID, Name: "   "})
		if err != nil {
			t.Fatalf("空 name 更新失败: %v", err)
		}
		if res.Name != "原始主题" {
			t.Fatalf("空 name 应保留原名，实际: %q", res.Name)
		}
	})

	t.Run("空Settings保留原设置", func(t *testing.T) {
		res, err := svc.UpdateTheme(ctx, &projectdto.ThemeUpdateReq{ID: th.ID, Name: "改名"})
		if err != nil {
			t.Fatalf("空 settings 更新失败: %v", err)
		}
		assertJSONEqual(t, res.Settings, `{"color":"red"}`)
	})

	t.Run("正常更新", func(t *testing.T) {
		res, err := svc.UpdateTheme(ctx, &projectdto.ThemeUpdateReq{
			ID: th.ID, Name: "新主题", Settings: json.RawMessage(`{"color":"blue"}`),
		})
		if err != nil {
			t.Fatalf("正常更新失败: %v", err)
		}
		if res.Name != "新主题" {
			t.Fatalf("更新名称错误: %+v", res)
		}
		assertJSONEqual(t, res.Settings, `{"color":"blue"}`)
		got, err := svc.GetTheme(ctx, th.ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Name != "新主题" {
			t.Fatalf("DB 中名称不一致: %+v", got)
		}
		assertJSONEqual(t, got.Settings, `{"color":"blue"}`)
	})

	t.Run("Settings数组未校验", func(t *testing.T) {
		// 观察：UpdateTheme 无 settings 对象校验，[] 可原样写入 jsonb。
		res, err := svc.UpdateTheme(ctx, &projectdto.ThemeUpdateReq{ID: th.ID, Settings: json.RawMessage(`[]`)})
		if err != nil {
			t.Fatalf("[] 竟被拒绝（观察点：当前未校验）: %v", err)
		}
		assertJSONEqual(t, res.Settings, `[]`)
	})
}

// TestThemeActivateEdge ActivateTheme：空 ID/不存在/多主题轮换。
func TestThemeActivateEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()

	t.Run("空ID", func(t *testing.T) {
		err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: "  "})
		if !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("应返回 ErrThemeNotFound，实际: %v", err)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: uuid.NewString()})
		if !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("应返回 ErrThemeNotFound，实际: %v", err)
		}
	})

	t.Run("多主题轮换", func(t *testing.T) {
		pid := newProjectID()
		t1 := mustCreateTheme(t, svc, pid, "T1", nil)
		t2 := mustCreateTheme(t, svc, pid, "T2", nil)
		t3 := mustCreateTheme(t, svc, pid, "T3", nil)

		assertActive := func(wantID string) {
			t.Helper()
			themes, err := svc.ListThemes(ctx, pid)
			if err != nil {
				t.Fatalf("ListThemes 失败: %v", err)
			}
			activeCount := 0
			for _, th := range themes {
				if th.IsActive {
					activeCount++
					if th.ID != wantID {
						t.Fatalf("激活主题应为 %s，实际 %s", wantID, th.ID)
					}
				}
			}
			if activeCount != 1 {
				t.Fatalf("同一工程应恰有 1 个激活主题，实际 %d 个", activeCount)
			}
		}

		// 初始：首个自动激活。
		assertActive(t1.ID)
		// 激活 T2 → T1/T3 取消激活。
		if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: t2.ID}); err != nil {
			t.Fatalf("激活 T2 失败: %v", err)
		}
		assertActive(t2.ID)
		// 再激活 T3 → 轮换到 T3。
		if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: t3.ID}); err != nil {
			t.Fatalf("激活 T3 失败: %v", err)
		}
		assertActive(t3.ID)
		// 重复激活同一主题应幂等。
		if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: t3.ID}); err != nil {
			t.Fatalf("重复激活应幂等: %v", err)
		}
		assertActive(t3.ID)
	})
}

// TestThemeDeleteEdge DeleteTheme：空 ID/不存在/激活态拒绝/非激活可删。
func TestThemeDeleteEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()

	t.Run("空ID", func(t *testing.T) {
		err := svc.DeleteTheme(ctx, "  ")
		if !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("应返回 ErrThemeNotFound，实际: %v", err)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		err := svc.DeleteTheme(ctx, uuid.NewString())
		if !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("应返回 ErrThemeNotFound，实际: %v", err)
		}
	})

	t.Run("激活态拒绝", func(t *testing.T) {
		pid := newProjectID()
		first := mustCreateTheme(t, svc, pid, "激活主题", nil) // 首个自动激活
		err := svc.DeleteTheme(ctx, first.ID)
		if !errors.Is(err, projectservice.ErrThemeIsActive) {
			t.Fatalf("激活主题应拒绝删除，实际: %v", err)
		}
	})

	t.Run("非激活可删", func(t *testing.T) {
		pid := newProjectID()
		active := mustCreateTheme(t, svc, pid, "A", nil)
		dead := mustCreateTheme(t, svc, pid, "B", nil)
		if err := svc.DeleteTheme(ctx, dead.ID); err != nil {
			t.Fatalf("非激活主题应可删除: %v", err)
		}
		if _, err := svc.GetTheme(ctx, dead.ID); !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("删除后应查不到，实际: %v", err)
		}
		// 激活主题不受影响。
		got, err := svc.GetTheme(ctx, active.ID)
		if err != nil || !got.IsActive {
			t.Fatalf("删除非激活主题不应影响激活主题: %+v err=%v", got, err)
		}
	})
}

// TestThemeGetEdge GetTheme：存在/不存在。
func TestThemeGetEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()
	pid := newProjectID()
	th := mustCreateTheme(t, svc, pid, "主题A", json.RawMessage(`{"font":"sans"}`))

	t.Run("存在", func(t *testing.T) {
		got, err := svc.GetTheme(ctx, th.ID)
		if err != nil {
			t.Fatalf("GetTheme 应成功: %v", err)
		}
		if got.ID != th.ID || got.Name != "主题A" || got.ProjectID != pid {
			t.Fatalf("GetTheme 返回不一致: %+v", got)
		}
		assertJSONEqual(t, got.Settings, `{"font":"sans"}`)
	})

	t.Run("不存在", func(t *testing.T) {
		_, err := svc.GetTheme(ctx, uuid.NewString())
		if !errors.Is(err, projectservice.ErrThemeNotFound) {
			t.Fatalf("应返回 ErrThemeNotFound，实际: %v", err)
		}
	})
}

// TestThemeGetActiveThemeEdge GetActiveTheme：无主题/单主题/轮换后。
func TestThemeGetActiveThemeEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()

	t.Run("无主题返回原始错误", func(t *testing.T) {
		// 观察：GetActiveTheme 无主题时原样返回 gorm.ErrRecordNotFound，
		// 未映射为业务错误（与 GetTheme 的 ErrThemeNotFound 不一致）。
		_, err := svc.GetActiveTheme(ctx, newProjectID())
		if err == nil {
			t.Fatalf("无主题应报错")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("观察点：返回 gorm.ErrRecordNotFound，实际: %v", err)
		}
	})

	t.Run("单主题返回激活主题", func(t *testing.T) {
		pid := newProjectID()
		th := mustCreateTheme(t, svc, pid, "唯一", nil)
		got, err := svc.GetActiveTheme(ctx, pid)
		if err != nil {
			t.Fatalf("GetActiveTheme 应成功: %v", err)
		}
		if got.ID != th.ID || !got.IsActive {
			t.Fatalf("应返回激活主题: %+v", got)
		}
	})

	t.Run("轮换后返回新激活主题", func(t *testing.T) {
		pid := newProjectID()
		t1 := mustCreateTheme(t, svc, pid, "T1", nil)
		t2 := mustCreateTheme(t, svc, pid, "T2", nil)
		if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: t2.ID}); err != nil {
			t.Fatalf("激活 T2 失败: %v", err)
		}
		got, err := svc.GetActiveTheme(ctx, pid)
		if err != nil {
			t.Fatalf("GetActiveTheme 应成功: %v", err)
		}
		if got.ID != t2.ID {
			t.Fatalf("应返回新激活主题 T2，实际 %s", got.ID)
		}
		_ = t1
	})
}

// TestThemeListEdge ListThemes 激活在前排序。
func TestThemeListEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()
	pid := newProjectID()
	t1 := mustCreateTheme(t, svc, pid, "T1", nil)
	t2 := mustCreateTheme(t, svc, pid, "T2", nil)
	t3 := mustCreateTheme(t, svc, pid, "T3", nil)

	t.Run("初始首个激活在前", func(t *testing.T) {
		themes, err := svc.ListThemes(ctx, pid)
		if err != nil {
			t.Fatalf("ListThemes 失败: %v", err)
		}
		if len(themes) != 3 {
			t.Fatalf("应返回 3 个主题，实际 %d", len(themes))
		}
		if !themes[0].IsActive || themes[0].ID != t1.ID {
			t.Fatalf("激活主题应排最前: %+v", themes[0])
		}
	})

	t.Run("激活轮换后激活主题仍在前", func(t *testing.T) {
		if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: t3.ID}); err != nil {
			t.Fatalf("激活 T3 失败: %v", err)
		}
		themes, err := svc.ListThemes(ctx, pid)
		if err != nil {
			t.Fatalf("ListThemes 失败: %v", err)
		}
		if themes[0].ID != t3.ID || !themes[0].IsActive {
			t.Fatalf("激活 T3 后应排最前: %+v", themes[0])
		}
		for _, th := range themes[1:] {
			if th.IsActive {
				t.Fatalf("除首个外不应有激活主题: %+v", th)
			}
		}
		_ = t2
	})

	t.Run("空工程返回空列表", func(t *testing.T) {
		themes, err := svc.ListThemes(ctx, newProjectID())
		if err != nil {
			t.Fatalf("ListThemes 失败: %v", err)
		}
		if themes == nil || len(themes) != 0 {
			t.Fatalf("空工程应返回空列表，实际: %v", themes)
		}
	})
}

// TestThemeListByBlockIDEdge ListThemesByBlockID：headerBlockId/footerBlockId 匹配。
func TestThemeListByBlockIDEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()
	pid := newProjectID()
	mustCreateTheme(t, svc, pid, "页眉主题", json.RawMessage(`{"headerBlockId":"blk-header"}`))
	mustCreateTheme(t, svc, pid, "页脚主题", json.RawMessage(`{"footerBlockId":"blk-footer"}`))
	mustCreateTheme(t, svc, pid, "无绑定", json.RawMessage(`{"color":"red"}`))
	both := mustCreateTheme(t, svc, pid, "双绑定", json.RawMessage(`{"headerBlockId":"blk-header","footerBlockId":"blk-footer"}`))

	t.Run("按页眉匹配", func(t *testing.T) {
		themes, err := svc.ListThemesByBlockID(ctx, "blk-header")
		if err != nil {
			t.Fatalf("ListThemesByBlockID 失败: %v", err)
		}
		if len(themes) != 2 {
			t.Fatalf("应匹配 2 个页眉主题，实际 %d", len(themes))
		}
		ids := map[string]bool{}
		for _, th := range themes {
			ids[th.ID] = true
		}
		if !ids[both.ID] {
			t.Fatalf("双绑定主题应出现在页眉匹配中: %v", ids)
		}
	})

	t.Run("按页脚匹配", func(t *testing.T) {
		themes, err := svc.ListThemesByBlockID(ctx, "blk-footer")
		if err != nil {
			t.Fatalf("ListThemesByBlockID 失败: %v", err)
		}
		if len(themes) != 2 {
			t.Fatalf("应匹配 2 个页脚主题，实际 %d", len(themes))
		}
	})

	t.Run("无匹配返回空", func(t *testing.T) {
		themes, err := svc.ListThemesByBlockID(ctx, "blk-none")
		if err != nil {
			t.Fatalf("ListThemesByBlockID 失败: %v", err)
		}
		if themes == nil || len(themes) != 0 {
			t.Fatalf("无匹配应返回空列表，实际: %v", themes)
		}
	})

	t.Run("空ID返回空", func(t *testing.T) {
		themes, err := svc.ListThemesByBlockID(ctx, "")
		if err != nil {
			t.Fatalf("ListThemesByBlockID 失败: %v", err)
		}
		if len(themes) != 0 {
			t.Fatalf("空 blockID 应无匹配，实际 %d", len(themes))
		}
	})
}
