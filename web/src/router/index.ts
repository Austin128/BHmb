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
        path: 'website',
        name: 'Website',
        component: () => import('@/views/website/index.vue'),
        meta: { module: 'website', titleKey: 'menu.website', permission: 'website:site:list' },
      },
      {
        path: 'website/sites/:id',
        name: 'WebsiteDetail',
        component: () => import('@/views/website/detail.vue'),
        // 详情不出现在菜单里，module 仍填 website 以便布局高亮父级入口
        meta: { module: 'website', titleKey: 'website.detailTitle', permission: 'website:site:read' },
      },
      {
        path: 'file',
        name: 'FileManager',
        component: () => import('@/views/file/index.vue'),
        meta: { module: 'file', titleKey: 'menu.file', permission: 'file:file:list' },
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
