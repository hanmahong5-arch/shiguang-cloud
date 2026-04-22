/**
 * validators 单元测试
 * ------------------------------------------------------------------
 * 使用 node:test + node:assert（零依赖）。frontend/package.json 未声明
 * vitest，为避免引入新依赖，改用 Node 18+ 原生测试运行器：
 *
 *   node --test --loader tsx src/utils/validators.test.ts
 *
 * 若后续添加 vitest，本文件的 describe/it/expect 语义等价可直接迁移。
 */

import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  validateAccount,
  validatePassword,
  validateNewPassword,
  validateEmail,
  validateServerCode,
  validateHttpUrl,
  validateClientPath,
} from './validators'

// ── validateAccount ────────────────────────────────────────────────
test('validateAccount: 合法 4-32 位', () => {
  assert.equal(validateAccount('abcd').ok, true)
  assert.equal(validateAccount('Player_01').ok, true)
  assert.equal(validateAccount('a'.repeat(32)).ok, true)
})

test('validateAccount: 非法字符与空', () => {
  assert.equal(validateAccount('').ok, false)
  assert.equal(validateAccount('   ').ok, false)
  assert.equal(validateAccount('ab c').ok, false) // 空格
  assert.equal(validateAccount('user@x').ok, false) // @
  assert.equal(validateAccount('用户名').ok, false) // 中文
})

test('validateAccount: 边界长度', () => {
  assert.equal(validateAccount('abc').ok, false) // 3 位
  assert.equal(validateAccount('a'.repeat(33)).ok, false) // 33 位
})

// ── validatePassword ───────────────────────────────────────────────
test('validatePassword: 合法', () => {
  assert.equal(validatePassword('abc123').ok, true)
  assert.equal(validatePassword('P@ssw0rd!').ok, true)
  assert.equal(validatePassword('x'.repeat(64)).ok, true)
})

test('validatePassword: 非法', () => {
  assert.equal(validatePassword('').ok, false)
  assert.equal(validatePassword('12345').ok, false) // 过短
  assert.equal(validatePassword('has space').ok, false) // 含空白
  assert.equal(validatePassword('tab\there').ok, false)
})

test('validatePassword: 边界', () => {
  assert.equal(validatePassword('x'.repeat(65)).ok, false)
  assert.equal(validatePassword('x'.repeat(6)).ok, true)
})

// ── validateNewPassword ────────────────────────────────────────────
test('validateNewPassword: 与基础规则一致', () => {
  assert.equal(validateNewPassword('abc123').ok, true)
  assert.equal(validateNewPassword('short').ok, false)
})

test('validateNewPassword: 不得与原密码相同', () => {
  assert.equal(validateNewPassword('same123', 'same123').ok, false)
  assert.equal(validateNewPassword('new123', 'old123').ok, true)
})

test('validateNewPassword: 无 old 参数退化为基础校验', () => {
  assert.equal(validateNewPassword('valid123').ok, true)
  assert.equal(validateNewPassword('').ok, false)
})

// ── validateEmail ──────────────────────────────────────────────────
test('validateEmail: 合法', () => {
  assert.equal(validateEmail('a@b.co').ok, true)
  assert.equal(validateEmail('user.name+tag@example.com').ok, true)
})

test('validateEmail: 非法', () => {
  assert.equal(validateEmail('').ok, false)
  assert.equal(validateEmail('no-at-sign').ok, false)
  assert.equal(validateEmail('a@b').ok, false) // 无点
  assert.equal(validateEmail('a b@c.d').ok, false) // 空格
})

test('validateEmail: 边界长度', () => {
  const long = 'a'.repeat(120) + '@b.co' // 125 字符
  assert.equal(validateEmail(long).ok, true)
  const tooLong = 'a'.repeat(130) + '@b.co'
  assert.equal(validateEmail(tooLong).ok, false)
})

// ── validateServerCode ─────────────────────────────────────────────
test('validateServerCode: 合法', () => {
  assert.equal(validateServerCode('ACE').ok, true)
  assert.equal(validateServerCode('ace-58').ok, true) // 大写化后合法
  assert.equal(validateServerCode('ABCDEFGHIJKLMNOP').ok, true) // 16 位
})

test('validateServerCode: 非法', () => {
  assert.equal(validateServerCode('').ok, false)
  assert.equal(validateServerCode('AB').ok, false) // 2 位
  assert.equal(validateServerCode('code_1').ok, false) // 下划线
  assert.equal(validateServerCode('混字CN').ok, false)
})

test('validateServerCode: 边界', () => {
  assert.equal(validateServerCode('A'.repeat(17)).ok, false)
  assert.equal(validateServerCode('A1-').ok, true) // 3 位边界
})

// ── validateHttpUrl ────────────────────────────────────────────────
test('validateHttpUrl: 合法', () => {
  assert.equal(validateHttpUrl('http://a.com').ok, true)
  assert.equal(validateHttpUrl('https://a.com:8080/p').ok, true)
  assert.equal(validateHttpUrl('HTTP://127.0.0.1').ok, true)
})

test('validateHttpUrl: 非法 scheme 或空', () => {
  assert.equal(validateHttpUrl('').ok, false)
  assert.equal(validateHttpUrl('ftp://a.com').ok, false)
  assert.equal(validateHttpUrl('a.com').ok, false)
})

test('validateHttpUrl: 畸形 URL', () => {
  assert.equal(validateHttpUrl('http://').ok, false)
})

// ── validateClientPath ─────────────────────────────────────────────
test('validateClientPath: 合法', () => {
  assert.equal(validateClientPath('C:\\Games\\Aion').ok, true)
  assert.equal(validateClientPath('/opt/aion').ok, true)
})

test('validateClientPath: 非法', () => {
  assert.equal(validateClientPath('').ok, false)
  assert.equal(validateClientPath('   ').ok, false)
  assert.equal(validateClientPath('a\nb').ok, false) // 换行
  assert.equal(validateClientPath('a\tb').ok, false) // tab
})

test('validateClientPath: 边界（单字符与超长路径）', () => {
  assert.equal(validateClientPath('a').ok, true)
  assert.equal(validateClientPath('C:\\' + 'x'.repeat(500)).ok, true)
})
