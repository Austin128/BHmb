import type { PageQuery, PageResult } from '@/types/api'
import { request } from '@/utils/request'

/* 只读接口：账号的增删改目前只在 novactl / bh 命令行提供，
   因此这里不提供任何写方法，页面也不应出现点了没用的按钮。 */

export interface UserItem {
  id: string
  username: string
  nickname: string
  email?: string
  status: string
  isSuper: boolean
  twoFABound: boolean
  roles: string[]
  roleCodes: string[]
  lockedUntil?: string
  loginFailCount: number
  mustChangePassword: boolean
  lastLoginAt?: string
  lastLoginIp?: string
  createdAt: string
}

export interface RoleItem {
  id: string
  code: string
  name: string
  description?: string
  dataScope: string
  isBuiltin: boolean
  status: string
  sort: number
  permissionCount: number
  /** 超管绕过权限校验，数据库里没有绑定行，permissionCount 恒为 0 */
  allPermissions: boolean
}

export interface PermissionItem {
  code: string
  name: string
  module: string
  resource: string
  action: string
  isSensitive: boolean
  granted: boolean
}

export function fetchUsers(params: PageQuery = {}) {
  return request<PageResult<UserItem>>({ url: '/user/users', method: 'GET', params })
}

export function fetchRoles() {
  return request<RoleItem[]>({ url: '/user/roles', method: 'GET' })
}

export function fetchPermissions() {
  return request<PermissionItem[]>({ url: '/user/permissions', method: 'GET' })
}
