import axios, { AxiosError, type AxiosInstance, type InternalAxiosRequestConfig } from 'axios'

import { ENV } from '@/config/env'
import {
  CODE_NETWORK,
  CODE_TIMEOUT,
  CODE_TOKEN_EXPIRED,
  CODE_TOKEN_REVOKED,
  CODE_UNAUTHORIZED,
  NO_TOAST_CODES,
} from '@/config/errorCode'
import { BizError, type ApiResponse } from '@/types/api'
import { clearAuth, getAccessToken, setAccessToken } from '@/utils/auth'
import { errorText } from '@/utils/errorText'
import { toast } from '@/utils/feedback'
import { genRequestId } from '@/utils/uuid'

const http: AxiosInstance = axios.create({
  baseURL: ENV.apiBaseUrl,
  timeout: ENV.requestTimeout,
  // 必须带上 refreshToken 的 HttpOnly Cookie，否则刷新接口拿不到凭证
  withCredentials: true,
  headers: { 'Content-Type': 'application/json;charset=utf-8' },
})

/* utils 层不静态依赖 store：由 main.ts 在 Pinia 安装后注入 provider。 */
let nodeIdProvider: () => string = () => ''
let localeProvider: () => string = () => 'zh-CN'
let sessionExpiredHandler: () => void = () => {}

export function registerContextProvider(node: () => string, locale: () => string) {
  nodeIdProvider = node
  localeProvider = locale
}

/** 注册会话失效回调（清理 store 并跳登录页），由 main.ts 注入以避免循环依赖。 */
export function registerSessionExpiredHandler(fn: () => void) {
  sessionExpiredHandler = fn
}

http.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const token = getAccessToken()
  if (token) config.headers.Authorization = `Bearer ${token}`

  // nodeId 显式为 null 时不带该头；未指定时取当前节点上下文
  if (config.nodeId !== null) {
    const nodeId = config.nodeId ?? nodeIdProvider()
    if (nodeId) config.headers['X-Node-Id'] = nodeId
  }

  config.headers['X-Request-Id'] = genRequestId()
  const locale = localeProvider()
  if (locale) config.headers['Accept-Language'] = locale
  return config
})

/* ---------- 单飞刷新：并发 401 只发一次 refresh，其余请求排队重放 ---------- */
let refreshing = false
let waiters: Array<(token: string | null) => void> = []

function onRefreshed(token: string | null) {
  waiters.forEach((fn) => fn(token))
  waiters = []
}

async function doRefresh(): Promise<string | null> {
  try {
    // 裸 axios 调用，绕开本实例拦截器，避免刷新接口自身 401 递归
    const { data } = await axios.post<ApiResponse<{ accessToken: string }>>(
      `${ENV.apiBaseUrl}/auth/refresh`,
      null,
      { withCredentials: true, headers: { 'X-Request-Id': genRequestId() } },
    )
    if (data.code !== 0 || !data.data?.accessToken) return null
    setAccessToken(data.data.accessToken)
    return data.data.accessToken
  } catch {
    return null
  }
}

const AUTH_CODES = new Set([CODE_UNAUTHORIZED, CODE_TOKEN_EXPIRED, CODE_TOKEN_REVOKED])

http.interceptors.response.use(
  async (res) => {
    const body = res.data as ApiResponse
    if (body?.code === 0) return res

    const err = new BizError(body.code, body.message, body.traceId, body.data)
    const config = res.config as InternalAxiosRequestConfig & { __retried?: boolean }

    // 认证失效且未重试过：静默刷新后重放原请求
    if (AUTH_CODES.has(body.code) && !config.__retried) {
      config.__retried = true
      const token = await waitForToken()
      if (token) {
        config.headers.Authorization = `Bearer ${token}`
        return http.request(config)
      }
      expireSession(err)
      throw err
    }

    if (!config.silent && !NO_TOAST_CODES.has(body.code)) {
      toast(messageOf(err), 'error')
    }
    throw err
  },
  (error: AxiosError) => {
    // 传输层错误：超时、网络中断、5xx 无信封响应
    const err = normalizeTransportError(error)
    const config = error.config as (InternalAxiosRequestConfig & { silent?: boolean }) | undefined
    if (!config?.silent) toast(err.message, 'error')
    throw err
  },
)

async function waitForToken(): Promise<string | null> {
  if (refreshing) {
    return new Promise<string | null>((resolve) => waiters.push(resolve))
  }
  refreshing = true
  const token = await doRefresh()
  refreshing = false
  onRefreshed(token)
  return token
}

function expireSession(err: BizError) {
  clearAuth()
  if (!NO_TOAST_CODES.has(err.code)) toast(messageOf(err), 'error')
  sessionExpiredHandler()
}

function messageOf(err: BizError): string {
  const text = errorText(err.code, err.message)
  return err.traceId ? `${text}（traceId: ${err.traceId}）` : text
}

function normalizeTransportError(error: AxiosError): BizError {
  const body = error.response?.data as ApiResponse | undefined
  if (body && typeof body.code === 'number') {
    // 保留后端原始 message，展示时再翻译，日志里仍能看到服务端描述
    return new BizError(body.code, body.message, body.traceId)
  }
  if (error.code === 'ECONNABORTED' || error.code === 'ETIMEDOUT') {
    return new BizError(CODE_TIMEOUT, errorText(CODE_TIMEOUT))
  }
  return new BizError(CODE_NETWORK, errorText(CODE_NETWORK))
}

/** 统一取出信封中的 data，业务层不接触 code/message。 */
export async function request<T>(config: Parameters<AxiosInstance['request']>[0]): Promise<T> {
  const res = await http.request<ApiResponse<T>>(config)
  return res.data.data
}

export default http
