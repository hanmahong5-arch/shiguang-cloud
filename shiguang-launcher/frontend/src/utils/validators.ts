/**
 * validators
 * ------------------------------------------------------------------
 * 纯函数字段校验器。与后端账号白名单保持严格一致
 * （TokenHandoff.java 的 ACCOUNT_NAME_PATTERN = ^[A-Za-z0-9_-]{4,32}$）。
 *
 * 每个函数返回统一结构 {ok, message?}：
 *   - ok=true 时 message 缺省
 *   - ok=false 时 message 必填，且为直接面向玩家的中文提示
 *
 * 前端做前置校验的目的：减少往返 RTT、立即反馈，不代替后端权威校验。
 */

export type Validation = { ok: boolean; message?: string }

const OK: Validation = { ok: true }

/**
 * 校验服务器邀请码：3–16 位大写字母、数字或连字符。
 * 之所以限制这一窄集是因为码来自运营商手写分发，避开混淆字符（0/O、1/I）需由下发侧自律。
 */
export function validateServerCode(v: string): Validation {
  const s = v.trim().toUpperCase()
  if (!s) return { ok: false, message: '请输入服务器代码' }
  if (!/^[A-Z0-9-]{3,16}$/.test(s)) {
    return { ok: false, message: '格式：3–16 位大写字母、数字或连字符' }
  }
  return OK
}

/**
 * 校验账号：与后端 TokenHandoff.ACCOUNT_NAME_PATTERN 完全一致。
 * 不允许 '.' / '@' / 空格 —— 防御 SQL 与日志注入。
 */
export function validateAccount(v: string): Validation {
  const s = v.trim()
  if (!s) return { ok: false, message: '请输入账号' }
  if (!/^[A-Za-z0-9_-]{4,32}$/.test(s)) {
    return { ok: false, message: '账号长度 4–32 位，仅限字母、数字、下划线或连字符' }
  }
  return OK
}

/**
 * 校验密码：6–64 位，任意可打印 ASCII。
 * Beyond LS 的密码会 SHA1+Base64 存储，无字符集限制，但 launcher 侧不接受全空白密码。
 */
export function validatePassword(v: string): Validation {
  if (!v) return { ok: false, message: '请输入密码' }
  if (v.length < 6) return { ok: false, message: '密码至少 6 位' }
  if (v.length > 64) return { ok: false, message: '密码不得超过 64 位' }
  if (/\s/.test(v)) return { ok: false, message: '密码不得包含空白字符' }
  return OK
}

/**
 * 校验新密码：在基础规则之上，额外要求与原密码不同。
 * 若调用方未传 `old`，则只做基础校验。
 */
export function validateNewPassword(v: string, old?: string): Validation {
  const base = validatePassword(v)
  if (!base.ok) return base
  if (old && v === old) return { ok: false, message: '新密码不得与原密码相同' }
  return OK
}

/**
 * 校验邮箱：宽松正则（单 @、domain 含点），长度 ≤ 128。
 * 不追求完整 RFC 5322 合规，依赖后端对真实投递做最终判定。
 */
export function validateEmail(v: string): Validation {
  const s = v.trim()
  if (!s) return { ok: false, message: '请输入邮箱' }
  if (s.length > 128) return { ok: false, message: '邮箱过长（≤128 字符）' }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s)) {
    return { ok: false, message: '邮箱格式不正确' }
  }
  return OK
}

/**
 * 校验 HTTP(S) URL：必须以 http:// 或 https:// 开头，可解析为 URL 对象。
 * 用于 Control URL 设置。
 */
export function validateHttpUrl(v: string): Validation {
  const s = v.trim()
  if (!s) return { ok: false, message: '请输入地址' }
  if (!/^https?:\/\//i.test(s)) return { ok: false, message: '必须以 http:// 或 https:// 开头' }
  try {
    // URL 构造函数对域名 / IP / 端口都能正确解析
    // eslint-disable-next-line no-new
    new URL(s)
  } catch {
    return { ok: false, message: '地址格式不合法' }
  }
  return OK
}

/**
 * 校验客户端路径：非空、不含明显非法字符。
 * 实际存在性由 Go 后端复查（SetClientPath 会 stat），前端只拒绝空串与换行。
 */
export function validateClientPath(v: string): Validation {
  const s = v.trim()
  if (!s) return { ok: false, message: '请选择客户端目录' }
  if (/[\r\n\t]/.test(s)) return { ok: false, message: '路径含非法字符' }
  return OK
}
