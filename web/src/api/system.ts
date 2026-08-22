import { request } from '@/utils/request'

// 与 internal/api/v1/handler.HealthStatus 对齐。
export interface DatabaseCheck {
  driver: string
  ok: boolean
  error?: string
}

export interface HealthStatus {
  status: 'ok' | 'degraded'
  version: string
  commit?: string
  buildTime?: string
  uptimeSeconds: number
  database: DatabaseCheck
}

export function fetchHealth() {
  return request<HealthStatus>({ url: '/health', method: 'GET', nodeId: null, silent: true })
}

/* ---------- 系统总览：字段对齐后端 handler.OverviewResult 与 internal/service/sysinfo ---------- */

export interface HostInfo {
  hostname: string
  os: string
  osName: string
  kernel: string
  arch: string
  uptimeSeconds: number
  bootTime?: string
  now: string
  timezone: string
}

export interface CPUInfo {
  model: string
  cores: number
  usagePercent: number
  load1: number
  load5: number
  load15: number
}

export interface MemoryInfo {
  total: number
  available: number
  used: number
  buffers: number
  cached: number
  swapTotal: number
  swapUsed: number
  usedPercent: number
}

export interface DiskInfo {
  device: string
  path: string
  fsType: string
  total: number
  used: number
  free: number
  usedPercent: number
}

export interface NetInterfaceInfo {
  name: string
  bytesRecv: number
  bytesSent: number
  packetsRecv: number
  packetsSent: number
}

export interface PanelInfo {
  version: string
  commit?: string
  buildTime?: string
  goVersion: string
  pid: number
  startedAt: string
  uptimeSeconds: number
  goroutines: number
  heapAllocBytes: number
  rssBytes: number
  numCpu: number
  dataDir?: string
}

export interface DatabaseStat extends DatabaseCheck {
  openConnections: number
  inUse: number
  idle: number
}

export interface SystemOverview {
  host: HostInfo
  cpu: CPUInfo
  memory: MemoryInfo
  disks: DiskInfo[]
  network: NetInterfaceInfo[]
  panel: PanelInfo
  database: DatabaseStat
  /** 为 false 时 CPU/内存/磁盘/网络无法采集（非 Linux 主机），页面必须显式标注不可用 */
  hostMetricsAvailable: boolean
}

/**
 * 采集包含两次 CPU 采样，首次调用可能耗时数百毫秒，调用方需自行处理 loading。
 * silent：错误由页面内联展示，避免自动刷新时反复弹全局提示。
 */
export function fetchOverview() {
  return request<SystemOverview>({ url: '/system/overview', method: 'GET', silent: true })
}
