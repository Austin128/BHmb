import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { applyTheme, resolveTheme, watchSystemTheme, type ResolvedTheme, type ThemeMode } from '@/utils/theme'

export type Locale = 'zh-CN' | 'en-US'

export const useAppStore = defineStore(
  'app',
  () => {
    const theme = ref<ThemeMode>('auto')
    const locale = ref<Locale>('zh-CN')
    const collapsed = ref(false)
    const resolved = ref<ResolvedTheme>('light')

    let unwatchSystem: (() => void) | null = null

    function setTheme(mode: ThemeMode) {
      theme.value = mode
      resolved.value = applyTheme(mode)
      bindSystemWatcher()
    }

    // 仅 auto 模式需要订阅系统主题，其余模式解绑以免无谓回调
    function bindSystemWatcher() {
      unwatchSystem?.()
      unwatchSystem = null
      if (theme.value !== 'auto') return
      unwatchSystem = watchSystemTheme(() => {
        resolved.value = applyTheme('auto')
      })
    }

    function toggleTheme() {
      setTheme(resolved.value === 'dark' ? 'light' : 'dark')
    }

    function setLocale(next: Locale) {
      locale.value = next
    }

    function toggleCollapsed() {
      collapsed.value = !collapsed.value
    }

    /** setCollapsed 供响应式断点回调使用：窄屏自动收起、宽屏自动展开。 */
    function setCollapsed(next: boolean) {
      collapsed.value = next
    }

    /** 应用启动时按持久化偏好落地主题。 */
    function init() {
      resolved.value = resolveTheme(theme.value)
      applyTheme(theme.value)
      bindSystemWatcher()
    }

    const isDark = computed(() => resolved.value === 'dark')

    return {
      theme,
      locale,
      collapsed,
      resolved,
      isDark,
      init,
      setTheme,
      toggleTheme,
      setLocale,
      toggleCollapsed,
      setCollapsed,
    }
  },
  { persist: { key: 'nova:app', storage: localStorage, pick: ['theme', 'locale', 'collapsed'] } },
)
