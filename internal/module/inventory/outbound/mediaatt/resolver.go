// Package mediaatt 把 media 模块的附件详情适配成库存域要的窄读口（迁移 627，W3）。
//
// 为什么需要这一层：库存的调拨凭证要校验「这个附件 id 存不存在、它长什么样」，
// 而 media 的 MediaService 有二十来个方法（上传 / 列表 / 删除 / 换图 / 分类树 /
// 引用同步…）—— 库存一条都用不上。契约侧只声明 ResolveAttachment(s) 两个方法，
// media 侧不需要知道自己被库存用了。
//
// 依赖方向：本包 → inventorycontract（调用方契约）+ media 的 dto（只读形状）。
// 与 orderstock 同一手法：实现方依赖调用方契约，调用方（inventory service）不认识 media。
package mediaatt

import (
	"context"
	"errors"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	mediacontract "go_wp/internal/module/media/contract"
	mediadto "go_wp/internal/module/media/dto"
)

// Resolver 附件读口的 media 实现。
type Resolver struct {
	svc mediacontract.MediaService
}

// 编译期断言：适配器必须满足库存侧的附件读口契约。
var _ inventorycontract.AttachmentResolver = (*Resolver)(nil)

// New 构造；svc 为 media 契约（只用 Detail 一条能力）。
//
// 返回具体类型而不是接口：装配方拿到它直接传给库存模块，不需要类型断言；
// 也刻意不做「svc 为空返回 nil」—— 那会在赋给接口时变成 typed nil（接口非空、调用才炸）。
func New(svc mediacontract.MediaService) *Resolver { return &Resolver{svc: svc} }

// ResolveAttachment 取单个附件；不存在时返回 ErrAttachmentNotFound。
func (r *Resolver) ResolveAttachment(ctx context.Context, projectID string, id int64) (*inventorycontract.AttachmentBrief, error) {
	one, err := r.resolveOne(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	if one == nil {
		return nil, errors.New(inventorycontract.ErrAttachmentNotFound)
	}
	return one, nil
}

// ResolveAttachments 批量取；顺序与入参一致，缺项返回 nil 占位。
//
// 顺序必须与入参一致：调用方按这个顺序写「凭证的排序」（sort 列），
// 乱序会让运营在页面上看到的凭证顺序与他们在抽屉里选的顺序不一样。
func (r *Resolver) ResolveAttachments(ctx context.Context, projectID string, ids []int64) ([]*inventorycontract.AttachmentBrief, error) {
	out := make([]*inventorycontract.AttachmentBrief, 0, len(ids))
	for _, id := range ids {
		one, err := r.resolveOne(ctx, projectID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, one) // nil 占位
	}
	return out, nil
}

// resolveOne 取一个附件；查不到返回 (nil, nil)（由调用方决定是占位还是报错）。
func (r *Resolver) resolveOne(ctx context.Context, projectID string, id int64) (*inventorycontract.AttachmentBrief, error) {
	if r.svc == nil || id <= 0 {
		return nil, nil
	}
	got, err := r.svc.Detail(ctx, &mediadto.DetailReq{ProjectID: projectID, ID: uint64(id)})
	if err != nil {
		// media 的「不存在」也是业务错误，但库存不需要区分（都是「这张凭证没有」）——
		// 只要不是基础设施故障就归到「查不到」。
		if isBusinessNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if got == nil || got.ID == 0 {
		return nil, nil
	}
	return &inventorycontract.AttachmentBrief{
		ID:       int64(got.ID),
		FileName: got.FileName,
		FileSize: got.FileSize,
		MimeType: got.MimeType,
		URL:      got.URL,
	}, nil
}

// isBusinessNotFound 判「附件不存在」这类业务错误（media 用 facing 的形态）。
//
// 用错误文本里的 key 判（与 order 侧 mapStockError 同一手法）：跨模块不 import 对方的
// enums（module_boundary_test.go 的反向依赖判据），两边 key 的常量值就是约定的锚点。
func isBusinessNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, key := range []string{"erratachmentnotfound", "errmmedianotfound", "not found"} {
		if strings.Contains(msg, key) {
			return true
		}
	}
	return false
}
