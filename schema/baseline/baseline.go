// Package baseline 暴露初始化基线的产物：manifest、结构与系统数据脚本。
//
// manifest 冻结「基线覆盖的集合」：迁移段与 seed 段各自是一个**前缀**，
// 段内顺序即执行顺序。段外的新版本照常执行 —— 新增迁移不需要重生成基线。
//
// 迁移器把 sys_baseline.manifest_sha256 与 ContentSHA256 比对。该列名沿用历史，
// 但存的是**产物内容摘要**（manifest + structure.sql + 系统数据，时间字面量归一后），
// 不是清单本身的摘要：只比清单的话，基线 SQL 内容变了也会被误判成「已覆盖」。
// 时间字面量必须归一 —— 系统数据的时间列由生成时刻的 now() 写入，不归一则
// 每次重生成哈希都变，已接入的库会无谓地回落全量。
//
// 产物由 scripts/baseline-generate.sh 生成，不手改。
package baseline

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"regexp"
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

// Manifest 是 manifest 的两段：基线覆盖的迁移与 seed，各自保持注册顺序。
type Manifest struct {
	Migrations []string
	Seeds      []string
}

// ParseManifest 解析 manifest。段标记为 [migrations] / [seeds]，
// 空行与 # 开头的注释行不计入。
func ParseManifest() Manifest {
	var out Manifest
	var section *[]string
	for _, line := range strings.Split(ManifestText(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch line {
		case "[migrations]":
			section = &out.Migrations
			continue
		case "[seeds]":
			section = &out.Seeds
			continue
		}
		if section != nil {
			*section = append(*section, line)
		}
	}
	return out
}

// Migrations 返回基线覆盖的迁移 version。
func Migrations() []string { return ParseManifest().Migrations }

// Seeds 返回基线覆盖的 seed version。
func Seeds() []string { return ParseManifest().Seeds }

// StructureSQL 返回基线结构脚本（非分区对象 + 分区父表，不含月分区）。
func StructureSQL() string { return read("structure.sql") }

// SystemDataSQL 返回基线系统数据脚本（迁移与 seed 都跑完后的系统数据）。
func SystemDataSQL() string { return read("system_data.sql") }

// tsLiteralRe 匹配系统数据里的时间字面量（pg_dump 的 `YYYY-MM-DD HH:MM:SS[.ffffff]+TZ`）。
// 与 scripts/baseline-generate.sh 的 sed 归一规则必须逐字等价。
var tsLiteralRe = regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?\+[0-9]{2}`)

// NormalizeTimestamps 把时间字面量替换成占位符。导出是为了让测试与生成器共用同一口径。
func NormalizeTimestamps(s string) string { return tsLiteralRe.ReplaceAllString(s, "<TS>") }

// stripGenerationStamp 去掉产物头部的「-- 生成时间：」注释行。
// 它每次生成都不同，属于噪声而不是内容；不剥的话重生成一次摘要就变，
// 已接入的库会无谓地回落全量。按行过滤再 Join，保留原文末尾换行与否（与 shell 侧一致）。
func stripGenerationStamp(s string) string {
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "-- 生成时间：") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// ContentSHA256 返回基线内容的十六进制 SHA-256。
//
// 拼接顺序与分隔符固定，scripts/baseline-generate.sh 必须用同样的顺序：
// manifest 原文 → "--structure--" → structure.sql → "--system-data--" → 归一后的系统数据。
func ContentSHA256() string {
	h := sha256.New()
	h.Write([]byte(ManifestText()))
	h.Write([]byte("\n--structure--\n"))
	h.Write([]byte(stripGenerationStamp(StructureSQL())))
	h.Write([]byte("\n--system-data--\n"))
	h.Write([]byte(NormalizeTimestamps(stripGenerationStamp(SystemDataSQL()))))
	return hex.EncodeToString(h.Sum(nil))
}
