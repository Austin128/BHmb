<script setup lang="ts">
/**
 * 伪静态卡片。内置模板正文只读（保存时以后端规则库为准），选择「自定义」后才可编辑。
 * 保存失败时后端会还原线上片段，因此这里出错后重新拉取，界面与磁盘保持一致。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message, Modal } from '@arco-design/web-vue'

import {
  fetchRewriteTemplates,
  fetchSiteRewrite,
  saveSiteRewrite,
  type SiteRewrite,
  type SiteRewriteTemplate,
} from '@/api/website'
import { BizError } from '@/types/api'
import { useUserStore } from '@/store/modules/user'
import { errorText } from '@/utils/errorText'

const CUSTOM = 'custom'
const NONE = 'none'

const props = defineProps<{ siteId: string; stopped: boolean }>()
const emit = defineEmits<{ (e: 'deployed'): void }>()

const { t } = useI18n()
const userStore = useUserStore()

const canRead = computed(() => userStore.hasPermission('website:rewrite:read'))
const canWrite = computed(() => userStore.hasPermission('website:rewrite:update'))

const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const current = ref<SiteRewrite | null>(null)
const templates = ref<SiteRewriteTemplate[]>([])

const templateCode = ref(NONE)
const content = ref('')
const enabled = ref(false)

const isCustom = computed(() => templateCode.value === CUSTOM)
const isNone = computed(() => templateCode.value === NONE)
/** 内置模板正文由后端规则库提供，只读展示，避免与线上片段不一致。 */
const readonlyBody = computed(() => !isCustom.value)

const options = computed(() => {
  const items = templates.value.map((tpl) => ({
    value: tpl.code,
    label: tpl.code === NONE ? t('website.rewriteNone') : tpl.name,
  }))
  items.push({ value: CUSTOM, label: t('website.rewriteCustom') })
  return items
})

function messageOf(e: unknown): string {
  if (e instanceof BizError) return errorText(e.code, e.message)
  return e instanceof Error ? e.message : t('common.requestFailed')
}

function applyCurrent(r: SiteRewrite) {
  current.value = r
  templateCode.value = r.templateCode || NONE
  content.value = r.content
  enabled.value = r.enabled
}

async function load() {
  if (!canRead.value) return
  loading.value = true
  try {
    const [r, lib] = await Promise.all([fetchSiteRewrite(props.siteId), fetchRewriteTemplates()])
    templates.value = lib.items
    applyCurrent(r)
    loadError.value = ''
  } catch (e) {
    loadError.value = messageOf(e)
    current.value = null
  } finally {
    loading.value = false
  }
}

/** 切换来源时用规则库正文预览；custom 保留用户已有正文，避免误清空。 */
function onTemplateChange(code: string) {
  if (code === CUSTOM) {
    if (!content.value) content.value = current.value?.content ?? ''
    enabled.value = true
    return
  }
  if (code === NONE) {
    content.value = ''
    enabled.value = false
    return
  }
  content.value = templates.value.find((tpl) => tpl.code === code)?.content ?? ''
  enabled.value = true
}

async function save() {
  saving.value = true
  try {
    const r = await saveSiteRewrite(props.siteId, {
      templateCode: templateCode.value,
      content: isCustom.value ? content.value : '',
      enabled: isNone.value ? false : enabled.value,
    })
    applyCurrent(r)
    Message.success(t('website.rewriteSaved'))
    emit('deployed')
  } catch (e) {
    Message.error(messageOf(e))
    // 后端已还原线上片段，重新拉取以免界面停留在被拒绝的草稿上。
    await load()
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
    title: t('website.confirmSaveRewrite'),
    content: t('website.confirmSaveRewriteTip'),
    okText: t('common.confirm'),
    cancelText: t('common.cancel'),
    onOk: () => void save(),
  })
}

watch(() => props.siteId, load, { immediate: true })
defineExpose({ reload: load })
</script>

<template>
  <a-card :bordered="false" :title="$t('website.rewriteTitle')">
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
          :disabled="!current"
          @click="confirmSave"
        >
          {{ $t('website.saveAndDeploy') }}
        </a-button>
      </a-space>
    </template>

    <a-alert v-if="!canRead" type="info">{{ $t('error.forbidden') }}</a-alert>
    <a-alert v-else-if="loadError" type="error">{{ loadError }}</a-alert>
    <a-skeleton v-else-if="loading && !current" animation>
      <a-skeleton-line :rows="5" />
    </a-skeleton>

    <template v-else-if="current">
      <a-alert v-if="stopped" type="warning" class="site-rewrite__alert">
        {{ $t('website.settingsStoppedTip') }}
      </a-alert>

      <a-form
        :model="{ templateCode, content, enabled }"
        layout="vertical"
        size="small"
        :disabled="!canWrite"
      >
        <div class="site-rewrite__head">
          <a-form-item :label="$t('website.rewriteTemplate')" class="site-rewrite__select">
            <a-select
              v-model="templateCode"
              :options="options"
              @change="onTemplateChange(String($event))"
            />
          </a-form-item>
          <a-form-item v-if="!isNone" :label="$t('website.rewriteEnabled')">
            <a-switch v-model="enabled" />
          </a-form-item>
        </div>

        <a-form-item
          v-if="!isNone"
          :label="$t('website.rewriteContent')"
          :extra="readonlyBody ? $t('website.rewriteBuiltinReadonly') : $t('website.rewriteHint')"
        >
          <a-textarea
            v-model="content"
            class="site-rewrite__editor"
            :auto-size="{ minRows: 10, maxRows: 24 }"
            :readonly="readonlyBody"
            spellcheck="false"
          />
        </a-form-item>
      </a-form>

      <p v-if="current.file" class="site-rewrite__file">
        {{ $t('website.rewriteFile') }}: {{ current.file }}
      </p>
    </template>
  </a-card>
</template>

<style lang="scss" scoped>
.site-rewrite {
  &__alert {
    margin-bottom: 12px;
  }

  &__head {
    display: flex;
    flex-wrap: wrap;
    gap: 0 16px;
  }

  &__select {
    max-width: 260px;
  }

  &__editor {
    font-family: var(--font-mono, 'SFMono-Regular', Menlo, Consolas, monospace);
    font-size: 12px;
  }

  &__file {
    margin: 4px 0 0;
    color: var(--color-text-3);
    font-size: 12px;
    word-break: break-all;
  }
}
</style>
