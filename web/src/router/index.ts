import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

import { ENV } from '@/config/env'

import { setupGuard } from './guard'

export const LOGIN_ROUTE = 'Login'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: LOGIN_ROUTE,
    component: () => import('@/views/login/index.vue'),
    meta: { public: true, titleKey: 'route.login' },
  },
  {
    path: '/',
    component: () => import('@/layout/index.vue'),
    redirect: '/dashboard',
    children: [
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('@/views/dashboard/index.vue'),
        meta: { module: 'dashboard', titleKey: 'menu.dashboard' },
      },
      {
        path: 'user',
        name: 'UserCenter',
        component: () => import('@/views/user/index.vue'),
        meta: { module: 'user', titleKey: 'menu.user', permission: 'user:user:list' },
      },
      {
        path: 'ops',
        name: 'Ops',
        component: () => import('@/views/ops/index.vue'),
        meta: { module: 'ops', titleKey: 'menu.ops', permission: 'ops:setting:list' },
      },
    ],
  },
  {
    path: '/403',
    name: 'Forbidden',
    component: () => import('@/views/error/403.vue'),
    meta: { public: true },
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/error/404.vue'),
    meta: { public: true },
  },
]

const router = createRouter({
  history: createWebHistory(ENV.publicPath),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})

setupGuard(router)

export default router
