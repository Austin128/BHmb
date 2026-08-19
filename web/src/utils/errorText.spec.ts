import { describe, expect, it } from 'vitest'

import { i18n, setI18nLocale } from '@/locales'
import { errorText } from '@/utils/errorText'

describe('errorText', () => {
  it('已知错误码优先用前端覆盖文案', () => {
    setI18nLocale('zh-CN')
    expect(errorText(110004, '后端原文')).toBe('用户名或密码错误')
  })

  it('跟随语言切换', () => {
    setI18nLocale('en-US')
    expect(errorText(110004)).toBe('Incorrect username or password')
    setI18nLocale('zh-CN')
  })

  it('未覆盖的错误码回落到后端 message', () => {
    expect(errorText(130099, '磁盘挂载失败')).toBe('磁盘挂载失败')
  })

  it('后端 message 为空时给出通用兜底', () => {
    expect(errorText(130099, '   ')).toBe(i18n.global.t('common.requestFailed'))
  })
})
