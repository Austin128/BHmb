// 菜单元数据：后端 profile.menus 只给模块码（dashboard/user/ops），
// 图标、文案 key、落地路由由前端维护。新增模块需与 internal/rbac 的 Module 对齐。
// 图标组件在 layout 层显式映射（Arco 图标必须静态导入才能被打包）。
export interface MenuMeta {
  /** 模块码，与后端 rbac.Permission.Module 一致 */
  module: string
  /** 路由名 */
  name: string
  path: string
  /** i18n key，形如 menu.dashboard */
  titleKey: string
}

export const MENU_META: MenuMeta[] = [
  { module: 'dashboard', name: 'Dashboard', path: '/dashboard', titleKey: 'menu.dashboard' },
  { module: 'website', name: 'Website', path: '/website', titleKey: 'menu.website' },
  { module: 'file', name: 'FileManager', path: '/file', titleKey: 'menu.file' },
  { module: 'user', name: 'UserCenter', path: '/user', titleKey: 'menu.user' },
  { module: 'ops', name: 'Ops', path: '/ops', titleKey: 'menu.ops' },
]

export const MENU_BY_MODULE = new Map(MENU_META.map((m) => [m.module, m]))
