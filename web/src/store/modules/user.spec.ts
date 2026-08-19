import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as authApi from '@/api/auth'
import { useUserStore } from '@/store/modules/user'
import type { ProfileResult } from '@/types/user'
import { getAccessToken, setAccessToken } from '@/utils/auth'

// api 层整体替身：store 只负责状态收敛，不该在单测里发真实请求
vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  verifyTwoFA: vi.fn(),
  refresh: vi.fn(),
  logout: vi.fn(),
  fetchProfile: vi.fn(),
}))

const PROFILE: ProfileResult = {
  user: {
    id: '1',
    username: 'admin',
    nickname: '管理员',
    avatar: null,
    isSuper: false,
    language: 'zh-CN',
    theme: 'auto',
    mustChangePassword: false,
    twoFABound: false,
  },
  roles: [{ code: 'admin', name: '管理员', dataScope: 'all' }],
  permissions: ['user:user:list'],
  nodeScope: [],
  menus: ['dashboard', 'user'],
}

describe('user store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setAccessToken('')
    // restoreMocks 会清掉实现，故每例重新装配
    vi.mocked(authApi.fetchProfile).mockResolvedValue(PROFILE)
    vi.mocked(authApi.logout).mockRejectedValue(new Error('token already invalid'))
  })

  it('通配权限放行任意权限点', () => {
    const store = useUserStore()
    store.permissions = ['*']
    expect(store.hasPermission('ops:setting:list')).toBe(true)
    expect(store.isSuperAdmin).toBe(true)
  })

  it('精确权限只放行已授予的权限点', () => {
    const store = useUserStore()
    store.permissions = ['user:user:list']
    expect(store.hasPermission('user:user:list')).toBe(true)
    expect(store.hasPermission(['ops:setting:list', 'user:user:list'])).toBe(true)
    expect(store.hasPermission('ops:setting:list')).toBe(false)
  })

  it('fetchProfile 落库菜单与角色并置 loaded', async () => {
    const store = useUserStore()
    await store.fetchProfile()
    expect(store.menus).toEqual(['dashboard', 'user'])
    expect(store.roles[0]?.code).toBe('admin')
    expect(store.loaded).toBe(true)
    expect(store.nickname).toBe('管理员')
  })

  it('后端登出失败仍完成本地清理', async () => {
    const store = useUserStore()
    setAccessToken('token-a')
    store.accessToken = 'token-a'
    await store.fetchProfile()

    await store.logout()

    expect(store.accessToken).toBe('')
    expect(store.menus).toEqual([])
    expect(store.loaded).toBe(false)
    expect(getAccessToken()).toBe('')
  })
})
