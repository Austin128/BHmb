// 错误码 → 前端文案 i18n key 覆盖表。未命中时回落到后端返回的 message。
// 后端错误码定义见 internal/pkg/errs/code.go 与 docs/05-API接口规范.md 5.5。
export const ERROR_MESSAGE_KEYS: Record<number, string> = {
  100001: 'errcode.e100001',
  100003: 'errcode.e100003',
  100005: 'errcode.e100005',
  100009: 'errcode.e100009',
  110001: 'errcode.e110001',
  110002: 'errcode.e110002',
  110003: 'errcode.e110003',
  110004: 'errcode.e110004',
  110005: 'errcode.e110005',
  110006: 'errcode.e110006',
  110010: 'errcode.e110010',
  110016: 'errcode.e110016',
  110021: 'errcode.e110021',
  120002: 'errcode.e120002',
  120003: 'errcode.e120003',
}

/** 这些错误码由页面自行处理，不弹全局提示。 */
export const NO_TOAST_CODES = new Set<number>([110001, 110002, 110003, 110009, 110021])

export const CODE_NEED_2FA = 110009
export const CODE_UNAUTHORIZED = 110001
export const CODE_TOKEN_EXPIRED = 110002
export const CODE_TOKEN_REVOKED = 110003
export const CODE_REFRESH_INVALID = 110021
export const CODE_FORBIDDEN = 120003
export const CODE_NETWORK = 100005
export const CODE_TIMEOUT = 100009
