import enUS from '@arco-design/web-vue/es/locale/lang/en-us'
import zhCN from '@arco-design/web-vue/es/locale/lang/zh-cn'
import { createI18n } from 'vue-i18n'

import en from './en-US'
import zh from './zh-CN'

export type AppLocale = 'zh-CN' | 'en-US'

export const i18n = createI18n({
  legacy: false,
  globalInjection: true,
  locale: 'zh-CN' as AppLocale,
  fallbackLocale: 'zh-CN',
  messages: { 'zh-CN': zh, 'en-US': en },
})

/** Arco 组件库自带文案与应用文案共用一套 locale 开关。 */
export const ARCO_LOCALES = { 'zh-CN': zhCN, 'en-US': enUS } as const

export function setI18nLocale(locale: AppLocale) {
  i18n.global.locale.value = locale
  document.documentElement.lang = locale
}

export const t = i18n.global.t

export default i18n
