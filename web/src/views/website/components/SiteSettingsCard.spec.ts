import ArcoVue from '@arco-design/web-vue'
import ArcoVueIcon from '@arco-design/web-vue/es/icon'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

import * as api from '@/api/website'
import type { SiteSettings } from '@/api/website'
import zh from '@/locales/zh-CN'
import { useUserStore } from '@/store/modules/user'
import { BizError } from '@/types/api'
import SiteSettingsCard from '@/views/website/components/SiteSettingsCard.vue'

vi.mock('@/api/website', () => ({
  fetchSiteSettings: vi.fn(),
  saveSiteSettings: vi.fn(),
}))

const confirmSpy = vi.hoisted(() => vi.fn())
vi.mock('@arco-design/web-vue', async (orig) => {
  const actual = (await orig()) as Record<string, unknown>
  return {
    ...actual,
    Modal: { confirm: confirmSpy },
    Message: { success: vi.fn(), error: vi.fn() },
  }
})

function settingsOf(patch: Partial<SiteSettings> = {}): SiteSettings {
  return {
    siteId: '7',
    name: 'demo',
    type: 'static',
    indexFiles: ['index.html'],
    runPath: '',
    autoIndex: false,
    cacheProfile: 'none',
    antiLeech: {
      enabled: false,
      extensions: [],
      allowedReferers: [],
      allowEmptyReferer: true,
      returnCode: 403,
    },
    accessControl: { enabled: false, allowIps: [], denyIps: [], denyUserAgents: [] },
    redirects: [],
    rateLimit: { enabled: false, bandwidthKb: 0, burstKb: 0 },
    accessLogEnabled: true,
    errorLogEnabled: true,
    accessLogPath: '/www/wwwlogs/demo.log',
    errorLogPath: '/www/wwwlogs/demo.error.log',
    rootPath: '/www/wwwroot/demo',
    cacheProfiles: ['none', 'hour', 'day', 'month', 'year'],
    ...patch,
  }
}

function mountCard(permissions: string[], stopped = false) {
  setActivePinia(createPinia())
  useUserStore().permissions = permissions
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } })
  return mount(SiteSettingsCard, {
    props: { siteId: '7', siteType: 'static', stopped },
    global: { plugins: [ArcoVue, ArcoVueIcon, i18n] },
  })
}

type Card = ReturnType<typeof mountCard>

async function waitReady(w: Card) {
  await vi.waitFor(() => expect(w.find('input[placeholder="/public"]').exists()).toBe(true))
}

function buttonByText(w: Card, text: string) {
  return w.findAll('button').find((b) => b.text() === text)
}

/** 重定向面板默认折叠，需要先展开才会渲染规则编辑区。 */
async function expandRedirectPanel(w: Card) {
  const header = w
    .findAll('.arco-collapse-item-header-title')
    .find((h) => h.text() === zh.website.settingsRedirect)
  await header?.trigger('click')
  await w.vm.$nextTick()
}

describe('SiteSettingsCard', () => {
  beforeEach(() => {
    confirmSpy.mockReset()
    vi.mocked(api.fetchSiteSettings).mockResolvedValue(settingsOf())
    vi.mocked(api.saveSiteSettings).mockImplementation((_id, payload) =>
      Promise.resolve(settingsOf(payload)),
    )
  })

  it('表单状态全部来自后端，并展示站点根目录', async () => {
    const w = mountCard(['website:setting:read'])
    await vi.waitFor(() => expect(api.fetchSiteSettings).toHaveBeenCalledWith('7'))
    await waitReady(w)
    expect(w.text()).toContain('/www/wwwroot/demo')
    // 缓存档位来自后端 cacheProfiles，不在前端写死
    expect(w.text()).toContain(zh.website.cacheProfile)
  })

  it('无读权限不发请求，无写权限不出现保存按钮', async () => {
    const blind = mountCard([])
    await blind.vm.$nextTick()
    expect(api.fetchSiteSettings).not.toHaveBeenCalled()
    expect(blind.text()).toContain(zh.error.forbidden)

    const readonly = mountCard(['website:setting:read'])
    await waitReady(readonly)
    expect(buttonByText(readonly, zh.website.saveAndDeploy)).toBeUndefined()
    // 只读时也不给「添加重定向」入口
    await expandRedirectPanel(readonly)
    expect(readonly.text()).toContain(zh.website.redirectEmpty)
    expect(buttonByText(readonly, zh.website.addRedirect)).toBeUndefined()
  })

  it('提交时整体替换：运行路径去空格，未填完的重定向被丢弃', async () => {
    const w = mountCard(['website:setting:read', 'website:setting:update'], true)
    await waitReady(w)

    await w.find('input[placeholder="/public"]').setValue('  /public  ')
    await expandRedirectPanel(w)
    await vi.waitFor(() => expect(buttonByText(w, zh.website.addRedirect)).toBeTruthy())

    // 第一条填完整，第二条留空：后者不该发给后端
    await buttonByText(w, zh.website.addRedirect)?.trigger('click')
    await buttonByText(w, zh.website.addRedirect)?.trigger('click')
    const paths = w.findAll(`input[placeholder="${zh.website.redirectPath}"]`)
    const targets = w.findAll(`input[placeholder="${zh.website.redirectTarget}"]`)
    expect(paths).toHaveLength(2)
    await paths[0].setValue(' /old ')
    await targets[0].setValue(' https://new.example.com ')

    // 站点已停止：只落库不下发，因此不弹确认框
    await buttonByText(w, zh.website.saveAndDeploy)?.trigger('click')
    expect(confirmSpy).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(api.saveSiteSettings).toHaveBeenCalled())

    const payload = vi.mocked(api.saveSiteSettings).mock.calls[0][1]
    expect(payload.runPath).toBe('/public')
    expect(payload.redirects).toEqual([
      {
        enabled: true,
        path: '/old',
        target: 'https://new.example.com',
        code: 301,
        keepPath: true,
        keepQuery: false,
        remark: '',
      },
    ])
    expect(w.emitted('deployed')).toHaveLength(1)
  })

  it('运行中站点保存前需二次确认', async () => {
    const w = mountCard(['website:setting:read', 'website:setting:update'])
    await waitReady(w)

    await buttonByText(w, zh.website.saveAndDeploy)?.trigger('click')
    expect(confirmSpy).toHaveBeenCalledTimes(1)
    expect(api.saveSiteSettings).not.toHaveBeenCalled()

    confirmSpy.mock.calls[0][0].onOk()
    await vi.waitFor(() => expect(api.saveSiteSettings).toHaveBeenCalledTimes(1))
  })

  it('保存被后端拒绝时保留草稿，不静默回滚用户输入', async () => {
    vi.mocked(api.saveSiteSettings).mockRejectedValue(
      new BizError(100001, '运行目录必须在站点根目录内', 'trace'),
    )
    const w = mountCard(['website:setting:read', 'website:setting:update'], true)
    await waitReady(w)

    await w.find('input[placeholder="/public"]').setValue('/etc')
    await buttonByText(w, zh.website.saveAndDeploy)?.trigger('click')
    await vi.waitFor(() => expect(api.saveSiteSettings).toHaveBeenCalledTimes(1))

    expect(w.emitted('deployed')).toBeUndefined()
    expect(api.fetchSiteSettings).toHaveBeenCalledTimes(1)
    expect(w.find<HTMLInputElement>('input[placeholder="/public"]').element.value).toBe('/etc')
  })
})
