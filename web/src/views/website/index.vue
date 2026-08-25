<script setup lang="ts">
/**
 * 网站列表页。顶部为 Web 服务前置状态条，主体为站点表格。
 * 所有操作都对应真实后端能力：创建、启停、重建下发、删除、查看配置。
 * 未检测到 Nginx/OpenResty 时创建入口禁用并直接给出原因，
 * 而不是先落一条数据库记录再假装站点已上线。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import dayjs from 'dayjs'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Message, Modal } from '@arco-design/web-vue'

import {
  changeSiteStatus,
  createSite,
  deleteSite,
  fetchSites,
  fetchWebsiteEnv,
  rebuildSite,
  reloadWebServer,
  testWebServer,
  type CreateSitePayload,
  type SiteDomain,
  type SiteSummary,
  type SiteType,
  type WebsiteEnv,
} from '@/api/website'
import { BizError } from '@/types/api'
import { useUserStore } from '@/store/modules/user'
import { errorText } from '@/utils/errorText'

const { t } = useI18n()
const router = useRouter()
const userStore = useUserStore()

const canRead = computed(() => userStore.hasPermission('website:site:read'))
const canCreate = computed(() => userStore.hasPermission('website:site:create'))
const canDelete = computed(() => userStore.hasPermission('website:site:delete'))
const canExec = computed(() => userStore.hasPermission('website:site:exec'))

const env = ref<WebsiteEnv | null>(null)
const envError = ref('')
const booting = ref(true)

const sites = ref<SiteSummary[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const loading = ref(false)
const listError = ref('')

/** 创建向导：静态站与反代站共用步骤，第 2 步按类型切换字段。 */
const createVisible = ref(false)
const createStep = ref(1)
const submitting = ref(false)
const form = reactive({
  name: '',
  type: 'static' as SiteType,
  domains: [{ domain: '', port: 80 }] as SiteDomain[],
  root: '',
  remark: '',
  createDefaultPage: true,
  accessLogEnabled: true,
  errorLogEnabled: true,
  proxyTarget: '',
  proxyHost: '',
  proxyWebSocket: true,
})

/** 删除确认：目录处理方式必须由用户显式选择，默认走回收站而不是直接删。 */
const deleteVisible = ref(false)
const deleting = ref(false)
const deleteTarget = ref<SiteSummary | null>(null)
const deleteWithRoot = ref(true)

const defaultRoot = computed(() =>
  form.name && env.value ? `${env.value.wwwRoot}/${form.name}` : '',
)

const canSubmit = computed(() => {
  if (!form.name.trim()) return false
  if (form.domains.length === 0 || form.domains.some((d) => !d.domain.trim())) return false
  if (form.type === 'proxy' && !form.proxyTarget.trim()) return false
  return true
})

function messageOf(e: unknown): string {
  if (e instanceof BizError) return errorText(e.code, e.message)
  return e instanceof Error ? e.message : t('common.requestFailed')
}

function reportError(e: unknown) {
  Message.error(messageOf(e))
}

async function loadEnv() {
  try {
    env.value = await fetchWebsiteEnv()
    envError.value = ''
  } catch (e) {
    envError.value = messageOf(e)
  }
}

async function loadSites() {
  loading.value = true
  try {
    const res = await fetchSites({
      page: page.value,
      pageSize: pageSize.value,
      keyword: keyword.value.trim() || undefined,
    })
    sites.value = res.items ?? []
    total.value = res.total
    listError.value = ''
  } catch (e) {
    listError.value = messageOf(e)
    sites.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function boot() {
  booting.value = true
  await Promise.all([loadEnv(), loadSites()])
  booting.value = false
}

function onSearch() {
  page.value = 1
  void loadSites()
}

function onPageChange(p: number) {
  page.value = p
  void loadSites()
}

function openCreate() {
  createStep.value = 1
  Object.assign(form, {
    name: '',
    type: 'static' as SiteType,
    domains: [{ domain: '', port: 80 }],
    root: '',
    remark: '',
    createDefaultPage: true,
    accessLogEnabled: true,
    errorLogEnabled: true,
    proxyTarget: '',
    proxyHost: '',
    proxyWebSocket: true,
  })
  createVisible.value = true
}

function addDomain() {
  form.domains.push({ domain: '', port: 80 })
}

function removeDomain(index: number) {
  if (form.domains.length <= 1) return
  form.domains.splice(index, 1)
}

function domainSummary() {
  return form.domains
    .filter((d) => d.domain.trim())
    .map((d) => `${d.domain.trim()}:${d.port}`)
    .join('、')
}

async function submitCreate() {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    const payload: CreateSitePayload = {
      name: form.name.trim(),
      type: form.type,
      domains: form.domains
        .filter((d) => d.domain.trim())
        .map((d) => ({ domain: d.domain.trim(), port: Number(d.port) || 80 })),
      root: form.root.trim() || undefined,
      remark: form.remark.trim() || undefined,
      accessLogEnabled: form.accessLogEnabled,
      errorLogEnabled: form.errorLogEnabled,
      expireAt: 0,
    }
    if (form.type === 'static') {
      payload.createDefaultPage = form.createDefaultPage
    } else {
      payload.proxyTarget = form.proxyTarget.trim()
      payload.proxyHost = form.proxyHost.trim() || undefined
      payload.proxyWebSocket = form.proxyWebSocket
    }
    const detail = await createSite(payload)
    Message.success(t('website.created', { name: detail.name }))
    createVisible.value = false
    page.value = 1
    await Promise.all([loadSites(), loadEnv()])
  } catch (e) {
    reportError(e)
  } finally {
    submitting.value = false
  }
}

/** Arco 的 Modal.confirm 是回调式的，这里包一层 Promise 便于顺序执行。 */
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

async function toggleStatus(row: SiteSummary) {
  const target = row.status === 'running' ? 'stopped' : 'running'
  if (target === 'stopped') {
    const ok = await confirmModal(
      t('website.confirmStopTitle'),
      t('website.confirmStopTip', { name: row.name }),
    )
    if (!ok) return
  }
  try {
    await changeSiteStatus(row.id, target)
    Message.success(t('website.statusChanged'))
    await loadSites()
  } catch (e) {
    reportError(e)
  }
}

async function rebuild(row: SiteSummary) {
  try {
    await rebuildSite(row.id)
    Message.success(t('website.rebuilt'))
    await loadSites()
  } catch (e) {
    reportError(e)
  }
}

function askDelete(row: SiteSummary) {
  deleteTarget.value = row
  deleteWithRoot.value = true
  deleteVisible.value = true
}

async function confirmDelete() {
  const row = deleteTarget.value
  if (!row) return
  deleting.value = true
  try {
    const res = await deleteSite(row.id, deleteWithRoot.value)
    Message.success(res.rootRecycled ? t('website.deletedRecycled') : t('website.deleted'))
    deleteVisible.value = false
    await Promise.all([loadSites(), loadEnv()])
  } catch (e) {
    reportError(e)
  } finally {
    deleting.value = false
  }
}

async function reloadServer() {
  const ok = await confirmModal(t('website.confirmReloadTitle'), t('website.confirmReloadTip'))
  if (!ok) return
  try {
    await reloadWebServer()
    Message.success(t('website.reloaded'))
  } catch (e) {
    reportError(e)
  }
}

async function runTest() {
  try {
    const res = await testWebServer()
    Modal.info({
      title: t('website.testTitle'),
      content: res.output || t('website.testOK'),
      okText: t('common.confirm'),
    })
  } catch (e) {
    reportError(e)
  }
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

function expireText(value: number) {
  return value ? dayjs(value).format('YYYY-MM-DD') : t('website.neverExpire')
}

function openDetail(row: SiteSummary) {
  void router.push({ name: 'WebsiteDetail', params: { id: row.id } })
}

onMounted(boot)
</script>

<template>
  <div class="site">
    <a-card :bordered="false" :title="$t('menu.website')">
      <template #extra>
        <a-space wrap>
          <a-input-search
            v-model="keyword"
            allow-clear
            :placeholder="$t('website.searchTip')"
            class="site__search"
            @search="onSearch"
            @press-enter="onSearch"
          />
          <a-button size="small" :loading="loading" @click="loadSites">
            <template #icon><icon-refresh /></template>
            {{ $t('sys.refresh') }}
          </a-button>
          <a-button v-if="canExec" size="small" @click="runTest">
            {{ $t('website.testConfig') }}
          </a-button>
          <a-button v-if="canExec" size="small" status="warning" @click="reloadServer">
            {{ $t('website.reload') }}
          </a-button>
          <a-button
            v-if="canCreate"
            type="primary"
            size="small"
            :disabled="!env?.installed"
            @click="openCreate"
          >
            <template #icon><icon-plus /></template>
            {{ $t('website.create') }}
          </a-button>
        </a-space>
      </template>

      <a-alert v-if="envError" type="error" class="site__alert">{{ envError }}</a-alert>
      <a-alert v-else-if="env && !env.installed" type="warning" class="site__alert">
        {{ env.message || $t('website.notInstalled') }}
      </a-alert>
      <a-alert v-else-if="env && env.vhostIncluded === false" type="warning" class="site__alert">
        {{ $t('website.vhostNotIncluded', { dir: env.vhostDir }) }}
      </a-alert>

      <div v-if="env?.installed" class="site__env">
        <span>{{ $t('website.engine') }}: {{ env.kind }} {{ env.version }}</span>
        <span>{{ $t('website.vhostDir') }}: {{ env.vhostDir }}</span>
        <span>{{ $t('website.wwwRoot') }}: {{ env.wwwRoot }}</span>
        <span>{{ $t('website.siteCount') }}: {{ env.siteCount }}</span>
      </div>

      <a-alert v-if="listError" type="error" class="site__alert">{{ listError }}</a-alert>

      <a-skeleton v-if="booting" animation>
        <a-skeleton-line :rows="5" />
      </a-skeleton>

      <a-table
        v-else
        :data="sites"
        :loading="loading"
        :pagination="false"
        :scroll="{ x: '100%' }"
        row-key="id"
        size="small"
      >
        <template #empty>
          <a-empty :description="$t('website.empty')" />
        </template>
        <template #columns>
          <a-table-column :title="$t('website.colSite')" :width="220">
            <template #cell="{ record }">
              <div class="site__name">
                <a-link v-if="canRead" @click="openDetail(record)">{{ record.name }}</a-link>
                <span v-else>{{ record.name }}</span>
                <span class="site__domain">{{ record.primaryDomain }}</span>
              </div>
            </template>
          </a-table-column>
          <a-table-column :title="$t('website.colType')" :width="100">
            <template #cell="{ record }">
              {{ record.type === 'proxy' ? $t('website.typeProxy') : $t('website.typeStatic') }}
            </template>
          </a-table-column>
          <a-table-column :title="$t('website.colStatus')" :width="150">
            <template #cell="{ record }">
              <a-space size="mini">
                <a-tag :color="statusColor(record.status)">{{ statusText(record.status) }}</a-tag>
                <a-tooltip v-if="record.deployError" :content="record.deployError">
                  <icon-exclamation-circle-fill class="site__warn" />
                </a-tooltip>
              </a-space>
            </template>
          </a-table-column>
          <a-table-column
            :title="$t('website.colRoot')"
            data-index="rootPath"
            ellipsis
            tooltip
            :width="240"
          />
          <a-table-column :title="$t('website.colExpire')" :width="120">
            <template #cell="{ record }">{{ expireText(record.expireAt) }}</template>
          </a-table-column>
          <a-table-column :title="$t('website.colVersion')" :width="90">
            <template #cell="{ record }">v{{ record.configVersion }}</template>
          </a-table-column>
          <a-table-column :title="$t('website.colActions')" :width="250" fixed="right">
            <template #cell="{ record }">
              <a-space size="mini" wrap>
                <a-link v-if="canRead" @click="openDetail(record)">
                  {{ $t('website.actionDetail') }}
                </a-link>
                <a-link v-if="canExec" @click="toggleStatus(record)">
                  {{
                    record.status === 'running'
                      ? $t('website.actionStop')
                      : $t('website.actionStart')
                  }}
                </a-link>
                <a-link v-if="canExec" @click="rebuild(record)">
                  {{ $t('website.actionRebuild') }}
                </a-link>
                <a-link v-if="canDelete" status="danger" @click="askDelete(record)">
                  {{ $t('website.actionDelete') }}
                </a-link>
              </a-space>
            </template>
          </a-table-column>
        </template>
      </a-table>

      <div v-if="total > pageSize" class="site__pager">
        <a-pagination
          :current="page"
          :page-size="pageSize"
          :total="total"
          size="small"
          @change="onPageChange"
        />
      </div>
    </a-card>

    <a-modal
      v-model:visible="createVisible"
      :title="$t('website.create')"
      width="640px"
      :mask-closable="false"
      unmount-on-close
    >
      <a-steps :current="createStep" size="small" class="site__steps">
        <a-step>{{ $t('website.stepBasic') }}</a-step>
        <a-step>{{ $t('website.stepType') }}</a-step>
        <a-step>{{ $t('website.stepConfirm') }}</a-step>
      </a-steps>

      <a-form :model="form" layout="vertical" size="small">
        <template v-if="createStep === 1">
          <a-form-item :label="$t('website.fieldName')" required>
            <a-input v-model="form.name" :placeholder="$t('website.fieldNameTip')" />
          </a-form-item>
          <a-form-item :label="$t('website.fieldType')">
            <a-radio-group v-model="form.type" type="button">
              <a-radio value="static">{{ $t('website.typeStatic') }}</a-radio>
              <a-radio value="proxy">{{ $t('website.typeProxy') }}</a-radio>
            </a-radio-group>
          </a-form-item>
          <a-form-item :label="$t('website.fieldDomains')" required>
            <div class="site__domains">
              <div v-for="(d, i) in form.domains" :key="i" class="site__domain-row">
                <a-input v-model="d.domain" :placeholder="$t('website.fieldDomainTip')" />
                <a-input-number
                  v-model="d.port"
                  :min="1"
                  :max="65535"
                  :style="{ width: '110px' }"
                />
                <a-button size="mini" :disabled="form.domains.length <= 1" @click="removeDomain(i)">
                  <template #icon><icon-minus /></template>
                </a-button>
              </div>
              <a-button size="mini" @click="addDomain">
                <template #icon><icon-plus /></template>
                {{ $t('website.addDomain') }}
              </a-button>
            </div>
          </a-form-item>
          <a-form-item :label="$t('website.fieldRoot')">
            <a-input
              v-model="form.root"
              :placeholder="defaultRoot || $t('website.fieldRootTip')"
            />
          </a-form-item>
        </template>

        <template v-else-if="createStep === 2">
          <a-form-item v-if="form.type === 'static'">
            <a-checkbox v-model="form.createDefaultPage">
              {{ $t('website.fieldDefaultPage') }}
            </a-checkbox>
          </a-form-item>
          <template v-else>
            <a-form-item :label="$t('website.fieldProxyTarget')" required>
              <a-input v-model="form.proxyTarget" placeholder="http://127.0.0.1:3000" />
            </a-form-item>
            <a-form-item :label="$t('website.fieldProxyHost')">
              <a-input v-model="form.proxyHost" :placeholder="$t('website.fieldProxyHostTip')" />
            </a-form-item>
            <a-form-item>
              <a-checkbox v-model="form.proxyWebSocket">
                {{ $t('website.fieldWebSocket') }}
              </a-checkbox>
            </a-form-item>
          </template>
          <a-form-item :label="$t('website.fieldLogs')">
            <a-space>
              <a-checkbox v-model="form.accessLogEnabled">
                {{ $t('website.accessLog') }}
              </a-checkbox>
              <a-checkbox v-model="form.errorLogEnabled">
                {{ $t('website.errorLog') }}
              </a-checkbox>
            </a-space>
          </a-form-item>
          <a-form-item :label="$t('website.fieldRemark')">
            <a-textarea
              v-model="form.remark"
              :max-length="255"
              show-word-limit
              :auto-size="{ minRows: 2 }"
            />
          </a-form-item>
        </template>

        <template v-else>
          <a-descriptions :column="1" size="small" bordered>
            <a-descriptions-item :label="$t('website.fieldName')">
              {{ form.name }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('website.fieldType')">
              {{ form.type === 'proxy' ? $t('website.typeProxy') : $t('website.typeStatic') }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('website.fieldDomains')">
              {{ domainSummary() }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('website.fieldRoot')">
              {{ form.root || defaultRoot }}
            </a-descriptions-item>
            <a-descriptions-item v-if="form.type === 'proxy'" :label="$t('website.fieldProxyTarget')">
              {{ form.proxyTarget }}
            </a-descriptions-item>
            <a-descriptions-item
              v-if="form.type === 'proxy'"
              :label="$t('website.fieldWebSocket')"
            >
              {{ form.proxyWebSocket ? $t('common.yes') : $t('common.no') }}
            </a-descriptions-item>
            <a-descriptions-item
              v-if="form.type === 'proxy' && form.proxyHost.trim()"
              :label="$t('website.fieldProxyHost')"
            >
              {{ form.proxyHost.trim() }}
            </a-descriptions-item>
          </a-descriptions>
          <a-alert type="info" class="site__hint">{{ $t('website.createHint') }}</a-alert>
        </template>
      </a-form>

      <template #footer>
        <div class="site__footer">
          <a-button v-if="createStep > 1" size="small" @click="createStep -= 1">
            {{ $t('website.prevStep') }}
          </a-button>
          <span v-else></span>
          <a-space>
            <a-button size="small" @click="createVisible = false">
              {{ $t('common.cancel') }}
            </a-button>
            <a-button
              v-if="createStep < 3"
              type="primary"
              size="small"
              :disabled="createStep === 1 && !form.name.trim()"
              @click="createStep += 1"
            >
              {{ $t('website.nextStep') }}
            </a-button>
            <a-button
              v-else
              type="primary"
              size="small"
              :loading="submitting"
              :disabled="!canSubmit"
              @click="submitCreate"
            >
              {{ $t('website.submitCreate') }}
            </a-button>
          </a-space>
        </div>
      </template>
    </a-modal>

    <a-modal
      v-model:visible="deleteVisible"
      :title="$t('website.confirmDeleteTitle')"
      :ok-text="$t('website.actionDelete')"
      unmount-on-close
      :cancel-text="$t('common.cancel')"
      :ok-loading="deleting"
      :ok-button-props="{ status: 'danger' }"
      @ok="confirmDelete"
    >
      <p>{{ $t('website.confirmDeleteTip', { name: deleteTarget?.name ?? '' }) }}</p>
      <p class="site__path">{{ deleteTarget?.rootPath }}</p>
      <a-checkbox v-model="deleteWithRoot">{{ $t('website.deleteRoot') }}</a-checkbox>
    </a-modal>
  </div>
</template>

<style lang="scss" scoped>
.site {
  &__search {
    width: 200px;
  }

  &__alert {
    margin-bottom: 12px;
  }

  &__hint {
    margin-top: 12px;
  }

  &__env {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 20px;
    margin-bottom: 12px;
    color: var(--color-text-3);
    font-size: 12px;
  }

  &__name {
    display: flex;
    flex-direction: column;
    line-height: 1.4;
  }

  &__domain {
    color: var(--color-text-3);
    font-size: 12px;
  }

  &__warn {
    color: rgb(var(--danger-6));
  }

  &__pager {
    display: flex;
    justify-content: flex-end;
    margin-top: 12px;
  }

  &__steps {
    margin-bottom: 16px;
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

  &__footer {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  &__path {
    color: var(--color-text-3);
    font-size: 12px;
    word-break: break-all;
  }
}

@media (max-width: 768px) {
  .site {
    &__search {
      width: 100%;
    }
  }
}
</style>
