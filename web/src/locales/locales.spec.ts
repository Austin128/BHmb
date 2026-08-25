import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import en from '@/locales/en-US'
import zh from '@/locales/zh-CN'

/** 把嵌套文案对象拍平成 a.b.c 形式的 key 集合。 */
function flatten(obj: Record<string, unknown>, prefix = '', out = new Set<string>()) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object') flatten(v as Record<string, unknown>, key, out)
    else out.add(key)
  }
  return out
}

function walk(dir: string, exts: string[], out: string[] = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, exts, out)
    else if (exts.some((e) => name.endsWith(e))) out.push(p)
  }
  return out
}

const zhKeys = flatten(zh as unknown as Record<string, unknown>)
const enKeys = flatten(en as unknown as Record<string, unknown>)

describe('locales', () => {
  it('中英文 key 完全对齐，避免切语言后出现原始 key', () => {
    const onlyZh = [...zhKeys].filter((k) => !enKeys.has(k))
    const onlyEn = [...enKeys].filter((k) => !zhKeys.has(k))
    expect({ onlyZh, onlyEn }).toEqual({ onlyZh: [], onlyEn: [] })
  })

  it('界面里用到的静态 key 都有文案', () => {
    // 只校验字面量 key：动态拼接的 key（如 `a.${code}`）由各自组件兜底。
    const pattern = /\$?\bt\(\s*'([a-zA-Z][a-zA-Z0-9_.]*)'/g
    const missing: string[] = []
    for (const file of walk(join(process.cwd(), 'src'), ['.vue', '.ts'])) {
      if (file.endsWith('.spec.ts')) continue
      const src = readFileSync(file, 'utf8')
      for (const m of src.matchAll(pattern)) {
        if (!zhKeys.has(m[1])) missing.push(`${m[1]} @ ${file.replace(process.cwd(), '.')}`)
      }
    }
    expect(missing).toEqual([])
  })
})
