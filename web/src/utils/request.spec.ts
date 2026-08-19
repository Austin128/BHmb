import axios, { type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ApiResponse } from '@/types/api'
import { clearAuth, getAccessToken, setAccessToken } from '@/utils/auth'
import http, { registerSessionExpiredHandler, request } from '@/utils/request'

// 提示层依赖 Arco 的 Message 单例，测试里不需要真实渲染
vi.mock('@/utils/feedback', () => ({ toast: vi.fn() }))

function envelope<T>(code: number, data: T): ApiResponse<T> {
  return { code, message: code === 0 ? 'ok' : 'token expired', data, traceId: 'trace-1', timestamp: 0 }
}

function stubAdapter(onCall: (config: InternalAxiosRequestConfig) => ApiResponse<unknown>) {
  http.defaults.adapter = async (config) =>
    ({
      data: onCall(config),
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }) as AxiosResponse
}

describe('request 拦截器', () => {
  beforeEach(() => {
    clearAuth()
    registerSessionExpiredHandler(() => {})
  })

  it('注入 Authorization 与 X-Request-Id', async () => {
    setAccessToken('token-a')
    const seen: Record<string, unknown> = {}
    stubAdapter((config) => {
      Object.assign(seen, config.headers)
      return envelope(0, { ok: true })
    })

    await request<{ ok: boolean }>({ url: '/auth/profile', method: 'GET' })

    expect(seen.Authorization).toBe('Bearer token-a')
    expect(String(seen['X-Request-Id']).length).toBeGreaterThan(0)
  })

  it('并发认证失效只触发一次 refresh，并重放原请求', async () => {
    setAccessToken('expired')
    const refresh = vi
      .spyOn(axios, 'post')
      .mockResolvedValue({ data: envelope(0, { accessToken: 'token-new' }) })

    let firstPass = 0
    stubAdapter((config) => {
      const retried = (config as InternalAxiosRequestConfig & { __retried?: boolean }).__retried
      if (!retried) {
        firstPass += 1
        return envelope(110002, null)
      }
      return envelope(0, { url: config.url })
    })

    const [a, b] = await Promise.all([
      request<{ url: string }>({ url: '/a', method: 'GET', silent: true }),
      request<{ url: string }>({ url: '/b', method: 'GET', silent: true }),
    ])

    expect(firstPass).toBe(2)
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(getAccessToken()).toBe('token-new')
    expect([a.url, b.url].sort()).toEqual(['/a', '/b'])
  })

  it('refresh 失败时清理令牌并通知会话失效', async () => {
    setAccessToken('expired')
    vi.spyOn(axios, 'post').mockRejectedValue(new Error('401'))
    const onExpired = vi.fn()
    registerSessionExpiredHandler(onExpired)

    stubAdapter(() => envelope(110002, null))

    await expect(request({ url: '/a', method: 'GET', silent: true })).rejects.toMatchObject({
      code: 110002,
    })
    expect(getAccessToken()).toBe('')
    expect(onExpired).toHaveBeenCalledTimes(1)
  })

  it('业务错误按信封 code 抛出 BizError', async () => {
    stubAdapter(() => envelope(120003, null))
    await expect(request({ url: '/a', method: 'GET', silent: true })).rejects.toMatchObject({
      code: 120003,
      traceId: 'trace-1',
    })
  })
})
