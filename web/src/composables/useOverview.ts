import dayjs from 'dayjs'
import { computed, onUnmounted, ref, watch } from 'vue'

import { fetchOverview, type SystemOverview } from '@/api/system'
import { useUserStore } from '@/store/modules/user'
import { BizError } from '@/types/api'

/** 总览接口所需权限点，与后端 registerSystem 保持一致。 */
export const OVERVIEW_PERMISSION = 'dashboard:overview:read'

const AUTO_REFRESH_MS = 10_000

/**
 * 系统总览数据源：总览页与运维页共用同一份采集结果。
 * 无权限时不发请求，由页面显示权限提示，而不是让接口报 403 弹一堆错误提示。
 */
export function useOverview() {
  const user = useUserStore()
  const data = ref<SystemOverview | null>(null)
  const loading = ref(false)
  const error = ref('')
  const collectedAt = ref('')
  const autoRefresh = ref(false)

  const allowed = computed(() => user.hasPermission(OVERVIEW_PERMISSION))
  /** 首屏骨架：仅在还没有任何数据时展示，刷新时不清空已有内容避免闪烁。 */
  const initializing = computed(() => loading.value && data.value === null)

  let timer: ReturnType<typeof setInterval> | undefined

  async function load() {
    if (!allowed.value) return
    loading.value = true
    try {
      // 静默请求：错误在页面内展示，避免自动刷新时反复弹全局提示
      const res = await fetchOverview()
      data.value = res
      collectedAt.value = dayjs(res.host.now).format('YYYY-MM-DD HH:mm:ss')
      error.value = ''
    } catch (e) {
      error.value = e instanceof BizError ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  function stop() {
    if (timer) {
      clearInterval(timer)
      timer = undefined
    }
  }

  watch(autoRefresh, (on) => {
    stop()
    if (on) timer = setInterval(load, AUTO_REFRESH_MS)
  })

  onUnmounted(stop)

  return { data, loading, initializing, error, collectedAt, autoRefresh, allowed, load }
}
