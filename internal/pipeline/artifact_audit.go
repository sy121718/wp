package pipeline

import (
	"context"
	"os"
	"path/filepath"
)

type OrphanArtifactResp struct {
	Hash string `json:"hash"`
	Path string `json:"path"`

	Bytes int64 `json:"bytes"`

	Files int `json:"files"`
}

type OwnerProvider func(ctx context.Context) ([]string, error)

func ListDiskArtifactHashes(root string) (hashes []string, err error) {
	base := filepath.Join(root, "artifacts")
	entries, rerr := os.ReadDir(base)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, nil
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

func AuditOrphanArtifacts(ctx context.Context, root string, owners ...OwnerProvider) (orphans []OrphanArtifactResp, checked int, err error) {
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
		orphans = append(orphans, OrphanArtifactResp{Hash: h, Path: dir, Bytes: bytes, Files: files})
	}
	return orphans, checked, nil
}

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
