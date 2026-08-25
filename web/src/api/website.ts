import { request } from '@/utils/request'

/* 类型与后端 internal/model/dto_website.go、internal/service/site 保持一致。 */

export type SiteType = 'static' | 'proxy'
export type SiteStatus = 'running' | 'stopped' | 'error' | 'deploying' | 'maintenance'
export type DeployStatus = 'pending' | 'deployed' | 'failed' | ''

export interface SiteDomain {
  /** 雪花 ID 以字符串传输，避开 JS 整数精度限制；创建时不传 */
  id?: string
  domain: string
  port: number
  isPrimary?: boolean
  isWildcard?: boolean
}

export interface SiteSummary {
  id: string
  name: string
  type: SiteType
  status: SiteStatus
  serverType: string
  primaryDomain: string
  domains: SiteDomain[]
  rootPath: string
  proxyTarget: string
  sslEnabled: boolean
  groupName: string
  remark: string
  configVersion: number
  deployStatus: DeployStatus
  deployError: string
  /** Unix 毫秒；0 表示永不过期 */
  expireAt: number
  createdAt: number
  updatedAt: number
}

export interface SiteDetail extends SiteSummary {
  indexFiles: string[]
  accessLogEnabled: boolean
  errorLogEnabled: boolean
  accessLogPath: string
  errorLogPath: string
  proxyHost: string
  proxyWebSocket: boolean
  vhostFile: string
  rootExists: boolean
}

export interface SiteListResult {
  items: SiteSummary[]
  total: number
  page: number
  size: number
}

/** 建站前提：宿主机 Web 服务状态与真实支持的站点类型。 */
export interface WebsiteEnv {
  installed: boolean
  kind: string
  bin: string
  version: string
  vhostDir: string
  /** null 表示 nginx -T 不可用，无法判断主配置是否 include 面板目录 */
  vhostIncluded: boolean | null
  configOK: boolean
  message: string
  wwwRoot: string
  logDir: string
  supportedTypes: SiteType[]
  siteCount: number
}

export interface CreateSitePayload {
  name: string
  type: SiteType
  domains: SiteDomain[]
  root?: string
  indexFiles?: string[]
  remark?: string
  groupName?: string
  expireAt?: number
  proxyTarget?: string
  proxyHost?: string
  proxyWebSocket?: boolean
  createDefaultPage?: boolean
  accessLogEnabled?: boolean
  errorLogEnabled?: boolean
}

export interface UpdateSitePayload {
  domains?: SiteDomain[]
  root?: string
  indexFiles?: string[]
  remark?: string
  groupName?: string
  expireAt?: number
  proxyTarget?: string
  proxyHost?: string
  proxyWebSocket?: boolean
  accessLogEnabled?: boolean
  errorLogEnabled?: boolean
}

export interface SiteConfigVersion {
  version: number
  source: string
  isCurrent: boolean
  deployStatus: DeployStatus
  deployError: string
  contentHash: string
  changeSummary: string
  createdAt: number
  deployAt: number
}

export interface SiteConfig {
  siteId: string
  name: string
  file: string
  version: number
  content: string
  versions: SiteConfigVersion[]
}

// 网站模块的失败提示由页面自己给出（列表用告警区、详情用 Message），
// 因此统一带 silent，避免请求层再弹一条重复的全局提示。
export function fetchWebsiteEnv() {
  return request<WebsiteEnv>({ url: '/website/env', method: 'get', silent: true })
}

export function fetchSites(params: { page?: number; pageSize?: number; keyword?: string }) {
  return request<SiteListResult>({ url: '/website/sites', method: 'get', params, silent: true })
}

export function fetchSite(id: string) {
  return request<SiteDetail>({ url: `/website/sites/${id}`, method: 'get', silent: true })
}

export function createSite(payload: CreateSitePayload) {
  return request<SiteDetail>({ url: '/website/sites', method: 'post', data: payload, silent: true })
}

export function updateSite(id: string, payload: UpdateSitePayload) {
  return request<SiteDetail>({
    url: `/website/sites/${id}`,
    method: 'put',
    data: payload,
    silent: true,
  })
}

export function changeSiteStatus(id: string, target: Extract<SiteStatus, 'running' | 'stopped'>) {
  return request<SiteDetail>({
    url: `/website/sites/${id}/status`,
    method: 'post',
    data: { target },
    silent: true,
  })
}

export function rebuildSite(id: string) {
  return request<SiteDetail>({ url: `/website/sites/${id}/rebuild`, method: 'post', silent: true })
}

export function deleteSite(id: string, deleteRoot: boolean) {
  return request<{ deleted: boolean; rootRecycled: boolean }>({
    url: `/website/sites/${id}`,
    method: 'delete',
    params: { deleteRoot: deleteRoot ? 'true' : 'false' },
    silent: true,
  })
}

export function fetchSiteConfig(id: string) {
  return request<SiteConfig>({ url: `/website/sites/${id}/config`, method: 'get', silent: true })
}

export function saveSiteConfig(id: string, content: string, version: number) {
  return request<SiteConfig>({
    url: `/website/sites/${id}/config`,
    method: 'put',
    data: { content, version },
    silent: true,
  })
}

export function rollbackSiteConfig(id: string, version: number) {
  return request<SiteConfig>({
    url: `/website/sites/${id}/config/rollback/${version}`,
    method: 'post',
    silent: true,
  })
}

export function reloadWebServer() {
  return request<{ reloaded: boolean }>({
    url: '/website/nginx/reload',
    method: 'post',
    silent: true,
  })
}

export function testWebServer() {
  return request<{ ok: boolean; output: string }>({
    url: '/website/nginx/test',
    method: 'post',
    silent: true,
  })
}

/** 静态缓存档位，与后端 model.CacheProfile* 一致。 */
export type CacheProfile = 'none' | 'hour' | 'day' | 'month' | 'year'

export interface AntiLeechSetting {
  enabled: boolean
  extensions: string[]
  allowedReferers: string[]
  allowEmptyReferer: boolean
  returnCode: number
}

export interface AccessControlSetting {
  enabled: boolean
  allowIps: string[]
  denyIps: string[]
  denyUserAgents: string[]
}

export interface RedirectRule {
  enabled: boolean
  path: string
  target: string
  code: number
  keepPath: boolean
  keepQuery: boolean
  remark: string
}

export interface RateLimitSetting {
  enabled: boolean
  bandwidthKb: number
  burstKb: number
}

export interface SiteSettings {
  siteId: string
  name: string
  type: SiteType
  indexFiles: string[]
  runPath: string
  autoIndex: boolean
  cacheProfile: CacheProfile
  antiLeech: AntiLeechSetting
  accessControl: AccessControlSetting
  redirects: RedirectRule[]
  rateLimit: RateLimitSetting
  accessLogEnabled: boolean
  errorLogEnabled: boolean
  accessLogPath: string
  errorLogPath: string
  rootPath: string
  /** 后端给出的可选缓存档位，前端不写死枚举 */
  cacheProfiles: CacheProfile[]
}

/** 保存为整体替换语义：一次提交完整状态，避免部分更新的歧义。 */
export type SaveSiteSettingsPayload = Omit<
  SiteSettings,
  'siteId' | 'name' | 'type' | 'accessLogPath' | 'errorLogPath' | 'rootPath' | 'cacheProfiles'
>

export interface SiteRewrite {
  siteId: string
  name: string
  /** 内置模板代码、custom 或 none */
  templateCode: string
  content: string
  enabled: boolean
  /** 片段落盘路径，便于用户核对 */
  file: string
  updatedAt: number
}

export interface SiteRewriteTemplate {
  code: string
  name: string
  content: string
}

export function fetchSiteSettings(id: string) {
  return request<SiteSettings>({ url: `/website/sites/${id}/settings`, method: 'get', silent: true })
}

export function saveSiteSettings(id: string, payload: SaveSiteSettingsPayload) {
  return request<SiteSettings>({
    url: `/website/sites/${id}/settings`,
    method: 'put',
    data: payload,
    silent: true,
  })
}

export function fetchRewriteTemplates() {
  return request<{ items: SiteRewriteTemplate[] }>({
    url: '/website/rewrite/templates',
    method: 'get',
    silent: true,
  })
}

export function fetchSiteRewrite(id: string) {
  return request<SiteRewrite>({ url: `/website/sites/${id}/rewrite`, method: 'get', silent: true })
}

export function saveSiteRewrite(
  id: string,
  payload: { templateCode: string; content: string; enabled: boolean },
) {
  return request<SiteRewrite>({
    url: `/website/sites/${id}/rewrite`,
    method: 'put',
    data: payload,
    silent: true,
  })
}

export type SiteLogType = 'access' | 'error'

export interface SiteLog {
  siteId: string
  name: string
  type: SiteLogType
  path: string
  /** 站点设置里的日志开关；关闭时文件通常不存在 */
  enabled: boolean
  exists: boolean
  size: number
  modifiedAt: number
  lines: string[]
  /** 真表示只返回了尾部，文件比窗口更长 */
  truncated: boolean
}

export function fetchSiteLog(id: string, type: SiteLogType, lines: number) {
  return request<SiteLog>({
    url: `/website/sites/${id}/logs`,
    method: 'get',
    params: { type, lines },
    silent: true,
  })
}

export function clearSiteLog(id: string, type: SiteLogType) {
  return request<SiteLog>({
    url: `/website/sites/${id}/logs/clear`,
    method: 'post',
    params: { type },
    silent: true,
  })
}
