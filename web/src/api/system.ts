import { request } from '@/utils/request'

// 与 internal/api/v1/handler.HealthStatus 对齐。
export interface HealthStatus {
  status: 'ok' | 'degraded'
  version: string
  commit?: string
  buildTime?: string
  uptimeSeconds: number
  database: { driver: string; ok: boolean; error?: string }
}

export function fetchHealth() {
  return request<HealthStatus>({ url: '/health', method: 'GET', nodeId: null, silent: true })
}
