import 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    /** 免登录路由（登录页、错误页） */
    public?: boolean
    /** 所属模块码，与后端 profile.menus 对齐 */
    module?: string
    /** 进入页面所需权限点，数组表示任一即可 */
    permission?: string | string[]
    /** i18n key，用于文档标题与面包屑 */
    titleKey?: string
  }
}
