import { Message } from '@arco-design/web-vue'

/**
 * 命令式反馈统一走此模块：保证全站时长、位置一致，也便于测试整体 mock。
 * 业务代码禁止直接调用 Arco 的 Message。
 */
export function toast(content: string, type: 'success' | 'info' | 'warning' | 'error' = 'info') {
  Message[type]({ content, duration: type === 'error' ? 5000 : 3000, closable: type === 'error' })
}
