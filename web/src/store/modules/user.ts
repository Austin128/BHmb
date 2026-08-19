import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import * as authApi from '@/api/auth'
import type { LoginPayload, ProfileResult, RoleBrief, TwoFAVerifyPayload, UserBrief } from '@/types/user'
import { clearAuth, getAccessToken, setAccessToken } from '@/utils/auth'

export const useUserStore = defineStore(
  'user',
  () => {
    const accessToken = ref(getAccessToken())
    const user = ref<UserBrief | null>(null)
    const roles = ref<RoleBrief[]>([])
    const permissions = ref<string[]>([])
    const menus = ref<string[]>([])
    const loaded = ref(false)

    const isLogin = computed(() => Boolean(accessToken.value))
    const nickname = computed(() => user.value?.nickname || user.value?.username || '')
    const isSuperAdmin = computed(() => permissions.value.includes('*') || Boolean(user.value?.isSuper))

    function applyToken(token: string) {
      accessToken.value = token
      setAccessToken(token)
    }

    /** 登录成功返回 true；需要二次验证时由调用方捕获 110009 处理。 */
    async function login(payload: LoginPayload) {
      const res = await authApi.login(payload)
      applyToken(res.accessToken)
      applyProfileFromLogin(res.user, res.permissions)
      return res
    }

    async function verifyTwoFA(payload: TwoFAVerifyPayload) {
      const res = await authApi.verifyTwoFA(payload)
      applyToken(res.accessToken)
      applyProfileFromLogin(res.user, res.permissions)
      return res
    }

    // 登录响应只带角色码与权限，菜单仍以 profile 为准，故此处不置 loaded
    function applyProfileFromLogin(brief: UserBrief, perms: string[]) {
      user.value = brief
      permissions.value = perms
    }

    async function fetchProfile(): Promise<ProfileResult> {
      const res = await authApi.fetchProfile()
      user.value = res.user
      roles.value = res.roles
      permissions.value = res.permissions
      menus.value = res.menus
      loaded.value = true
      return res
    }

    async function logout() {
      try {
        await authApi.logout()
      } catch {
        // 令牌可能已失效：后端登出失败不应阻塞前端清理
      } finally {
        reset()
      }
    }

    function reset() {
      accessToken.value = ''
      user.value = null
      roles.value = []
      permissions.value = []
      menus.value = []
      loaded.value = false
      clearAuth()
    }

    /** 权限判定唯一入口：超管持有通配权限 *。 */
    function hasPermission(code: string | string[]): boolean {
      if (permissions.value.includes('*')) return true
      const list = Array.isArray(code) ? code : [code]
      return list.some((c) => permissions.value.includes(c))
    }

    return {
      accessToken,
      user,
      roles,
      permissions,
      menus,
      loaded,
      isLogin,
      nickname,
      isSuperAdmin,
      login,
      verifyTwoFA,
      fetchProfile,
      logout,
      reset,
      hasPermission,
    }
  },
  // accessToken 由 utils/auth 单独落 localStorage，这里不重复持久化敏感字段
  { persist: false },
)
