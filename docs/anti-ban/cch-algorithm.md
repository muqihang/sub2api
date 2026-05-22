# CCH 算法文档

 · `src/service/body_patch.rs` · `scripts/cch_signer.py`

---

## 一、背景

Claude Code 每次向 Anthropic API 发送请求时，`system` 字段中会携带一个
`x-anthropic-billing-header`，格式如下：

```
x-anthropic-billing-header: cc_version=2.1.126.a43; cc_entrypoint=cli; cch=4cc5d;
```

其中：

| 字段 | 说明 |
|------|------|
| `cc_version` | Claude Code 版本号 + 3位消息指纹后缀 |
| `cc_entrypoint` | 入口标识，网关统一重写为 `cli` |
| `cch` | **请求体完整性校验值**，本文档的核心 |

---

## 二、CCH 签名算法

### 2.1 算法概述

CCH 是对整个 JSON 请求体（UTF-8 字节）使用 **xxh64** 哈希的低 20 位，格式化为 5 位小写十六进制。

```
cch = lower_hex_5( xxh64(body_with_placeholder, SEED) & 0xFFFFF )
```

### 2.2 完整流程

**步骤 1：重置占位符**

在签名前，将 billing header 中的 `cch` 字段重置为五个零：

```
cch=00000;
```

**步骤 2：计算 xxh64**

对含占位符的完整 JSON body（UTF-8 字节）计算 xxh64，使用固定种子：

```
SEED = 0x4d659218e32a3268   // Rust 网关生产用种子
```

**步骤 3：截取低 20 位**

```
cch_value = hash & 0xFFFFF
cch_str   = format!("{:05x}", cch_value)   // 5位小写十六进制
```

**步骤 4：回填替换**

将 body 中第一个 `cch=00000;` 替换为 `cch={cch_str};`，签名完成。

> **注意：** 签名是对**含占位符的原始 body** 做 hash，而非替换后的 body。
> 验证时需先将 `cch` 字段还原为 `00000`，再重新计算比对。

### 2.3 Rust 实现（body_patch.rs）

```rust
const CCH_SEED: u64 = 0x4d659218e32a3268;
const CCH_MASK: u64 = 0xFFFFF;
const CCH_PLACEHOLDER_FIELD: &[u8] = b"cch=00000;";

pub fn sign_billing_header_cch(body: &[u8]) -> Vec<u8> {
    // 1. 在 billing-header 范围内找到 cch=00000; 的位置
    let cch_location = find_cch_placeholder_in_billing_header(body);
    let Some((header_start, field_offset)) = cch_location else {
        return body.to_vec();  // 无占位符，原样返回
    };

    // 2. 对原始 body（含占位符）计算 xxh64
    let cch = format!("{:05x}", xxh64_seeded(body, CCH_SEED) & CCH_MASK);

    // 3. 原地替换 5 字节
    let value_start = header_start + field_offset + b"cch=".len();
    let mut signed = body.to_vec();
    signed[value_start..value_start + 5].copy_from_slice(cch.as_bytes());
    signed
}
```

### 2.4 Python 实现（cch_signer.py）

```python
CCH_SEED = 0x6E52736AC806831E   # 实验性脚本种子，与生产不同！

def compute_cch(body_with_placeholder: str, seed: int) -> str:
    digest = xxh64(body_with_placeholder.encode("utf-8"), seed)
    return f"{digest & 0xFFFFF:05x}"

def sign_body_text(body: str, seed: int) -> tuple[str, str]:
    normalized, _ = normalize_body_placeholder(body)   # 将 cch 重置为 00000
    cch_value = compute_cch(normalized, seed)
    signed = normalized.replace(f"cch={CCH_PLACEHOLDER}", f"cch={cch_value}", 1)
    return signed, cch_value
```

---

## 三、xxh64 算法

### 3.1 常量

| 常量 | 值（十六进制） |
|------|--------------|
| `PRIME64_1` | `0x9E3779B185EBCA87` |
| `PRIME64_2` | `0xC2B2AE3D27D4EB4F` |
| `PRIME64_3` | `0x165667B19E3779F9` |
| `PRIME64_4` | `0x85EBCA77C2B2AE63` |
| `PRIME64_5` | `0x27D4EB2F165667C5` |

### 3.2 核心子函数

```rust
fn xxh64_round(acc: u64, lane: u64) -> u64 {
    acc.wrapping_add(lane.wrapping_mul(PRIME64_2))
        .rotate_left(31)
        .wrapping_mul(PRIME64_1)
}

fn xxh64_merge_round(acc: u64, lane: u64) -> u64 {
    (acc ^ xxh64_round(0, lane))
        .wrapping_mul(PRIME64_1)
        .wrapping_add(PRIME64_4)
}
```

### 3.3 主流程伪代码

```
xxh64(data, seed):
  if len(data) >= 32:
    v1 = seed + PRIME64_1 + PRIME64_2
    v2 = seed + PRIME64_2
    v3 = seed
    v4 = seed - PRIME64_1
    // 每轮处理 32 字节（4 × 8 字节 lane）
    while remaining >= 32:
      v1 = round(v1, read_u64_le())
      v2 = round(v2, read_u64_le())
      v3 = round(v3, read_u64_le())
      v4 = round(v4, read_u64_le())
    acc = rotl(v1,1) + rotl(v2,7) + rotl(v3,12) + rotl(v4,18)
    acc = merge_round(acc, v1..v4)
  else:
    acc = seed + PRIME64_5

  acc += len(data)
  // 尾部：8字节、4字节、逐字节处理
  // 最终混淆 avalanche
  acc ^= acc >> 33
  acc *= PRIME64_2
  acc ^= acc >> 29
  acc *= PRIME64_3
  acc ^= acc >> 32
  return acc
```

---

## 四、cc_version 指纹算法

`cc_version` 末尾 3 位十六进制后缀是用户消息的内容指纹。

### 4.1 算法步骤

```
fingerprint(message_text, version):
  1. 将 message_text 编码为 UTF-16LE
  2. 取索引 [4, 7, 20] 处的字符（超出长度取 '0'）
  3. input = "59cf53e54c78" + chars + version
  4. hash  = SHA256(input.encode("utf-8"))
  5. return hex(hash)[:3]   // 前 3 位十六进制
```

### 4.2 消息文本提取规则

- 取**第一条**角色为 `user` 的消息
- 若 content 是字符串，直接使用（但含 `<system-reminder>` 的跳过）
- 若 content 是数组，取第一个不含 `<system-reminder>` 的 `text` 类型块

### 4.3 示例

```
消息: "hi"，版本: 2.1.126
UTF-16 长度 = 2，位置 [4,7,20] 均超出 → chars = "000"
input = "59cf53e54c78" + "000" + "2.1.126"
SHA256(input) → 前3位 = f09
最终: cc_version=2.1.126.f09
```

### 4.4 已知指纹对照

| 版本 | 消息 | 指纹 |
|------|------|------|
| `2.1.63` | `"hi"` | `257` |
| `2.1.88` | `"hi"` | `758` |
| `2.1.121` | `"hi"` | `2e5` |
| `2.1.126`（当前） | `"hi"` | `f09` |

---

## 五、完整处理管线

```
原始请求体 (JSON bytes)
        │
        ▼
rewrite_billing_header()
  ├─ 重新计算消息指纹，更新 cc_version=<version>.<fingerprint>
  ├─ 强制 cc_entrypoint=cli
  └─ 重置 cch=00000;
        │
        ▼
patch_metadata()
  ├─ 替换 device_id / account_uuid
  └─ 派生 session_id（SHA256(original_session + account_id)）
        │
        ▼
（可选）rewrite_environment_block()
  ├─ 替换工作目录、OS 版本、Git 用户名为伪造值
  └─ sanitize_user_paths()：替换 /Users/<real>/ → /Users/<fake>/
        │
        ▼
serde_json 紧凑序列化
        │
        ▼
sign_billing_header_cch()
  └─ xxh64(body, 0x4d659218e32a3268) & 0xFFFFF → 回填 cch
        │
        ▼
最终请求体（转发至 Anthropic API）
```

---

## 六、种子差异说明

| 文件 | 种子 | 用途 |
|------|------|------|
| `src/service/body_patch.rs` | `0x4d659218e32a3268` | **生产** |
| `scripts/cch_signer.py` | `0x6E52736AC806831E` | 实验性分析脚本 |

两个种子**不可混用**。实际转发请求以 Rust 网关种子为准。

---

## 七、验证测试向量

以下数据来自 `body_patch.rs` 单元测试，可用于本地验证实现正确性。

| 请求体摘要 | 预期 cch | 完整 hash（hex） |
|-----------|---------|----------------|
| `cc_version=2.1.126.a43`, content=`[hello]` | `4cc5d` | `6aa8a90145d4cc5d` |
| `cc_version=2.1.126.c02`, content=`"test message"` | `7306b` | `8ee5b8ebe4e7306b` |
| `cc_version=2.1.126`, messages=`[]` | `dea28` | `544274a845cdea28` |
| `cc_version=2.1.126`, content=`"hi"`（多 system block） | `c22bb` | `5127deb29fec22bb` |

---

*源文件：`timicc-gateway/src/service/body_patch.rs`、`timicc-gateway/scripts/cch_signer.py`*
