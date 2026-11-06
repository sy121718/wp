package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"
)

// Version 计算一个只读 FS 全部文件的内容哈希（按逻辑键排序后依次写入
// 键名与内容字节），返回 16 位十六进制串。
//
// 它只依赖 fs.FS，供应用对**最终实际复合 FS**求版本：既覆盖工具资产，也覆盖
// 应用宿主适配资产；不读磁盘 mtime，也不假定私有目录。
func Version(fsys fs.FS) (string, error) {
	keys := make([]string, 0, 64)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		keys = append(keys, p)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		data, err := fs.ReadFile(fsys, key)
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
