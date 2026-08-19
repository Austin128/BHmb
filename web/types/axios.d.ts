import 'axios'

// 通过模块声明合并把 RequestExtra 挂到 Axios 配置上，
// 使 request(config) 的调用方能直接传 silent / nodeId。
declare module 'axios' {
  export interface AxiosRequestConfig {
    /** 跳过全局错误提示，由调用方自行处理 */
    silent?: boolean
    /** 覆盖 X-Node-Id；传 null 表示本次请求不带节点上下文（如登录） */
    nodeId?: string | null
  }
}
