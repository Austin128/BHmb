// 环境变量在此集中转型，业务代码禁止直接读 import.meta.env。
const bool = (v: string | undefined, fallback = false): boolean =>
  v === undefined ? fallback : v === 'true' || v === '1'

const int = (v: string | undefined, fallback: number): number => {
  const n = Number.parseInt(v ?? '', 10)
  return Number.isFinite(n) ? n : fallback
}

export const ENV = {
  appTitle: import.meta.env.VITE_APP_TITLE || 'NovaPanel',
  publicPath: import.meta.env.VITE_PUBLIC_PATH || '/',
  apiBaseUrl: import.meta.env.VITE_API_BASE_URL || '/api/v1',
  wsBaseUrl: import.meta.env.VITE_WS_BASE_URL || '/api/v1/ws',
  requestTimeout: int(import.meta.env.VITE_REQUEST_TIMEOUT, 15000),
  logLevel: import.meta.env.VITE_LOG_LEVEL || 'info',
  isDev: import.meta.env.DEV,
} as const

export { bool, int }
