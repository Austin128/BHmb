export type ThemeMode = 'light' | 'dark' | 'auto'
export type ResolvedTheme = 'light' | 'dark'

/** 主题变更事件：图表、终端等自绘组件监听它做换色，不重建整棵组件树。 */
export const THEME_EVENT = 'nova:theme-change'

const media = () =>
  typeof window !== 'undefined' && window.matchMedia
    ? window.matchMedia('(prefers-color-scheme: dark)')
    : null

export function systemTheme(): ResolvedTheme {
  return media()?.matches ? 'dark' : 'light'
}

export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  return mode === 'auto' ? systemTheme() : mode
}

/** 应用主题：Arco 用 body[arco-theme]，自有令牌挂 html[data-nova-theme]，两套同源切换。 */
export function applyTheme(mode: ThemeMode): ResolvedTheme {
  const theme = resolveTheme(mode)
  if (theme === 'dark') document.body.setAttribute('arco-theme', 'dark')
  else document.body.removeAttribute('arco-theme')

  document.documentElement.dataset.novaTheme = theme
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute('content', theme === 'dark' ? '#17171a' : '#ffffff')

  window.dispatchEvent(new CustomEvent(THEME_EVENT, { detail: { theme } }))
  return theme
}

/** auto 模式下跟随系统切换；返回取消订阅函数。 */
export function watchSystemTheme(onChange: (theme: ResolvedTheme) => void): () => void {
  const mq = media()
  if (!mq) return () => {}
  const handler = (e: MediaQueryListEvent) => onChange(e.matches ? 'dark' : 'light')
  mq.addEventListener('change', handler)
  return () => mq.removeEventListener('change', handler)
}
