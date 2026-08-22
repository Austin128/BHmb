/** 展示层格式化工具。总览、运维与文件页共用同一套口径，避免同一个数字在不同页面显示不一致。 */

const SIZE_UNITS = ['KB', 'MB', 'GB', 'TB', 'PB']

/** 字节数转可读体积，1 位小数；1024 以下直接显示字节。 */
export function humanSize(size: number): string {
  if (!Number.isFinite(size) || size <= 0) return '0 B'
  if (size < 1024) return `${Math.round(size)} B`
  let v = size / 1024
  let i = 0
  while (v >= 1024 && i < SIZE_UNITS.length - 1) {
    v /= 1024
    i += 1
  }
  return `${v.toFixed(1)} ${SIZE_UNITS[i]}`
}

/**
 * 秒数转「3d 4h」这类紧凑时长。
 * 只保留最大的两个量级：运行 3 天的机器没人关心那 12 秒。
 * 单位使用与语言无关的缩写，中英文界面共用。
 */
export function humanDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '-'
  const s = Math.floor(seconds)
  if (s < 60) return `${s}s`

  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`
  return `${m}m`
}

/** 百分比取 1 位小数并夹到 0-100，防止进度条越界。 */
export function percentText(value: number): string {
  return `${clampPercent(value).toFixed(1)}%`
}

/** 夹取百分比到 0-100 区间；非法值按 0 处理。 */
export function clampPercent(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 0
  return value > 100 ? 100 : value
}

/**
 * 按占用率给出状态色：Arco 进度条与标签共用。
 * 90% 以上判红，75% 以上判橙，其余正常。
 */
export function usageStatus(percent: number): 'normal' | 'warning' | 'danger' {
  const v = clampPercent(percent)
  if (v >= 90) return 'danger'
  if (v >= 75) return 'warning'
  return 'normal'
}

/** 进度条颜色，与 usageStatus 一一对应。 */
export function usageColor(percent: number): string {
  const status = usageStatus(percent)
  if (status === 'danger') return 'rgb(var(--danger-6))'
  if (status === 'warning') return 'rgb(var(--warning-6))'
  return 'rgb(var(--primary-6))'
}
