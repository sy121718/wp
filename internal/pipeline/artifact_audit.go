// artifact_audit.go —— 产物磁盘与数据库的双向对账（审计 IDX-015）。
//
// 为什么需要反向对账：正向巡检（AuditActiveLinks）能发现「访问面链到了不存在的文件」，
// 但反过来「磁盘上有一堆没人引用的产物目录」从来没人看 —— 它们占着磁盘，且因为
// 内容寻址（同一 hash 一份）很难靠人工判断哪个能删。
//
// 职责边界：本文件只认**磁盘**（artifacts 根目录下有哪些 hash），谁是这些 hash 的属主
// 由各模块自己回答（page_artifacts / presentation_artifacts 各自查自己的表）。
// 对账在这里合成 —— 避免 page 与 presentation 互相依赖，也避免谁替别人认领文件。
package pipeline

import (
	"context"
	"os"
	"path/filepath"
)

// OrphanArtifact 一个「磁盘上有、数据库里没人认领」的产物目录。
type OrphanArtifact struct {
	Hash string `json:"hash"`
	Path string `json:"path"`
	// Bytes 该目录下文件总字节数（运维判断清理收益用）。
	Bytes int64 `json:"bytes"`
	// Files 文件个数。
	Files int `json:"files"`
}

// OwnerProvider 返回某模块认领的全部产物 hash。允许为 nil（表示该模块无产物或未装配）。
//
// 导出是因为调用方在别的包（page service）构造这个切片 —— 对账需要把多个模块的
// 属主清单合起来看，谁认领哪些文件只有各模块自己知道。
type OwnerProvider func(ctx context.Context) ([]string, error)

// ListDiskArtifactHashes 列出磁盘上实际存在的产物目录 hash。
//
// 目录布局见 LocalStore：<Root>/artifacts/<hash>/；redirects 是另一类（重定向产物）
// 单独存放，不参与本对账（它的属主判定与普通产物不同）。
func ListDiskArtifactHashes(root string) (hashes []string, err error) {
	base := filepath.Join(root, "artifacts")
	entries, rerr := os.ReadDir(base)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, nil // 从未产出过：空集合等价于零孤儿
		}
		return nil, rerr
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "redirects" {
			continue
		}
		hashes = append(hashes, e.Name())
	}
	return hashes, nil
}

// AuditOrphanArtifacts 对账：磁盘上存在、但没有任何属主认领的产物目录。
//
// 只读、不删除 —— 孤儿文件交给产物 GC 按保留期处理，这里只负责把它们暴露出来。
// 属主查询失败时**不**把该模块的产物当孤儿（宁可漏报也不能误报，误报会导致误删）。
func AuditOrphanArtifacts(ctx context.Context, root string, owners ...OwnerProvider) (orphans []OrphanArtifact, checked int, err error) {
	diskHashes, derr := ListDiskArtifactHashes(root)
	if derr != nil {
		return nil, 0, derr
	}
	checked = len(diskHashes)
	owned := make(map[string]bool, len(diskHashes))
	for _, provider := range owners {
		if provider == nil {
			continue
		}
		hashes, oerr := provider(ctx)
		if oerr != nil {
			return nil, checked, oerr
		}
		for _, h := range hashes {
			if h != "" {
				owned[h] = true
			}
		}
	}
	for _, h := range diskHashes {
		if owned[h] {
			continue
		}
		dir := filepath.Join(root, "artifacts", h)
		bytes, files := dirUsage(dir)
		orphans = append(orphans, OrphanArtifact{Hash: h, Path: dir, Bytes: bytes, Files: files})
	}
	return orphans, checked, nil
}

// dirUsage 统计目录下文件总字节数与个数（读不动就返回 0，不影响对账结论）。
func dirUsage(dir string) (bytes int64, files int) {
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files
}
