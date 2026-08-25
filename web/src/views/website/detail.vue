<script setup lang="ts">
/**
 * 站点详情页。左侧为基础信息与域名编辑，右侧为 vhost 配置正文与版本历史。
 * 配置保存前展示与当前版本的差异摘要；回滚同样走后端的「渲染 → 校验 → 原子替换 → reload」链路，
 * 因此这里不做任何本地伪装成功。
 */
import { computed, onMounted, ref } from 'vue'
import dayjs from 'dayjs'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { Message, Modal } from '@arco-design/web-vue'

import {
  changeSiteStatus,
  fetchSite,
  fetchSiteConfig,
  rebuildSite,
  rollbackSiteConfig,
  saveSiteConfig,
  updateSite,
  type SiteConfig,
  type SiteDetail,
  type SiteDomain,
} from '@/api/website'
import SiteLogsCard from './components/SiteLogsCard.vue'
import SiteRewriteCard from './components/SiteRewriteCard.vue'
import SiteSettingsCard from './components/SiteSettingsCard.vue'
import { BizError } from '@/types/api'
import { useUserStore } from '@/store/modules/user'
import { errorText } from '@/utils/errorText'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const siteId = computed(() => String(route.params.id ?? ''))

const canUpdate = computed(() => userStore.hasPermission('website:site:update'))
const canExec = computed(() => userStore.hasPermission('website:site:exec'))
const canReadConfig = computed(() => userStore.hasPermission('website:config:read'))
const canWriteConfig = computed(() => userStore.hasPermission('website:config:update'))

const detail = ref<SiteDetail | null>(null)
const loading = ref(true)
const loadError = ref('')

const config = ref<SiteConfig | null>(null)
const configLoading = ref(false)
const configError = ref('')
const draft = ref('')
const saving = ref(false)

/** 域名编辑草稿：整体替换语义，保存时一次性提交。 */
const domains = ref<SiteDomain[]>([])
const remark = ref('')
const savingBasic = ref(false)

const dirtyConfig = computed(() => Boolean(config.value) && draft.value !== config.value?.content)

/** 停用站点的设置只落库，卡片据此提示用户不会立即生效。 */
const stopped = computed(() => detail.value?.status === 'stopped')

/** 设置或伪静态下发成功后 vhost 与版本历史都变了，跟着刷新。 */
async function onChildDeployed() {
  await loadDetail()
  await loadConfig()
}

function messageOf(e: unknown): string {
  if (e instanceof BizError) return errorText(e.code, e.message)
  return e instanceof Error ? e.message : t('common.requestFailed')
}

function reportError(e: unknown) {
  Message.error(messageOf(e))
}

function syncDraftFromDetail(d: SiteDetail) {
  domains.value = d.domains.map((item) => ({ ...item }))
  remark.value = d.remark
}

async function loadDetail() {
  loading.value = true
  try {
    const d = await fetchSite(siteId.value)
    detail.value = d
    syncDraftFromDetail(d)
    loadError.value = ''
  } catch (e) {
    loadError.value = messageOf(e)
    detail.value = null
  } finally {
    loading.value = false
  }
}

async function loadConfig() {
  if (!canReadConfig.value) return
  configLoading.value = true
  try {
    const c = await fetchSiteConfig(siteId.value)
    config.value = c
    draft.value = c.content
    configError.value = ''
  } catch (e) {
    configError.value = messageOf(e)
    config.value = null
  } finally {
    configLoading.value = false
  }
}

function addDomain() {
  domains.value.push({ domain: '', port: 80 })
}

function removeDomain(index: number) {
  if (domains.value.length <= 1) return
  domains.value.splice(index, 1)
}

async function saveBasic() {
  if (!detail.value) return
  savingBasic.value = true
  try {
    const d = await updateSite(siteId.value, {
      domains: domains.value
        .filter((item) => item.domain.trim())
        .map((item) => ({ domain: item.domain.trim(), port: Number(item.port) || 80 })),
      remark: remark.value.trim(),
    })
    detail.value = d
    syncDraftFromDetail(d)
    Message.success(t('website.saved'))
    await loadConfig()
  } catch (e) {
    reportError(e)
  } finally {
    savingBasic.value = false
  }
}

async function saveConfig() {
  if (!config.value) return
  const ok = await confirmModal(t('website.confirmSaveConfig'), t('website.confirmSaveConfigTip'))
  if (!ok) return
  saving.value = true
  try {
    const c = await saveSiteConfig(siteId.value, draft.value, config.value.version)
    config.value = c
    draft.value = c.content
    Message.success(t('website.configSaved', { version: c.version }))
    await loadDetail()
  } catch (e) {
    reportError(e)
  } finally {
    saving.value = false
  }
}

async function rollback(version: number) {
  const ok = await confirmModal(
    t('website.confirmRollback'),
    t('website.confirmRollbackTip', { version }),
  )
  if (!ok) return
  try {
    const c = await rollbackSiteConfig(siteId.value, version)
    config.value = c
    draft.value = c.content
    Message.success(t('website.rolledBack', { version }))
    await loadDetail()
  } catch (e) {
    reportError(e)
  }
}

async function toggleStatus() {
  const d = detail.value
  if (!d) return
  const target = d.status === 'running' ? 'stopped' : 'running'
  if (target === 'stopped') {
    const ok = await confirmModal(
      t('website.confirmStopTitle'),
      t('website.confirmStopTip', { name: d.name }),
    )
    if (!ok) return
  }
  try {
    detail.value = await changeSiteStatus(siteId.value, target)
    Message.success(t('website.statusChanged'))
    await loadConfig()
  } catch (e) {
    reportError(e)
  }
}

async function rebuild() {
  try {
    detail.value = await rebuildSite(siteId.value)
    Message.success(t('website.rebuilt'))
    await loadConfig()
  } catch (e) {
    reportError(e)
  }
}

function confirmModal(title: string, content: string): Promise<boolean> {
  return new Promise((resolve) => {
    Modal.confirm({
      title,
      content,
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      onOk: () => resolve(true),
      onCancel: () => resolve(false),
    })
  })
}

function statusColor(status: string) {
  switch (status) {
    case 'running':
      return 'green'
    case 'stopped':
      return 'gray'
    case 'error':
      return 'red'
    case 'deploying':
      return 'blue'
    default:
      return 'orange'
  }
}

function statusText(status: string) {
  const key = `website.status.${status}`
  return t(key) === key ? status : t(key)
}

/** 下发状态与配置来源都是后端枚举，缺少映射时退回原值而不是显示空白。 */
function deployStatusText(status: string) {
  const key = `website.deployStatus.${status}`
  return t(key) === key ? status : t(key)
}

function sourceText(source: string) {
  const key = `website.configSource.${source}`
  return t(key) === key ? source : t(key)
}

function fmtTime(ms: number) {
  return ms ? dayjs(ms).format('YYYY-MM-DD HH:mm:ss') : '-'
}

function back() {
  void router.push({ name: 'Website' })
}

onMounted(async () => {
  await loadDetail()
  await loadConfig()
})
</script>

<template>
  <div class="site-detail">
    <a-card :bordered="false">
      <template #title>
        <a-space>
          <a-button size="mini" @click="back">
            <template #icon><icon-left /></template>
          </a-button>
          <span>{{ detail?.name || $t('menu.website') }}</span>
          <a-tag v-if="detail" :color="statusColor(detail.status)">
            {{ statusText(detail.status) }}
          </a-tag>
        </a-space>
      </template>
      <template #extra>
        <a-space>
          <a-button v-if="canExec && detail" size="small" @click="toggleStatus">
            {{ detail.status === 'running' ? $t('website.actionStop') : $t('website.actionStart') }}
          </a-button>
          <a-button v-if="canExec && detail" size="small" @click="rebuild">
            {{ $t('website.actionRebuild') }}
          </a-button>
        </a-space>
      </template>

      <a-alert v-if="loadError" type="error">{{ loadError }}</a-alert>

      <a-skeleton v-else-if="loading" animation>
        <a-skeleton-line :rows="4" />
      </a-skeleton>

      <template v-else-if="detail">
        <a-alert v-if="detail.deployError" type="error" class="site-detail__alert">
          {{ detail.deployError }}
        </a-alert>
        <a-alert v-else-if="!detail.rootExists" type="warning" class="site-detail__alert">
          {{ $t('website.rootMissing', { root: detail.rootPath }) }}
        </a-alert>

        <a-descriptions :column="2" size="small" bordered class="site-detail__desc">
          <a-descriptions-item :label="$t('website.fieldType')">
            {{ detail.type === 'proxy' ? $t('website.typeProxy') : $t('website.typeStatic') }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('website.colVersion')">
            v{{ detail.configVersion }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('website.fieldRoot')">
            {{ detail.rootPath }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('website.vhostFile')">
            {{ detail.vhostFile }}
          </a-descriptions-item>
          <a-descriptions-item v-if="detail.type === 'proxy'" :label="$t('website.fieldProxyTarget')">
            {{ detail.proxyTarget }}
          </a-descriptions-item>
          <a-descriptions-item v-if="detail.type === 'proxy'" :label="$t('website.fieldWebSocket')">
            {{ detail.proxyWebSocket ? $t('common.yes') : $t('common.no') }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('website.accessLog')">
            {{ detail.accessLogEnabled ? detail.accessLogPath : $t('website.logDisabled') }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('website.errorLog')">
            {{ detail.errorLogEnabled ? detail.errorLogPath : $t('website.logDisabled') }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('website.indexFiles')">
            {{ detail.indexFiles.join(', ') || '-' }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('website.updatedAt')">
            {{ fmtTime(detail.updatedAt) }}
          </a-descriptions-item>
        </a-descriptions>
      </template>
    </a-card>

    <div v-if="detail" class="site-detail__grid">
      <a-card :bordered="false" :title="$t('website.domainsAndRemark')">
        <a-form :model="{ domains, remark }" layout="vertical" size="small" :disabled="!canUpdate">
          <a-form-item :label="$t('website.fieldDomains')">
            <div class="site-detail__domains">
              <div v-for="(d, i) in domains" :key="i" class="site-detail__domain-row">
                <a-input v-model="d.domain" :placeholder="$t('website.fieldDomainTip')" />
                <a-input-number
                  v-model="d.port"
                  :min="1"
                  :max="65535"
                  :style="{ width: '110px' }"
                />
                <a-button size="mini" :disabled="domains.length <= 1" @click="removeDomain(i)">
                  <template #icon><icon-minus /></template>
                </a-button>
              </div>
              <a-button size="mini" @click="addDomain">
                <template #icon><icon-plus /></template>
                {{ $t('website.addDomain') }}
              </a-button>
            </div>
          </a-form-item>
          <a-form-item :label="$t('website.fieldRemark')">
            <a-textarea
              v-model="remark"
              :max-length="255"
              show-word-limit
              :auto-size="{ minRows: 2 }"
            />
          </a-form-item>
          <a-form-item v-if="canUpdate">
            <a-button type="primary" size="small" :loading="savingBasic" @click="saveBasic">
              {{ $t('website.saveAndDeploy') }}
            </a-button>
          </a-form-item>
        </a-form>
      </a-card>

      <a-card :bordered="false" :title="$t('website.configTitle')">
        <template #extra>
          <a-space>
            <span v-if="config" class="site-detail__version">v{{ config.version }}</span>
            <a-button size="small" :loading="configLoading" @click="loadConfig">
              <template #icon><icon-refresh /></template>
            </a-button>
            <a-button
              v-if="canWriteConfig"
              type="primary"
              size="small"
              :loading="saving"
              :disabled="!dirtyConfig"
              @click="saveConfig"
            >
              {{ $t('website.saveConfig') }}
            </a-button>
          </a-space>
        </template>

        <a-alert v-if="!canReadConfig" type="info">{{ $t('error.forbidden') }}</a-alert>
        <a-alert v-else-if="configError" type="error">{{ configError }}</a-alert>
        <a-skeleton v-else-if="configLoading && !config" animation>
          <a-skeleton-line :rows="6" />
        </a-skeleton>
        <template v-else-if="config">
          <a-textarea
            v-model="draft"
            class="site-detail__editor"
            :auto-size="{ minRows: 14, maxRows: 26 }"
            :disabled="!canWriteConfig"
            spellcheck="false"
          />
          <p class="site-detail__file">{{ config.file }}</p>

          <!-- 版本历史用列表而不是表格：配置卡片在窄屏会被压到 260px，
               固定列宽的表格会出现横向滚动条，滚动条还会遮住回滚链接。 -->
          <ul class="site-detail__versions">
            <li v-for="item in config.versions" :key="item.version" class="site-detail__version-row">
              <span class="site-detail__version-no">v{{ item.version }}</span>
              <a-tag
                size="small"
                :color="
                  item.deployStatus === 'failed' ? 'red' : item.isCurrent ? 'green' : 'gray'
                "
              >
                {{ item.isCurrent ? $t('website.current') : deployStatusText(item.deployStatus) }}
              </a-tag>
              <span class="site-detail__version-meta">{{ sourceText(item.source) }}</span>
              <span class="site-detail__version-meta">{{ fmtTime(item.createdAt) }}</span>
              <a-link
                v-if="canWriteConfig && !item.isCurrent"
                class="site-detail__version-action"
                @click="rollback(item.version)"
              >
                {{ $t('website.rollback') }}
              </a-link>
            </li>
          </ul>
        </template>
      </a-card>
    </div>

    <div v-if="detail" class="site-detail__grid site-detail__grid--wide">
      <SiteSettingsCard
        :site-id="siteId"
        :site-type="detail.type"
        :stopped="stopped"
        @deployed="onChildDeployed"
      />
      <SiteRewriteCard :site-id="siteId" :stopped="stopped" @deployed="onChildDeployed" />
    </div>

    <SiteLogsCard v-if="detail" :site-id="siteId" />
  </div>
</template>

<style lang="scss" scoped>
.site-detail {
  display: flex;
  flex-direction: column;
  gap: 16px;

  &__alert {
    margin-bottom: 12px;
  }

  &__desc {
    word-break: break-all;
  }

  &__grid {
    display: grid;
    grid-template-columns: minmax(0, 360px) minmax(0, 1fr);
    gap: 16px;

    // 设置与伪静态都是表单密集型卡片，等宽排布比左窄右宽更好用。
    &--wide {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  &__domains {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
  }

  &__domain-row {
    display: flex;
    gap: 8px;
  }

  &__editor {
    font-family: var(--font-mono, 'SFMono-Regular', Menlo, Consolas, monospace);
    font-size: 12px;
  }

  &__file {
    margin: 8px 0 0;
    color: var(--color-text-3);
    font-size: 12px;
    word-break: break-all;
  }

  &__version {
    color: var(--color-text-3);
    font-size: 12px;
  }

  &__versions {
    margin: 12px 0 0;
    padding: 0;
    list-style: none;
    border-top: 1px solid var(--color-border-2);
  }

  &__version-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 8px 0;
    border-bottom: 1px solid var(--color-border-2);
    font-size: 12px;
  }

  &__version-no {
    min-width: 32px;
    font-weight: 600;
  }

  &__version-meta {
    color: var(--color-text-3);
  }

  &__version-action {
    margin-left: auto;
  }
}

@media (max-width: 1080px) {
  .site-detail__grid,
  .site-detail__grid--wide {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
