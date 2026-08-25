import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  changeSiteStatus,
  clearSiteLog,
  createSite,
  deleteSite,
  fetchRewriteTemplates,
  fetchSiteLog,
  fetchSiteSettings,
  fetchSites,
  rollbackSiteConfig,
  saveSiteConfig,
  saveSiteRewrite,
  saveSiteSettings,
} from '@/api/website'
import { request } from '@/utils/request'

vi.mock('@/utils/request', () => ({ request: vi.fn() }))

const mocked = vi.mocked(request)

function lastConfig() {
  return mocked.mock.calls.at(-1)?.[0]
}

describe('website api', () => {
  beforeEach(() => {
    mocked.mockReset()
    mocked.mockResolvedValue({} as never)
  })

  it('所有网站请求都带 silent，失败提示交给页面', async () => {
    await fetchSites({ page: 1, pageSize: 20 })
    expect(lastConfig()).toMatchObject({ url: '/website/sites', method: 'get', silent: true })

    await createSite({ name: 'demo', type: 'static', domains: [{ domain: 'a.com', port: 80 }] })
    expect(lastConfig()).toMatchObject({ url: '/website/sites', method: 'post', silent: true })
  })

  it('分页与关键字作为 query 传递', async () => {
    await fetchSites({ page: 2, pageSize: 50, keyword: 'shop' })
    expect(lastConfig()?.params).toEqual({ page: 2, pageSize: 50, keyword: 'shop' })
  })

  it('雪花 ID 原样拼进路径，不做数值转换', async () => {
    const id = '349576588838305792'
    await changeSiteStatus(id, 'stopped')
    expect(lastConfig()?.url).toBe(`/website/sites/${id}/status`)
    expect(lastConfig()?.data).toEqual({ target: 'stopped' })
  })

  it('删除按后端约定传字符串布尔的 deleteRoot', async () => {
    await deleteSite('12', true)
    expect(lastConfig()).toMatchObject({ method: 'delete', params: { deleteRoot: 'true' } })

    await deleteSite('12', false)
    expect(lastConfig()?.params).toEqual({ deleteRoot: 'false' })
  })

  it('保存配置带乐观版本号，回滚把版本放在路径上', async () => {
    await saveSiteConfig('12', 'server {}', 3)
    expect(lastConfig()).toMatchObject({
      url: '/website/sites/12/config',
      method: 'put',
      data: { content: 'server {}', version: 3 },
    })

    await rollbackSiteConfig('12', 2)
    expect(lastConfig()).toMatchObject({
      url: '/website/sites/12/config/rollback/2',
      method: 'post',
    })
  })

  it('高级设置读写落在同一路径，保存整体替换', async () => {
    await fetchSiteSettings('12')
    expect(lastConfig()).toMatchObject({
      url: '/website/sites/12/settings',
      method: 'get',
      silent: true,
    })

    const payload = {
      indexFiles: ['index.html'],
      runPath: '/public',
      autoIndex: true,
      cacheProfile: 'day' as const,
      antiLeech: {
        enabled: true,
        extensions: ['png'],
        allowedReferers: ['*.example.com'],
        allowEmptyReferer: true,
        returnCode: 403,
      },
      accessControl: { enabled: false, allowIps: [], denyIps: [], denyUserAgents: [] },
      redirects: [],
      rateLimit: { enabled: false, bandwidthKb: 0, burstKb: 0 },
      accessLogEnabled: true,
      errorLogEnabled: true,
    }
    await saveSiteSettings('12', payload)
    expect(lastConfig()).toMatchObject({
      url: '/website/sites/12/settings',
      method: 'put',
      data: payload,
    })
  })

  it('伪静态规则库与站点规则分开取，保存带模板代码与启用位', async () => {
    await fetchRewriteTemplates()
    expect(lastConfig()).toMatchObject({ url: '/website/rewrite/templates', method: 'get' })

    await saveSiteRewrite('12', { templateCode: 'custom', content: 'rewrite ^/a$ /b last;', enabled: true })
    expect(lastConfig()).toMatchObject({
      url: '/website/sites/12/rewrite',
      method: 'put',
      data: { templateCode: 'custom', content: 'rewrite ^/a$ /b last;', enabled: true },
    })
  })

  it('日志按类型与行数取尾部，清空是独立的 POST', async () => {
    await fetchSiteLog('12', 'error', 500)
    expect(lastConfig()).toMatchObject({
      url: '/website/sites/12/logs',
      method: 'get',
      params: { type: 'error', lines: 500 },
      silent: true,
    })

    await clearSiteLog('12', 'access')
    expect(lastConfig()).toMatchObject({
      url: '/website/sites/12/logs/clear',
      method: 'post',
      params: { type: 'access' },
    })
  })
})
