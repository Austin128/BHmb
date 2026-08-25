import ArcoVue, { Select } from '@arco-design/web-vue'
import ArcoVueIcon from '@arco-design/web-vue/es/icon'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

import * as api from '@/api/website'
import type { SiteRewrite } from '@/api/website'
import zh from '@/locales/zh-CN'
import { useUserStore } from '@/store/modules/user'
import { BizError } from '@/types/api'
import SiteRewriteCard from '@/views/website/components/SiteRewriteCard.vue'

vi.mock('@/api/website', () => ({
  fetchSiteRewrite: vi.fn(),
  fetchRewriteTemplates: vi.fn(),
  saveSiteRewrite: vi.fn(),
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

const WP_BODY = 'location / {\n  try_files $uri $uri/ /index.php?$args;\n}'

function rewriteOf(patch: Partial<SiteRewrite> = {}): SiteRewrite {
  return {
    siteId: '7',
    name: 'demo',
    templateCode: 'none',
    content: '',
    enabled: false,
    file: '/etc/nginx/vhost/rewrite/demo.conf',
    updatedAt: 1_766_000_000_000,
    ...patch,
  }
}

function mountCard(permissions: string[], stopped = false) {
  setActivePinia(createPinia())
  useUserStore().permissions = permissions
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } })
  return mount(SiteRewriteCard, {
    props: { siteId: '7', stopped },
    global: { plugins: [ArcoVue, ArcoVueIcon, i18n] },
  })
}

/** 等首屏数据落地：DOM 要等 load() 的 Promise 链跑完才有表单。 */
async function waitReady(w: ReturnType<typeof mountCard>) {
  await vi.waitFor(() => expect(w.find('.arco-select').exists()).toBe(true))
}

/** 选择规则来源：Arco Select 的候选项渲染在 teleport 里，直接驱动组件事件更稳。 */
async function pickTemplate(w: ReturnType<typeof mountCard>, code: string) {
  const select = w.findComponent(Select)
  await select.setValue(code)
  select.vm.$emit('change', code)
  await w.vm.$nextTick()
}

describe('SiteRewriteCard', () => {
  beforeEach(() => {
    confirmSpy.mockReset()
    vi.mocked(api.fetchSiteRewrite).mockResolvedValue(rewriteOf())
    vi.mocked(api.fetchRewriteTemplates).mockResolvedValue({
      items: [
        { code: 'none', name: '不启用', content: '' },
        { code: 'wordpress', name: 'WordPress', content: WP_BODY },
      ],
    })
    vi.mocked(api.saveSiteRewrite).mockImplementation((_id, payload) =>
      Promise.resolve(rewriteOf(payload)),
    )
  })

  it('挂载即拉取当前规则与规则库，并展示落盘路径', async () => {
    const w = mountCard(['website:rewrite:read'])
    await vi.waitFor(() => expect(api.fetchSiteRewrite).toHaveBeenCalledWith('7'))
    expect(api.fetchRewriteTemplates).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => expect(w.text()).toContain('/etc/nginx/vhost/rewrite/demo.conf'))
    // none 时不展示正文编辑区，避免误以为有规则在跑
    expect(w.find('textarea').exists()).toBe(false)
  })

  it('无读权限不发请求，无写权限不出现保存按钮', async () => {
    const blind = mountCard([])
    await blind.vm.$nextTick()
    expect(api.fetchSiteRewrite).not.toHaveBeenCalled()
    expect(blind.text()).toContain(zh.error.forbidden)

    const readonly = mountCard(['website:rewrite:read'])
    await vi.waitFor(() => expect(api.fetchSiteRewrite).toHaveBeenCalled())
    await waitReady(readonly)
    expect(readonly.text()).not.toContain(zh.website.saveAndDeploy)
  })

  it('内置模板正文只读，提交时不回传正文由后端规则库决定', async () => {
    const w = mountCard(['website:rewrite:read', 'website:rewrite:update'], true)
    await vi.waitFor(() => expect(api.fetchSiteRewrite).toHaveBeenCalled())
    await waitReady(w)

    await pickTemplate(w, 'wordpress')
    const editor = w.find('textarea')
    expect(editor.element.value).toBe(WP_BODY)
    expect(editor.attributes('readonly')).toBeDefined()

    // 站点已停止时不弹确认框：不会触发下发，直接落库
    const save = w.findAll('button').find((b) => b.text() === zh.website.saveAndDeploy)
    await save?.trigger('click')
    expect(confirmSpy).not.toHaveBeenCalled()
    await vi.waitFor(() =>
      expect(api.saveSiteRewrite).toHaveBeenCalledWith('7', {
        templateCode: 'wordpress',
        content: '',
        enabled: true,
      }),
    )
  })

  it('自定义规则提交用户正文，运行中站点需二次确认', async () => {
    const w = mountCard(['website:rewrite:read', 'website:rewrite:update'])
    await vi.waitFor(() => expect(api.fetchSiteRewrite).toHaveBeenCalled())
    await waitReady(w)

    await pickTemplate(w, 'custom')
    const editor = w.find('textarea')
    expect(editor.attributes('readonly')).toBeUndefined()
    await editor.setValue('rewrite ^/old$ /new permanent;')

    const save = w.findAll('button').find((b) => b.text() === zh.website.saveAndDeploy)
    await save?.trigger('click')
    expect(confirmSpy).toHaveBeenCalledTimes(1)
    expect(api.saveSiteRewrite).not.toHaveBeenCalled()

    confirmSpy.mock.calls[0][0].onOk()
    await vi.waitFor(() =>
      expect(api.saveSiteRewrite).toHaveBeenCalledWith('7', {
        templateCode: 'custom',
        content: 'rewrite ^/old$ /new permanent;',
        enabled: true,
      }),
    )
    expect(w.emitted('deployed')).toHaveLength(1)
  })

  it('保存失败后重新拉取，界面不停留在被拒绝的草稿上', async () => {
    vi.mocked(api.saveSiteRewrite).mockRejectedValue(
      new BizError(300010, 'nginx -t 校验失败', 'trace'),
    )
    const w = mountCard(['website:rewrite:read', 'website:rewrite:update'], true)
    await vi.waitFor(() => expect(api.fetchSiteRewrite).toHaveBeenCalledTimes(1))
    await waitReady(w)

    await pickTemplate(w, 'custom')
    await w.find('textarea').setValue('bad rule')
    const save = w.findAll('button').find((b) => b.text() === zh.website.saveAndDeploy)
    await save?.trigger('click')

    await vi.waitFor(() => expect(api.fetchSiteRewrite).toHaveBeenCalledTimes(2))
    expect(w.emitted('deployed')).toBeUndefined()
    await vi.waitFor(() => expect(w.find('textarea').exists()).toBe(false))
  })
})
