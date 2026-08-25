<script setup lang="ts">
/**
 * 站点日志卡片。只读取文件尾部若干行：访问日志可能有上百 MB，整读会拖垮浏览器。
 * 清空走后端截断（保留 inode），因此不需要 reload Nginx 即可继续写入。
 */
import { computed, ref, watch } from 'vue'
import dayjs from 'dayjs'
import { useI18n } from 'vue-i18n'
import { Message, Modal } from '@arco-design/web-vue'

import { clearSiteLog, fetchSiteLog, type SiteLog, type SiteLogType } from '@/api/website'
import { BizError } from '@/types/api'
import { useUserStore } from '@/store/modules/user'
import { errorText } from '@/utils/errorText'
import { humanSize } from '@/utils/format'

const LINE_CHOICES = [100, 200, 500, 1000]

const props = defineProps<{ siteId: string }>()

const { t } = useI18n()
const userStore = useUserStore()

const canRead = computed(() => userStore.hasPermission('website:log:read'))
const canClear = computed(() => userStore.hasPermission('website:log:exec'))

const kind = ref<SiteLogType>('access')
const lines = ref(200)
const loading = ref(false)
const clearing = ref(false)
const loadError = ref('')
const log = ref<SiteLog | null>(null)

const lineOptions = LINE_CHOICES.map((n) => ({ value: n, label: String(n) }))
const body = computed(() => log.value?.lines.join('\n') ?? '')
const updatedText = computed(() =>
  log.value?.modifiedAt ? dayjs(log.value.modifiedAt).format('YYYY-MM-DD HH:mm:ss') : '-',
)

function messageOf(e: unknown): string {
  if (e instanceof BizError) return errorText(e.code, e.message)
  return e instanceof Error ? e.message : t('common.requestFailed')
}

async function load() {
  if (!canRead.value) return
  loading.value = true
  try {
    log.value = await fetchSiteLog(props.siteId, kind.value, lines.value)
    loadError.value = ''
  } catch (e) {
    loadError.value = messageOf(e)
    log.value = null
  } finally {
    loading.value = false
  }
}

async function clear() {
  clearing.value = true
  try {
    log.value = await clearSiteLog(props.siteId, kind.value)
    Message.success(t('website.logCleared'))
  } catch (e) {
    Message.error(messageOf(e))
    await load()
  } finally {
    clearing.value = false
  }
}

function confirmClear() {
  Modal.confirm({
    title: t('website.confirmClearLog'),
    content: t('website.confirmClearLogTip'),
    okText: t('common.confirm'),
    cancelText: t('common.cancel'),
    onOk: () => void clear(),
  })
}

watch([() => props.siteId, kind, lines], load, { immediate: true })
defineExpose({ reload: load })
</script>

<template>
  <a-card :bordered="false" :title="$t('website.logTitle')">
    <template #extra>
      <a-space wrap>
        <a-radio-group v-model="kind" type="button" size="small">
          <a-radio value="access">{{ $t('website.accessLog') }}</a-radio>
          <a-radio value="error">{{ $t('website.errorLog') }}</a-radio>
        </a-radio-group>
        <a-select v-model="lines" :options="lineOptions" size="small" class="site-log__lines" />
        <a-button size="small" :loading="loading" @click="load">
          <template #icon><icon-refresh /></template>
        </a-button>
        <a-button
          v-if="canClear"
          status="danger"
          size="small"
          :loading="clearing"
          :disabled="!log?.exists"
          @click="confirmClear"
        >
          {{ $t('website.clearLog') }}
        </a-button>
      </a-space>
    </template>

    <a-alert v-if="!canRead" type="info">{{ $t('error.forbidden') }}</a-alert>
    <a-alert v-else-if="loadError" type="error">{{ loadError }}</a-alert>
    <a-skeleton v-else-if="loading && !log" animation>
      <a-skeleton-line :rows="6" />
    </a-skeleton>

    <template v-else-if="log">
      <a-alert v-if="!log.enabled" type="warning" class="site-log__alert">
        {{ $t('website.logDisabledTip') }}
      </a-alert>
      <a-alert v-else-if="log.truncated" type="info" class="site-log__alert">
        {{ $t('website.logTruncatedTip', { lines }) }}
      </a-alert>

      <a-empty v-if="!log.exists || !log.lines.length" :description="$t('website.logEmpty')" />
      <pre v-else class="site-log__body">{{ body }}</pre>

      <p class="site-log__meta">
        {{ log.path }}
        <span v-if="log.exists">
          · {{ humanSize(log.size) }} · {{ $t('website.logUpdatedAt') }} {{ updatedText }}
        </span>
      </p>
    </template>
  </a-card>
</template>

<style lang="scss" scoped>
.site-log {
  &__lines {
    width: 88px;
  }

  &__alert {
    margin-bottom: 12px;
  }

  &__body {
    max-height: 420px;
    margin: 0;
    padding: 12px;
    overflow: auto;
    background: var(--color-fill-2);
    border-radius: 4px;
    color: var(--color-text-1);
    font-family: var(--font-mono, 'SFMono-Regular', Menlo, Consolas, monospace);
    font-size: 12px;
    line-height: 1.6;
    white-space: pre;
  }

  &__meta {
    margin: 8px 0 0;
    color: var(--color-text-3);
    font-size: 12px;
    word-break: break-all;
  }
}
</style>
