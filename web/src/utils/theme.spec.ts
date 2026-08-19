import { describe, expect, it } from 'vitest'

import { applyTheme, resolveTheme, systemTheme, THEME_EVENT } from '@/utils/theme'

describe('theme', () => {
  it('auto 模式解析为系统主题', () => {
    expect(resolveTheme('auto')).toBe(systemTheme())
    expect(resolveTheme('dark')).toBe('dark')
    expect(resolveTheme('light')).toBe('light')
  })

  it('dark 时给 body 打上 arco-theme，light 时移除', () => {
    applyTheme('dark')
    expect(document.body.getAttribute('arco-theme')).toBe('dark')
    expect(document.documentElement.dataset.novaTheme).toBe('dark')

    applyTheme('light')
    expect(document.body.hasAttribute('arco-theme')).toBe(false)
    expect(document.documentElement.dataset.novaTheme).toBe('light')
  })

  it('切换时广播主题事件供自绘组件换色', () => {
    let received = ''
    const handler = (e: Event) => {
      received = (e as CustomEvent<{ theme: string }>).detail.theme
    }
    window.addEventListener(THEME_EVENT, handler)
    applyTheme('dark')
    window.removeEventListener(THEME_EVENT, handler)
    expect(received).toBe('dark')
  })
})
