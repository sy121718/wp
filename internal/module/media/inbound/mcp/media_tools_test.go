package mediamcp

// media_tools_test.go — 媒体工具集的形状与三态断言。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	mediadto "go_wp/internal/module/media/dto"
)

// fakeMedia 媒体的假实现。
type fakeMedia struct {
	detailReq *mediadto.DetailReq
	listReq   *mediadto.ListReq
	updated   []*mediadto.AttachmentUpdateReq
	deleted   []*mediadto.DeleteReq
}

func (f *fakeMedia) Detail(_ context.Context, req *mediadto.DetailReq) (*mediadto.AttachmentResp, error) {
	f.detailReq = req
	return &mediadto.AttachmentResp{
		ID: req.ID, FileName: "banner.webp", FileType: "image", MimeType: "image/webp",
		FileSize: 20480, URL: "https://cdn/banner.webp",
		ExtraInfo: `{"alt":"首页横幅","title":"2026 春季","refs":["page:1"]}`,
	}, nil
}

func (f *fakeMedia) List(_ context.Context, req *mediadto.ListReq) (*mediadto.ListResp, error) {
	f.listReq = req
	return &mediadto.ListResp{Total: 1, List: []mediadto.AttachmentResp{
		{ID: 7, FileName: "banner.webp", FileType: "image", ExtraInfo: `{"alt":"首页横幅"}`},
	}}, nil
}

func (f *fakeMedia) UpdateAttachment(_ context.Context, req *mediadto.AttachmentUpdateReq) error {
	f.updated = append(f.updated, req)
	return nil
}

func (f *fakeMedia) Delete(_ context.Context, req *mediadto.DeleteReq) error {
	f.deleted = append(f.deleted, req)
	return nil
}

func byName(t *testing.T, list []mcp.Tool, name string) mcp.Tool {
	t.Helper()
	for _, tl := range list {
		if tl.Name() == name {
			return tl
		}
	}
	t.Fatalf("工具集里没有 %s", name)
	return mcp.Tool{}
}

func call(t *testing.T, tool mcp.Tool, body string) mcp.Result {
	t.Helper()
	res, err := tool.Invoke(context.Background(), json.RawMessage(body))
	if err != nil {
		t.Fatalf("%s 调用失败：%v", tool.Name(), err)
	}
	return res
}

// TestMediaToolsHaveNoProjectArg 媒体工具**不得**收 projectId。
//
// sys_attachment 没有 project_id 列（媒体库站点级共享），这个参数既不参与查询
// 也不参与过滤。service 里有一段注释专门记着它曾经被当必填的后果：唯一效果是让
// 没带参数的调用方拿到一个 400，看起来像「附件不存在」。工具层重复这个错误更糟 ——
// 模型会把它当成「需要向用户索要的信息」而停下来问，而用户根本不知道答案。
func TestMediaToolsHaveNoProjectArg(t *testing.T) {
	f := &fakeMedia{}
	list, err := Tools(f, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("工具条数应为 4，实得 %d", len(list))
	}
	for _, name := range []string{"media_find", "media_get", "media_update", "media_delete"} {
		props := byName(t, list, name).Schema().Properties
		if _, ok := props["projectId"]; ok {
			t.Errorf("%s 不该收 projectId —— 它用不上，收了只会让模型向用户索要", name)
		}
	}
}

// TestMediaGetFlattensExtraInfo alt / 标题 / 描述必须出现在读取结果里。
//
// 它们藏在 extra_info 这一列 JSON 里；不展平的话模型「确认现状」之后看不到它们，
// 于是要么重写一遍同样的值，要么以为该清空。
func TestMediaGetFlattensExtraInfo(t *testing.T) {
	f := &fakeMedia{}
	list, _ := Tools(f, f, nil)
	res := call(t, byName(t, list, "media_get"), `{"id":7}`)
	for _, want := range []string{"首页横幅", "2026 春季", "banner.webp"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("读取结果里缺少 %q：\n%s", want, res.Text)
		}
	}
	// 同列里的别的键（refs）不该被当成可编辑字段平铺出来。
	if strings.Contains(res.Text, "refs") {
		t.Errorf("只该展平 alt/title/description，实得：\n%s", res.Text)
	}
	if f.detailReq == nil || f.detailReq.ID != 7 {
		t.Fatalf("id 没传下去：%+v", f.detailReq)
	}
}

// TestMediaFindReturnsIDAndAlt 搜索结果要能直接用来引用（id + 名称）。
func TestMediaFindReturnsIDAndAlt(t *testing.T) {
	f := &fakeMedia{}
	list, _ := Tools(f, f, nil)
	res := call(t, byName(t, list, "media_find"), `{"search":"banner"}`)
	if f.listReq == nil || f.listReq.Search != "banner" {
		t.Fatalf("关键词没传下去：%+v", f.listReq)
	}
	for _, want := range []string{"banner.webp", "id=7", "首页横幅"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("搜索结果里缺少 %q：\n%s", want, res.Text)
		}
	}
}

// TestMediaUpdateTriState 分类字段是三态：不传 / 0（移入未分类）/ 某个 id。
//
// 折成字符串（"" / "0" / "12"）会把前两者混成一个 —— 而「移入未分类」是
// 一个有意义的操作，混淆之后用户说「取消分类」会变成「不动分类」。
func TestMediaUpdateTriState(t *testing.T) {
	f := &fakeMedia{}
	list, _ := Tools(f, f, nil)
	tool := byName(t, list, "media_update")

	// ① 只改 alt：另外两个必须是 nil。
	call(t, tool, `{"id":7,"alt":"新 alt","confirm":true,"idempotencyKey":"k1"}`)
	got := f.updated[0]
	if got.Alt == nil || *got.Alt != "新 alt" {
		t.Fatalf("alt 没传对：%v", got.Alt)
	}
	if got.CategoryID != nil {
		t.Errorf("没传 categoryId 时该是 nil（不改），实得 %d", *got.CategoryID)
	}
	if got.Title != nil || got.FileName != nil {
		t.Error("没传的字段该保持 nil")
	}

	// ② 分类传 0：必须是「移入未分类」（非 nil 的 0），不是「不改」。
	call(t, tool, `{"id":7,"categoryId":0,"confirm":true,"idempotencyKey":"k2"}`)
	got2 := f.updated[1]
	if got2.CategoryID == nil {
		t.Fatal("传 0 应是「移入未分类」（非 nil），实得 nil")
	}
	if *got2.CategoryID != 0 {
		t.Fatalf("分类该是 0，实得 %d", *got2.CategoryID)
	}
}

// TestMediaDeleteNeedsConfirm 删除必须带确认位。
func TestMediaDeleteNeedsConfirm(t *testing.T) {
	f := &fakeMedia{}
	list, _ := Tools(f, f, nil)
	tool := byName(t, list, "media_delete")
	if _, err := tool.Invoke(context.Background(), json.RawMessage(
		`{"id":7,"idempotencyKey":"k3"}`)); err == nil {
		t.Fatal("缺 confirm 应被拒")
	}
	if len(f.deleted) != 0 {
		t.Fatal("被拒时不该执行删除")
	}
	call(t, tool, `{"id":7,"confirm":true,"idempotencyKey":"k3"}`)
	if len(f.deleted) != 1 || f.deleted[0].ID != 7 {
		t.Fatalf("删除参数不对：%+v", f.deleted)
	}
}

// TestMediaUpdateSaysFileCannotChange 说明里必须写清「改不了文件本身」。
func TestMediaUpdateSaysFileCannotChange(t *testing.T) {
	f := &fakeMedia{}
	list, _ := Tools(f, f, nil)
	if !strings.Contains(byName(t, list, "media_update").Description(), "改不了文件本身") {
		t.Fatal("不写清的话，模型会对着「换张图」的请求硬凑一个更新调用")
	}
}

// TestMediaToolsRejectMissingDeps 依赖缺失是装配期错误。
func TestMediaToolsRejectMissingDeps(t *testing.T) {
	f := &fakeMedia{}
	if _, err := Tools(nil, f, nil); err == nil {
		t.Error("读依赖缺失应报错")
	}
	if _, err := Tools(f, nil, nil); err == nil {
		t.Error("写依赖缺失应报错")
	}
}
