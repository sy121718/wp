# RSA 加密传输方案

更新时间：2026-07-13

关联文档：[权限管理开发方案.md](./权限管理开发方案.md) · [模块接口清单.md](./模块接口清单.md)

---

## 一、设计目标

在 HTTPS 传输层加密之上，对密码等敏感字段增加 RSA 加密，防止浏览器开发者工具、日志泄露、代理抓包等场景下明文暴露。

**适用范围：** 密码、身份证号、银行卡号等敏感字段。普通业务字段不需要加密。

---

## 二、核心概念

```
RSA = 非对称加密，公钥加密、私钥解密

公钥（Public Key）：
  - 给前端，前端用来加密
  - 暴露没关系，拿到公钥只能加密不能解密

私钥（Private Key）：
  - 留在后端，后端用来解密
  - 绝不外泄，谁拿到私钥谁就能解密所有密文

Base64：
  - 不是加密，是编码
  - RSA 加密后输出二进制，JSON 传不了二进制
  - Base64 把二进制转成文本，方便放 JSON 里传输
  - 解密时先 Base64 解码，再 RSA 解密
```

---

## 三、数据流

### 3.1 获取公钥

```
前端启动时或登录页加载时：
  GET /api/auth/public-key

后端返回：
{
  "code": 200,
  "data": {
    "public_key": "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA..."
  }
}

前端缓存公钥，后续加密用
```

### 3.2 加密并提交

```
前端：
  1. 用户输入密码 "123456"
  2. RSA 加密（用公钥）→ 二进制密文
  3. Base64 编码 → 文本密文 "c8e2a1f3..."
  4. 提交：
     POST /api/admin/login
     { "username": "admin", "password": "c8e2a1f3..." }
                                      ↑ 密文，明文不出现在网络请求里
```

### 3.3 后端解密

```
后端收到请求：
  1. 取 password = "c8e2a1f3..."
  2. Base64 解码 → 二进制密文
  3. RSA 解密（用私钥）→ "123456"
  4. bcrypt 哈希 → 存储或比对
  5. 原始明文 "123456" 用完即弃，不记日志
```

---

## 四、密钥管理

### 4.1 生成密钥对

```bash
# 生成 2048 位 RSA 私钥
openssl genrsa -out private_key.pem 2048

# 从私钥导出公钥
openssl rsa -in private_key.pem -pubout -out public_key.pem
```

### 4.2 存放位置

```
私钥：配置文件或环境变量（绝不提交到代码仓库）
  config/config.yaml:
    crypto:
      rsa_private_key: |
        -----BEGIN PRIVATE KEY-----
        MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQ...
        -----END PRIVATE KEY-----

公钥：接口动态返回（不写死在前端代码里）
  GET /api/auth/public-key → 返回公钥内容
```

### 4.3 密钥轮换

- 密钥泄露或定期轮换时，生成新密钥对
- 更新配置文件中的私钥
- 前端下次请求公钥接口自动拿到新公钥
- 旧密文用新私钥解不开，前端需要重新加密提交

---

## 五、后端实现

### 5.1 pkg/crypto 扩展

在 `pkg/crypto/` 下新增 `rsa.go`：

```go
package crypto

// 公开 API：
// Encrypt(plaintext string, publicKey string) (string, error)
//   明文 + 公钥 → Base64 密文
//
// Decrypt(ciphertext string, privateKey string) (string, error)
//   Base64 密文 + 私钥 → 明文
```

### 5.2 调用方

```go
// 登录场景
func (s *Service) Login(ctx context.Context, req *LoginReq) (*LoginResp, error) {
    // RSA 解密密码
    password, err := crypto.Decrypt(req.Password, s.privateKey)
    if err != nil {
        return nil, errors.New("密码解密失败")
    }

    // bcrypt 比对
    if !crypto.CompareHash(admin.Password, password) {
        return nil, errors.New("用户名或密码错误")
    }

    // ... 后续逻辑
}
```

---

## 六、前端实现

### 6.1 安装依赖

```bash
cd web && pnpm add jsencrypt
```

### 6.2 加密工具

```typescript
// src/utils/crypto.ts
import JSEncrypt from 'jsencrypt'

const encryptor = new JSEncrypt()

// 启动时获取公钥并设置
export async function initPublicKey() {
  const res = await fetch('/api/auth/public-key')
  const data = await res.json()
  encryptor.setPublicKey(data.data.public_key)
}

// 加密敏感字段
export function rsaEncrypt(plaintext: string): string {
  const result = encryptor.encrypt(plaintext)
  if (!result) {
    throw new Error('RSA 加密失败')
  }
  return result  // 已经是 Base64 字符串
}
```

### 6.3 使用

```typescript
// 登录提交前加密密码
const encryptedPassword = rsaEncrypt(form.password)
await loginApi({
  username: form.username,
  password: encryptedPassword  // 密文提交
})
```

---

## 七、接口规划

| 路由 | Method | 说明 |
|------|--------|------|
| `/api/auth/public-key` | GET | 获取 RSA 公钥（免登录） |

返回结构：

```json
{
  "code": 200,
  "data": {
    "public_key": "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA..."
  }
}
```

---

## 八、安全约定

- **私钥绝不外泄**：只存在后端配置文件或环境变量，不提交到代码仓库
- **公钥动态获取**：不写死在前端代码里，通过接口返回（方便轮换）
- **明文用完即弃**：后端解密后立即 bcrypt 哈希，原始明文不记日志、不缓存
- **只加密敏感字段**：密码、身份证、银行卡。普通字段明文传输，不做全 body 加密
- **HTTPS 是前提**：RSA 是 HTTPS 之上的补充，不是替代
- **前端不存私钥**：前端只有公钥，解密能力只在后端

---

## 九、边界说明

- RSA 加密长度限制：2048 位密钥最多加密 245 字节明文。密码够用，大文本不适用
- 如需加密大文本：用 RSA 加密 AES 密钥，AES 加密正文（混合加密，当前不需要）
- 公钥接口免登录：登录前需要公钥加密密码，所以公钥接口不能要 JWT
- 前端缓存公钥：获取一次后缓存在内存，不用每次请求都拉
- 公钥接口建议加频率限制：防恶意调用（虽无敏感数据，但可避免被用于重放攻击探测）
- 此方案为后续实现，当前优先级低于权限管理和数据权限模块
- auth 模块为新增模块（`internal/module/auth/`），接口清单见[模块接口清单](./模块接口清单.md)第 6 节