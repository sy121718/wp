package commentservice

// comment_ip.go — 来源 IP 的带盐哈希（唯一的算法口径）。
//
// 为什么存哈希而不是明文 IP（同 analytics 模块的既有口径）：
//
//   - 目的是**防刷取证**（「同一个来源短期灌了多少条」），不是「知道他是谁」；
//   - 裸哈希在 IPv4 空间（2^32）里等于把明文换个写法存下来 —— 彩虹表一分钟就能反查，
//     所以必须带盐，且盐是**部署级密钥**（不是硬编码常量，否则等于没加盐）；
//   - 明文 IP 一旦落库就会跟着备份、导出、日志走，而它属于个人数据。
//
// 盐由装配层从配置取（`comment.ip_pepper`，未配置时回退会话密钥并告警），
// 与 analytics 的 pepper、cart 的 cookie 密钥同一条「按用途分离密钥」的形态
// （见 routers/assembly.go 的 resolvePurposeSecret）。
//
// 哈希口径放在 service 而不是 inbound：片段层与后台都要用同一个口径，
// 两边各写一遍 sha256 迟早会分叉（表现是「同一台机器在两个入口算出两个来源」，
// 而这件事不会有任何报错）。

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashSourceIP 计算来源 IP 的带盐哈希（sha256(salt + ":" + ip) 的十六进制小写）。
//
// 输入为空时返回空串：调用方据此略过来源维度的限流与取证（而不是把全空 IP
// 哈希成同一个值 —— 那会让「所有拿不到 IP 的请求」共用一个来源额度）。
func HashSourceIP(salt, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(salt) + ":" + ip))
	return hex.EncodeToString(sum[:])
}
