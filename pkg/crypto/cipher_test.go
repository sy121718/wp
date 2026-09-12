package crypto

// cipher_test.go — 可逆加密的语义测试。
import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := "app-secret-for-test"
	plain := "74rGAtDoaJzzA09Q"
	ct, err := Encrypt(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ct, plain) {
		t.Fatal("密文里不该出现明文")
	}
	got, err := Decrypt(ct, key)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("往返不一致: %q != %q", got, plain)
	}
}

func TestEncryptUsesRandomNonce(t *testing.T) {
	// 同一明文两次加密必须不同 —— 否则等于泄露「两条配置相同」。
	a, _ := Encrypt("same", "k")
	b, _ := Encrypt("same", "k")
	if a == b {
		t.Fatal("两次加密结果相同，nonce 没有随机化")
	}
}

func TestDecryptRejectsWrongKeyAndTamperedText(t *testing.T) {
	ct, _ := Encrypt("secret", "key-a")
	if _, err := Decrypt(ct, "key-b"); err == nil {
		t.Fatal("换个密钥应当解不开")
	}
	// 翻转密文里的一个字符（GCM 认证应当发现）。
	tampered := []byte(ct)
	if tampered[len(tampered)/2] == 'A' {
		tampered[len(tampered)/2] = 'B'
	} else {
		tampered[len(tampered)/2] = 'A'
	}
	if _, err := Decrypt(string(tampered), "key-a"); err == nil {
		t.Fatal("被改动的密文必须解密失败（认证加密的意义）")
	}
}

func TestCipherRejectsEmptyKey(t *testing.T) {
	if _, err := Encrypt("x", ""); err == nil {
		t.Fatal("空密钥应当报错")
	}
	if _, err := Decrypt("x", ""); err == nil {
		t.Fatal("空密钥应当报错")
	}
}
