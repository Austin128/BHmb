// 统一响应信封，与后端 internal/api/v1/response.Result 一一对应。
export interface ApiResponse<T = unknown> {
  code: number
  message: string
  data: T
  traceId: string
  timestamp: number
}

export interface PageQuery {
  page?: number
  pageSize?: number
  keyword?: string
  sort?: string
  order?: 'asc' | 'desc'
}

export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  pageSize: number
}

/** 业务错误：拦截器在 code !== 0 时抛出，业务侧可按 code 分支处理。 */
export class BizError extends Error {
  constructor(
    public code: number,
    message: string,
    public traceId = '',
    public data: unknown = null,
  ) {
    super(message)
    this.name = 'BizError'
  }
}

export interface RequestExtra {
  /** 跳过全局错误提示，由调用方自行处理 */
  silent?: boolean
  /** 覆盖 X-Node-Id；传 null 表示本次请求不带节点上下文（如登录） */
  nodeId?: string | null
}
