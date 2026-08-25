<script setup lang="ts">
/**
 * 站点高级设置卡片。表单状态全部来自后端 GET /settings，保存后以响应为准回填，
 * 因此这里不做任何本地兜底渲染：后端拒绝的入参会原样报错，不会出现「界面已改、线上未生效」。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message, Modal } from '@arco-design/web-vue'

import {
  fetchSiteSettings,
  saveSiteSettings,
  type CacheProfile,
  type RedirectRule,
  type SaveSiteSettingsPayload,
  type SiteSettings,
  type SiteType,
} from '@/api/website'
import { BizError } from '@/types/api'
import { useUserStore } from '@/store/modules/user'
import { errorText } from '@/utils/errorText'

const props = defineProps<{
  siteId: string
  siteType: SiteType
  /** 停用站点只落库不下发，界面需要显式说明，避免用户误以为已生效 */
  stopped: boolean
}>()

const emit = defineEmits<{ (e: 'deployed'): void }>()

const { t } = useI18n()
const userStore = useUserStore()

const canRead = computed(() => userStore.hasPermission('website:setting:read'))
const canWrite = computed(() => userStore.hasPermission('website:setting:update'))

const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const settings = ref<SiteSettings | null>(null)
const form = ref<SaveSiteSettingsPayload | null>(null)

const cacheOptions = computed(() =>
  (settings.value?.cacheProfiles ?? []).map((code) => ({
    value: code,
    label: profileLabel(code),
  })),
)

function profileLabel(code: CacheProfile) {
  const key = `website.cacheProfileOption.${code}`
  return t(key) === key ? code : t(key)
}

function messageOf(e: unknown): string {
  if (e instanceof BizError) return errorText(e.code, e.message)
  return e instanceof Error ? e.message : t('common.requestFailed')
}

/** 后端返回的是完整状态，深拷成草稿，避免编辑时污染已保存的基线。 */
function toForm(s: SiteSettings): SaveSiteSettingsPayload {
  return {
    indexFiles: [...s.indexFiles],
    runPath: s.runPath,
    autoIndex: s.autoIndex,
    cacheProfile: s.cacheProfile,
    antiLeech: {
      ...s.antiLeech,
      extensions: [...s.antiLeech.extensions],
      allowedReferers: [...s.antiLeech.allowedReferers],
    },
    accessControl: {
      ...s.accessControl,
      allowIps: [...s.accessControl.allowIps],
      denyIps: [...s.accessControl.denyIps],
      denyUserAgents: [...s.accessControl.denyUserAgents],
    },
    redirects: s.redirects.map((r) => ({ ...r })),
    rateLimit: { ...s.rateLimit },
    accessLogEnabled: s.accessLogEnabled,
    errorLogEnabled: s.errorLogEnabled,
  }
}

async function load() {
  if (!canRead.value) return
  loading.value = true
  try {
    const s = await fetchSiteSettings(props.siteId)
    settings.value = s
    form.value = toForm(s)
    loadError.value = ''
  } catch (e) {
    loadError.value = messageOf(e)
    settings.value = null
    form.value = null
  } finally {
    loading.value = false
  }
}

function addRedirect() {
  form.value?.redirects.push({
    enabled: true,
    path: '',
    target: '',
    code: 301,
    keepPath: true,
    keepQuery: false,
    remark: '',
  })
}

function removeRedirect(index: number) {
  form.value?.redirects.splice(index, 1)
}

function trimRedirects(rules: RedirectRule[]) {
  return rules
    .filter((r) => r.path.trim() && r.target.trim())
    .map((r) => ({ ...r, path: r.path.trim(), target: r.target.trim(), remark: r.remark.trim() }))
}

async function save() {
  const draft = form.value
  if (!draft) return
  saving.value = true
  try {
    const s = await saveSiteSettings(props.siteId, {
      ...draft,
      runPath: draft.runPath.trim(),
      redirects: trimRedirects(draft.redirects),
    })
    settings.value = s
    form.value = toForm(s)
    Message.success(t('website.settingsSaved'))
    emit('deployed')
  } catch (e) {
    Message.error(messageOf(e))
  } finally {
    saving.value = false
  }
}

function confirmSave() {
  if (props.stopped) {
    void save()
    return
  }
  Modal.confirm({
    title: t('website.settingsTitle'),
    content: t('website.confirmSaveConfigTip'),
    okText: t('common.confirm'),
    cancelText: t('common.cancel'),
    onOk: () => void save(),
  })
}

watch(() => props.siteId, load, { immediate: true })
defineExpose({ reload: load })
</script>

<template>
  <a-card :bordered="false" :title="$t('website.settingsTitle')">
    <template #extra>
      <a-space>
        <a-button size="small" :loading="loading" @click="load">
          <template #icon><icon-refresh /></template>
        </a-button>
        <a-button
          v-if="canWrite"
          type="primary"
          size="small"
          :loading="saving"
          :disabled="!form"
          @click="confirmSave"
        >
          {{ $t('website.saveAndDeploy') }}
        </a-button>
      </a-space>
    </template>

    <a-alert v-if="!canRead" type="info">{{ $t('error.forbidden') }}</a-alert>
    <a-alert v-else-if="loadError" type="error">{{ loadError }}</a-alert>
    <a-skeleton v-else-if="loading && !form" animation>
      <a-skeleton-line :rows="6" />
    </a-skeleton>

    <template v-else-if="form && settings">
      <a-alert v-if="stopped" type="warning" class="site-settings__alert">
        {{ $t('website.settingsStoppedTip') }}
      </a-alert>

      <a-form :model="form" layout="vertical" size="small" :disabled="!canWrite">
        <a-collapse :default-active-key="['basic', 'security']" :bordered="false">
          <a-collapse-item key="basic" :header="$t('website.settingsBasic')">
            <div class="site-settings__grid">
              <a-form-item :label="$t('website.indexFiles')" :extra="$t('website.indexFilesTip')">
                <a-input-tag v-model="form.indexFiles" allow-clear />
              </a-form-item>
              <a-form-item :label="$t('website.runPath')" :extra="$t('website.runPathTip')">
                <a-input v-model="form.runPath" placeholder="/public" allow-clear />
              </a-form-item>
              <a-form-item :label="$t('website.autoIndex')" :extra="$t('website.autoIndexTip')">
                <a-switch v-model="form.autoIndex" />
              </a-form-item>
              <a-form-item :label="$t('website.fieldLogs')">
                <a-space direction="vertical" fill>
                  <a-checkbox v-model="form.accessLogEnabled">
                    {{ $t('website.accessLog') }}
                  </a-checkbox>
                  <a-checkbox v-model="form.errorLogEnabled">
                    {{ $t('website.errorLog') }}
                  </a-checkbox>
                </a-space>
              </a-form-item>
            </div>
            <p class="site-settings__path">{{ settings.rootPath }}</p>
          </a-collapse-item>

          <a-collapse-item key="security" :header="$t('website.settingsSecurity')">
            <a-form-item>
              <a-checkbox v-model="form.accessControl.enabled">
                {{ $t('website.accessControl') }}
              </a-checkbox>
            </a-form-item>
            <div v-if="form.accessControl.enabled" class="site-settings__grid">
              <a-form-item :label="$t('website.allowIps')" :extra="$t('website.allowIpsTip')">
                <a-input-tag v-model="form.accessControl.allowIps" allow-clear />
              </a-form-item>
              <a-form-item :label="$t('website.denyIps')" :extra="$t('website.denyIpsTip')">
                <a-input-tag v-model="form.accessControl.denyIps" allow-clear />
              </a-form-item>
              <a-form-item
                :label="$t('website.denyUserAgents')"
                :extra="$t('website.denyUserAgentsTip')"
              >
                <a-input-tag v-model="form.accessControl.denyUserAgents" allow-clear />
              </a-form-item>
            </div>

            <a-form-item>
              <a-checkbox v-model="form.antiLeech.enabled">
                {{ $t('website.antiLeech') }}
              </a-checkbox>
            </a-form-item>
            <div v-if="form.antiLeech.enabled" class="site-settings__grid">
              <a-form-item :label="$t('website.antiLeechExt')" :extra="$t('website.antiLeechExtTip')">
                <a-input-tag v-model="form.antiLeech.extensions" allow-clear />
              </a-form-item>
              <a-form-item
                :label="$t('website.antiLeechReferer')"
                :extra="$t('website.antiLeechRefererTip')"
              >
                <a-input-tag v-model="form.antiLeech.allowedReferers" allow-clear />
              </a-form-item>
              <a-form-item
                :label="$t('website.antiLeechEmpty')"
                :extra="$t('website.antiLeechEmptyTip')"
              >
                <a-switch v-model="form.antiLeech.allowEmptyReferer" />
              </a-form-item>
              <a-form-item :label="$t('website.antiLeechReturn')">
                <a-select v-model="form.antiLeech.returnCode">
                  <a-option :value="403">403</a-option>
                  <a-option :value="404">404</a-option>
                </a-select>
              </a-form-item>
            </div>
          </a-collapse-item>

          <a-collapse-item key="redirect" :header="$t('website.settingsRedirect')">
            <a-alert v-if="siteType === 'proxy'" type="info" class="site-settings__alert">
              {{ $t('website.redirectProxyTip') }}
            </a-alert>
            <p v-if="!form.redirects.length" class="site-settings__empty">
              {{ $t('website.redirectEmpty') }}
            </p>
            <div
              v-for="(rule, i) in form.redirects"
              :key="i"
              class="site-settings__rule"
            >
              <div class="site-settings__rule-head">
                <a-switch v-model="rule.enabled" size="small" />
                <a-input
                  v-model="rule.path"
                  :placeholder="$t('website.redirectPath')"
                  :style="{ width: '160px' }"
                />
                <a-input v-model="rule.target" :placeholder="$t('website.redirectTarget')" />
                <a-select v-model="rule.code" :style="{ width: '96px' }">
                  <a-option :value="301">301</a-option>
                  <a-option :value="302">302</a-option>
                  <a-option :value="307">307</a-option>
                  <a-option :value="308">308</a-option>
                </a-select>
                <a-button size="mini" @click="removeRedirect(i)">
                  <template #icon><icon-minus /></template>
                </a-button>
              </div>
              <div class="site-settings__rule-opts">
                <a-checkbox v-model="rule.keepPath">{{ $t('website.redirectKeepPath') }}</a-checkbox>
                <a-checkbox v-model="rule.keepQuery">
                  {{ $t('website.redirectKeepQuery') }}
                </a-checkbox>
                <a-input
                  v-model="rule.remark"
                  :placeholder="$t('website.fieldRemark')"
                  :max-length="120"
                />
              </div>
            </div>
            <a-button v-if="canWrite" size="mini" @click="addRedirect">
              <template #icon><icon-plus /></template>
              {{ $t('website.addRedirect') }}
            </a-button>
          </a-collapse-item>

          <a-collapse-item key="perf" :header="$t('website.settingsPerf')">
            <div class="site-settings__grid">
              <a-form-item :label="$t('website.cacheProfile')">
                <a-select v-model="form.cacheProfile" :options="cacheOptions" />
              </a-form-item>
              <a-form-item :label="$t('website.rateLimit')">
                <a-switch v-model="form.rateLimit.enabled" />
              </a-form-item>
              <template v-if="form.rateLimit.enabled">
                <a-form-item :label="$t('website.rateLimitBandwidth')">
                  <a-input-number v-model="form.rateLimit.bandwidthKb" :min="1" :max="1048576" />
                </a-form-item>
                <a-form-item
                  :label="$t('website.rateLimitBurst')"
                  :extra="$t('website.rateLimitBurstTip')"
                >
                  <a-input-number v-model="form.rateLimit.burstKb" :min="0" :max="1048576" />
                </a-form-item>
              </template>
            </div>
          </a-collapse-item>
        </a-collapse>
      </a-form>
    </template>
  </a-card>
</template>

<style lang="scss" scoped>
.site-settings {
  &__alert {
    margin-bottom: 12px;
  }

  &__grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 0 16px;
  }

  &__path,
  &__empty {
    margin: 0;
    color: var(--color-text-3);
    font-size: 12px;
    word-break: break-all;
  }

  &__rule {
    padding: 8px 0;
    border-bottom: 1px solid var(--color-border-2);
  }

  &__rule-head,
  &__rule-opts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  &__rule-opts {
    margin-top: 8px;
  }
}
</style>
