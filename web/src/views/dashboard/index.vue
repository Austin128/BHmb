<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted } from 'vue'

import MetricCard from '@/components/MetricCard.vue'
import { useOverview } from '@/composables/useOverview'
import { useUserStore } from '@/store/modules/user'
import { humanDuration, humanSize } from '@/utils/format'

const user = useUserStore()
const { data, loading, initializing, error, collectedAt, autoRefresh, allowed, load } = useOverview()

const lastLogin = computed(() => {
  const at = user.user?.lastLoginAt
  return at ? dayjs(at).format('YYYY-MM-DD HH:mm:ss') : ''
})

const roleNames = computed(() => user.roles.map((r) => r.name).join('、'))

/** 主机指标只在 Linux 可采集，其余平台把卡片标为不可用而不是显示 0%。 */
const hostOK = computed(() => data.value?.hostMetricsAvailable === true)

const cpuText = computed(() => {
  const cpu = data.value?.cpu
  return cpu ? `${cpu.cores} × ${cpu.model}` : ''
})

const loadText = computed(() => {
  const cpu = data.value?.cpu
  return cpu ? `${cpu.load1.toFixed(2)} / ${cpu.load5.toFixed(2)} / ${cpu.load15.toFixed(2)}` : ''
})

const memText = computed(() => {
  const m = data.value?.memory
  return m ? `${humanSize(m.used)} / ${humanSize(m.total)}` : ''
})

const swapText = computed(() => {
  const m = data.value?.memory
  if (!m || m.swapTotal === 0) return '-'
  return `${humanSize(m.swapUsed)} / ${humanSize(m.swapTotal)}`
})

/** 总览只展示占用最高的那块盘，完整列表在系统运维页。 */
const primaryDisk = computed(() => {
  const disks = data.value?.disks ?? []
  if (disks.length === 0) return null
  return [...disks].sort((a, b) => b.usedPercent - a.usedPercent)[0]
})

const diskText = computed(() =>
  primaryDisk.value
    ? `${humanSize(primaryDisk.value.used)} / ${humanSize(primaryDisk.value.total)}`
    : '-',
)

const panelUptime = computed(() =>
  data.value ? humanDuration(data.value.panel.uptimeSeconds) : '-',
)

const hostUptime = computed(() =>
  data.value && data.value.host.uptimeSeconds > 0
    ? humanDuration(data.value.host.uptimeSeconds)
    : '-',
)

onMounted(load)
</script>

<template>
  <div class="dash">
    <a-card :bordered="false">
      <a-typography-title :heading="5" class="dash__title">
        {{ $t('dashboard.welcome', { name: user.nickname }) }}
      </a-typography-title>
      <a-descriptions :column="{ xs: 1, md: 2, xl: 4 }" bordered size="medium">
        <a-descriptions-item :label="$t('dashboard.account')">
          {{ user.user?.username }}
          <a-tag v-if="user.isSuperAdmin" color="arcoblue" size="small">super</a-tag>
        </a-descriptions-item>
        <a-descriptions-item :label="$t('dashboard.roles')">
          {{ roleNames || '-' }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('dashboard.permissions')">
          {{ user.permissions.includes('*') ? $t('user.allPermissions') : user.permissions.length }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('dashboard.lastLogin')">
          {{ lastLogin || $t('dashboard.never') }}
        </a-descriptions-item>
      </a-descriptions>
    </a-card>

    <a-card v-if="!allowed" :bordered="false">
      <a-alert type="info">{{ $t('error.forbidden') }}</a-alert>
    </a-card>

    <a-card v-else :bordered="false" :title="$t('sys.host')">
      <template #extra>
        <a-space size="medium">
          <span class="dash__time">
            {{ collectedAt ? $t('sys.updatedAt', { time: collectedAt }) : '' }}
          </span>
          <a-switch v-model="autoRefresh" size="small" />
          <span class="dash__auto">{{ $t('sys.autoRefresh') }}</span>
          <a-button size="small" :loading="loading" @click="load">
            <template #icon><icon-refresh /></template>
            {{ $t('sys.refresh') }}
          </a-button>
        </a-space>
      </template>

      <a-alert v-if="error" type="error" class="dash__alert">{{ error }}</a-alert>
      <a-alert v-else-if="data && !hostOK" type="warning" class="dash__alert">
        {{ $t('sys.unsupported') }}
      </a-alert>

      <a-skeleton v-if="initializing" animation>
        <a-skeleton-line :rows="5" />
      </a-skeleton>

      <template v-else-if="data">
        <div class="dash__metrics">
          <MetricCard
            :title="$t('sys.cpuUsage')"
            :percent="data.cpu.usagePercent"
            :primary="cpuText"
            :secondary-label="$t('sys.load')"
            :secondary-value="loadText"
            :available="hostOK"
          />
          <MetricCard
            :title="$t('sys.memoryUsage')"
            :percent="data.memory.usedPercent"
            :primary="memText"
            :secondary-label="$t('sys.swap')"
            :secondary-value="swapText"
            :available="hostOK"
          />
          <MetricCard
            :title="$t('sys.diskUsage')"
            :percent="primaryDisk?.usedPercent ?? 0"
            :primary="diskText"
            :secondary-label="$t('sys.mountPoint')"
            :secondary-value="primaryDisk?.path ?? '-'"
            :available="Boolean(primaryDisk)"
          />
        </div>

        <a-descriptions class="dash__desc" :column="{ xs: 1, md: 2, xl: 3 }" bordered size="medium">
          <a-descriptions-item :label="$t('sys.hostname')">
            {{ data.host.hostname || '-' }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('sys.os')">
            {{ data.host.osName }} · {{ data.host.arch }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('sys.hostUptime')">{{ hostUptime }}</a-descriptions-item>
          <a-descriptions-item :label="$t('sys.version')">
            {{ data.panel.version }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('sys.panelUptime')">
            {{ panelUptime }}
          </a-descriptions-item>
          <a-descriptions-item :label="$t('sys.database')">
            <a-tag :color="data.database.ok ? 'green' : 'red'" size="small">
              {{ data.database.driver }}
            </a-tag>
            <span v-if="!data.database.ok" class="dash__dberr">{{ data.database.error }}</span>
          </a-descriptions-item>
        </a-descriptions>
      </template>
    </a-card>
  </div>
</template>

<style lang="scss" scoped>
.dash {
  display: flex;
  flex-direction: column;
  gap: 16px;

  &__title {
    margin-top: 0;
  }

  &__time,
  &__auto {
    color: var(--color-text-3);
    font-size: 12px;
  }

  &__alert {
    margin-bottom: 16px;
  }

  &__metrics {
    display: grid;
    /* 窄屏单列，宽屏自动铺满，避免卡片被压扁或重叠 */
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 16px;
  }

  &__desc {
    margin-top: 16px;
  }

  &__dberr {
    margin-left: 8px;
    color: rgb(var(--danger-6));
  }
}

@media (max-width: 768px) {
  .dash__time,
  .dash__auto {
    display: none;
  }
}
</style>
