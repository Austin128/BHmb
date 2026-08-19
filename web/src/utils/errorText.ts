import { ERROR_MESSAGE_KEYS } from '@/config/errorCode'
import { i18n } from '@/locales'

/**
 * errorText 把业务错误码翻译成当前语言的提示文案。
 * 优先级：前端覆盖文案 -> 后端 message -> 通用兜底，
 * 这样后端新增错误码时无需改前端也能显示可读信息。
 */
export function errorText(code: number, fallback?: string): string {
  const key = ERROR_MESSAGE_KEYS[code]
  if (key && i18n.global.te(key)) return i18n.global.t(key)
  return fallback?.trim() || i18n.global.t('common.requestFailed')
}
