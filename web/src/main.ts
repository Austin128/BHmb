import { createApp } from 'vue'

import App from './App.vue'
import i18n, { setI18nLocale } from './locales'
import router from './router'
import pinia from './store'
import { useAppStore } from './store/modules/app'
import { useUserStore } from './store/modules/user'
import { registerContextProvider, registerSessionExpiredHandler } from './utils/request'

import '@arco-design/web-vue/dist/arco.css'
import './styles/global.scss'

const app = createApp(App)

// 顺序要求：Pinia 必须先于路由与请求层初始化，
// 因为守卫与拦截器都要读 store 里的登录态与偏好。
app.use(pinia)

const appStore = useAppStore()
const userStore = useUserStore()

appStore.init()
setI18nLocale(appStore.locale)

// 打破 utils/request → store 的循环依赖：运行时注入上下文与会话失效回调
registerContextProvider(
  () => '',
  () => appStore.locale,
)
registerSessionExpiredHandler(() => {
  userStore.reset()
  const redirect = router.currentRoute.value.fullPath
  void router.replace({ name: 'Login', query: redirect === '/login' ? undefined : { redirect } })
})

app.use(i18n)
app.use(router)

void router.isReady().then(() => app.mount('#app'))
