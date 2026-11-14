// Package baseline 暴露初始化基线的产物：manifest、结构与系统数据脚本。
//
// manifest 是跳过判定的唯一依据：迁移器把它与 sys_baseline.manifest_sha256 比对，
// 一致才跳过 manifest 内的历史迁移；任何不一致都回落全量迁移（安全默认）。
// 产物由 scripts/baseline-generate.sh 生成，不手改。
package baseline

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"strings"
)

//go:embed manifest.txt structure.sql system_data.sql
var assets embed.FS

func read(name string) string {
	data, err := assets.ReadFile(name)
	if err != nil {
		// embed 在编译期保证文件存在，走到这里说明 //go:embed 指令与文件名不一致。
		panic("baseline 产物缺失：" + name + ": " + err.Error())
	}
	return string(data)
}

// ManifestText 返回 manifest 原文。
func ManifestText() string { return read("manifest.txt") }

// ManifestSHA256 返回 manifest 的十六进制 SHA-256。
func ManifestSHA256() string {
	sum := sha256.Sum256([]byte(ManifestText()))
	return hex.EncodeToString(sum[:])
}

// Versions 返回 manifest 登记的迁移 version，保持注册序列顺序。
// 空行与 # 开头的注释行不计入。
func Versions() []string {
	var out []string
	for _, line := range strings.Split(ManifestText(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// StructureSQL 返回基线结构脚本（非分区对象 + 分区父表，不含月分区）。
func StructureSQL() string { return read("structure.sql") }

// SystemDataSQL 返回基线系统数据脚本。
func SystemDataSQL() string { return read("system_data.sql") }
