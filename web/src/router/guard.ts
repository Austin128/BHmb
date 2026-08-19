import type { Router } from 'vue-router'

import { ENV } from '@/config/env'
import { i18n } from '@/locales'
import { useUserStore } from '@/store/modules/user'
import { getAccessToken } from '@/utils/auth'

// 路由守卫职责：登录态判定 + profile 兜底拉取 + 页面级权限校验。
// 权限数据只从 store 取，守卫本身不直接调 API 以外的业务逻辑。
export function setupGuard(router: Router) {
  router.beforeEach(async (to) => {
    const user = useUserStore()

    if (to.meta.public) {
      // 已登录用户访问登录页直接回首页，避免重复登录
      if (to.name === 'Login' && getAccessToken()) return { path: '/' }
      return true
    }

    if (!getAccessToken()) {
      return { name: 'Login', query: { redirect: to.fullPath } }
    }

    // 刷新页面后 store 为空：用 accessToken 补拉 profile，失败则回登录页
    if (!user.loaded) {
      try {
        await user.fetchProfile()
      } catch {
        user.reset()
        return { name: 'Login', query: { redirect: to.fullPath } }
      }
    }

    // 菜单为空说明后端尚未下发模块清单（或该里程碑无可见模块），
    // 此时不做模块级拦截，仍由下面的权限点校验与服务端 RBAC 兜底，避免整站锁死。
    const module = to.meta.module as string | undefined
    if (module && user.menus.length > 0 && !user.menus.includes(module)) {
      return { name: 'Forbidden' }
    }

    const permission = to.meta.permission as string | string[] | undefined
    if (permission && !user.hasPermission(permission)) {
      return { name: 'Forbidden' }
    }

    return true
  })

  router.afterEach((to) => {
    const key = to.meta.titleKey as string | undefined
    const title = key ? i18n.global.t(key) : ''
    document.title = title ? `${title} - ${ENV.appTitle}` : ENV.appTitle
  })
}
