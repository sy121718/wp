package crypto

// cipher.go — 可逆加密（AES-256-GCM），用于**需要还原的敏感配置**（如 SMTP 密码、
// 第三方 API key）。
//
// 与同包里的 Md5 / Sha256 的区别：那些是单向摘要（校验用），本文件是可逆的（要用）。
//
// 两个设计决定：
//
//  1. **密钥由调用方传入**，本包不读配置（pkg 不导入 config，见 pkg/CLAUDE.md）。
//  2. **不要复用 auth.session_secret** —— 会话密钥可以轮换（改配置、重启即生效），
//     而用它加密的邮件密码会随轮换一起失效，且失败表现为「某天邮件突然发不出去」，
//     排查起来很难。敏感数据加密应当有自己的、不轻易轮换的密钥。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// ErrCipherKeyMissing 密钥为空。
var ErrCipherKeyMissing = errors.New("加密密钥未配置")

// deriveKey 把任意长度的密钥字符串派生成 AES-256 需要的 32 字节。
//
// 用 SHA-256 而不是要求配置里写满 32 字节：配置写 32 字节 hex 是人肉易错环节，
// 而且容易被写短（AES 会直接拒绝，报错还不好懂）。
func deriveKey(key string) ([]byte, error) {
	if key == "" {
		return nil, ErrCipherKeyMissing
	}
	sum := sha256.Sum256([]byte(key))
	return sum[:], nil
}

// Encrypt 加密：返回 base64(nonce || ciphertext||tag)。
//
// 每次调用都生成随机 nonce —— 同一明文两次加密得到不同密文（不然等于泄露「两条配置相同」）。
func Encrypt(plaintext, key string) (string, error) {
	k, err := deriveKey(key)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	// Seal 把 nonce 作为前缀写入 out（nonce || 密文），解密时按同一长度切回来。
	out := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt 解密。
//
// 密钥不对或密文被改动都会失败（GCM 是认证加密，篡改必然被发现）——
// 这正是我们要的：宁可直接报错，也不要静默解出一段垃圾去连 SMTP。
func Decrypt(ciphertext, key string) (string, error) {
	k, err := deriveKey(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密文长度不足")
	}
	nonce, data := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
