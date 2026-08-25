import ArcoVue from '@arco-design/web-vue'
import ArcoVueIcon from '@arco-design/web-vue/es/icon'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

import * as api from '@/api/website'
import type { SiteLog } from '@/api/website'
import zh from '@/locales/zh-CN'
import { useUserStore } from '@/store/modules/user'
import { BizError } from '@/types/api'
import SiteLogsCard from '@/views/website/components/SiteLogsCard.vue'

vi.mock('@/api/website', () => ({
  fetchSiteLog: vi.fn(),
  clearSiteLog: vi.fn(),
}))

// Modal.confirm 在 jsdom 里不好点，直接执行 onOk 断言确认后的行为；
// 同时保留其余导出，卡片模板仍要用真实的 Arco 组件渲染。
const confirmSpy = vi.hoisted(() => vi.fn())
vi.mock('@arco-design/web-vue', async (orig) => {
  const actual = (await orig()) as Record<string, unknown>
  return {
    ...actual,
    Modal: { confirm: confirmSpy },
    Message: { success: vi.fn(), error: vi.fn() },
  }
})

function logOf(patch: Partial<SiteLog> = {}): SiteLog {
  return {
    siteId: '7',
    name: 'demo',
    type: 'access',
    path: '/www/wwwlogs/demo.log',
    enabled: true,
    exists: true,
    size: 128,
    modifiedAt: 1_766_000_000_000,
    lines: ['GET / 200', 'GET /a 404'],
    truncated: false,
    ...patch,
  }
}

function mountCard(permissions: string[]) {
  setActivePinia(createPinia())
  useUserStore().permissions = permissions
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } })
  return mount(SiteLogsCard, {
    props: { siteId: '7' },
    global: { plugins: [ArcoVue, ArcoVueIcon, i18n] },
  })
}

describe('SiteLogsCard', () => {
  beforeEach(() => {
    confirmSpy.mockReset()
    vi.mocked(api.fetchSiteLog).mockResolvedValue(logOf())
    vi.mocked(api.clearSiteLog).mockResolvedValue(logOf({ exists: true, size: 0, lines: [] }))
  })

  it('挂载即按默认类型与行数拉取尾部日志', async () => {
    const w = mountCard(['website:log:read'])
    await vi.waitFor(() => expect(api.fetchSiteLog).toHaveBeenCalled())
    expect(api.fetchSiteLog).toHaveBeenCalledWith('7', 'access', 200)
    await w.vm.$nextTick()
    expect(w.text()).toContain('GET /a 404')
    expect(w.text()).toContain('/www/wwwlogs/demo.log')
  })

  it('无读权限时不发请求，只提示无权访问', async () => {
    const w = mountCard([])
    await w.vm.$nextTick()
    expect(api.fetchSiteLog).not.toHaveBeenCalled()
    expect(w.text()).toContain(zh.error.forbidden)
  })

  it('切到错误日志会按新类型重新拉取', async () => {
    const w = mountCard(['website:log:read'])
    await vi.waitFor(() => expect(api.fetchSiteLog).toHaveBeenCalledTimes(1))

    await w.findAll('input.arco-radio-target')[1].setValue(true)
    await vi.waitFor(() => expect(api.fetchSiteLog).toHaveBeenCalledTimes(2))
    expect(api.fetchSiteLog).toHaveBeenLastCalledWith('7', 'error', 200)
  })

  it('日志开关关闭与截断分别给出提示', async () => {
    vi.mocked(api.fetchSiteLog).mockResolvedValue(logOf({ enabled: false }))
    const off = mountCard(['website:log:read'])
    await vi.waitFor(() => expect(off.text()).toContain(zh.website.logDisabledTip))

    vi.mocked(api.fetchSiteLog).mockResolvedValue(logOf({ truncated: true }))
    const cut = mountCard(['website:log:read'])
    await vi.waitFor(() => expect(cut.text()).toContain('仅显示最后 200 行'))
  })

  it('文件不存在时展示空态而不是报错', async () => {
    vi.mocked(api.fetchSiteLog).mockResolvedValue(logOf({ exists: false, size: 0, lines: [] }))
    const w = mountCard(['website:log:read'])
    await vi.waitFor(() => expect(w.text()).toContain(zh.website.logEmpty))
    expect(w.text()).not.toContain(zh.error.forbidden)
  })

  it('读取失败展示后端错误文案', async () => {
    vi.mocked(api.fetchSiteLog).mockRejectedValue(new BizError(100001, 'lines 需为正整数', 'trace'))
    const w = mountCard(['website:log:read'])
    await vi.waitFor(() => expect(w.text()).toContain('lines 需为正整数'))
  })

  it('清空按钮受权限控制，确认后才调用后端', async () => {
    const readonly = mountCard(['website:log:read'])
    await vi.waitFor(() => expect(api.fetchSiteLog).toHaveBeenCalled())
    expect(readonly.text()).not.toContain(zh.website.clearLog)

    const w = mountCard(['website:log:read', 'website:log:exec'])
    await vi.waitFor(() => expect(w.text()).toContain(zh.website.clearLog))

    const clearBtn = w.findAll('button').find((b) => b.text() === zh.website.clearLog)
    await clearBtn?.trigger('click')
    expect(confirmSpy).toHaveBeenCalledTimes(1)
    expect(api.clearSiteLog).not.toHaveBeenCalled()

    // 走一遍确认回调：清空后直接用返回值刷新，不需要再拉一次
    const before = vi.mocked(api.fetchSiteLog).mock.calls.length
    confirmSpy.mock.calls[0][0].onOk()
    await vi.waitFor(() => expect(api.clearSiteLog).toHaveBeenCalledWith('7', 'access'))
    expect(vi.mocked(api.fetchSiteLog).mock.calls.length).toBe(before)
    await vi.waitFor(() => expect(w.text()).toContain(zh.website.logEmpty))
  })
})
