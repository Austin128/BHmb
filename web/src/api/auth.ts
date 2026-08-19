import type {
  LoginPayload,
  LoginResult,
  ProfileResult,
  RefreshResult,
  TwoFAVerifyPayload,
} from '@/types/user'
import { request } from '@/utils/request'

// 登录/2FA 失败由页面自行处理提示（含 110009 分流），故统一 silent。
export function login(payload: LoginPayload) {
  return request<LoginResult>({
    url: '/auth/login',
    method: 'POST',
    data: payload,
    nodeId: null,
    silent: true,
  })
}

export function verifyTwoFA(payload: TwoFAVerifyPayload) {
  return request<LoginResult>({
    url: '/auth/2fa/verify',
    method: 'POST',
    data: payload,
    nodeId: null,
    silent: true,
  })
}

export function refresh() {
  return request<RefreshResult>({
    url: '/auth/refresh',
    method: 'POST',
    nodeId: null,
    silent: true,
  })
}

export function logout() {
  return request<null>({ url: '/auth/logout', method: 'POST', nodeId: null, silent: true })
}

export function fetchProfile() {
  return request<ProfileResult>({ url: '/auth/profile', method: 'GET', nodeId: null })
}
