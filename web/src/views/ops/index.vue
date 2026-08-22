<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted } from 'vue'

import MetricCard from '@/components/MetricCard.vue'
import { useOverview } from '@/composables/useOverview'
import { humanDuration, humanSize, percentText, usageColor } from '@/utils/format'

const { data, loading, initializing, error, collectedAt, autoRefresh, allowed, load } = useOverview()

const hostOK = computed(() => data.value?.hostMetricsAvailable === true)

const loadText = computed(() => {
  const cpu = data.value?.cpu
  return cpu ? `${cpu.load1.toFixed(2)} / ${cpu.load5.toFixed(2)} / ${cpu.load15.toFixed(2)}` : '-'
})

const memText = computed(() => {
  const m = data.value?.memory
  return m ? `${humanSize(m.used)} / ${humanSize(m.total)}` : '-'
})

const swapText = computed(() => {
  const m = data.value?.memory
  if (!m || m.swapTotal === 0) return '-'
  return `${humanSize(m.swapUsed)} / ${humanSize(m.swapTotal)}`
})

const disks = computed(() => data.value?.disks ?? [])

/** 占用最高的挂载点：磁盘卡片展示它，最容易先出问题。 */
const busiestDisk = computed(() => {
  if (disks.value.length === 0) return null
  return [...disks.value].sort((a, b) => b.usedPercent - a.usedPercent)[0]
})

const diskText = computed(() =>
  busiestDisk.value
    ? `${humanSize(busiestDisk.value.used)} / ${humanSize(busiestDisk.value.total)}`
    : '-',
)

/** 只展示有流量的网卡，回环与未启用的虚拟网卡会把表格撑得很长。 */
const nics = computed(() =>
  (data.value?.network ?? []).filter((n) => n.bytesRecv > 0 || n.bytesSent > 0),
)

function fmtTime(value?: string) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '-'
}

onMounted(load)
</script>

<template>
  <div class="ops">
    <a-card v-if="!allowed" :bordered="false" :title="$t('menu.ops')">
      <a-alert type="info">{{ $t('error.forbidden') }}</a-alert>
    </a-card>

    <template v-else>
      <a-card :bordered="false" :title="$t('menu.ops')">
        <template #extra>
          <a-space size="medium">
            <span class="ops__time">
              {{ collectedAt ? $t('sys.updatedAt', { time: collectedAt }) : '' }}
            </span>
            <a-switch v-model="autoRefresh" size="small" />
            <span class="ops__auto">{{ $t('sys.autoRefresh') }}</span>
            <a-button size="small" :loading="loading" @click="load">
              <template #icon><icon-refresh /></template>
              {{ $t('sys.refresh') }}
            </a-button>
          </a-space>
        </template>

        <a-alert v-if="error" type="error" class="ops__alert">{{ error }}</a-alert>
        <a-alert v-else-if="data && !hostOK" type="warning" class="ops__alert">
          {{ $t('sys.unsupported') }}
        </a-alert>

        <a-skeleton v-if="initializing" animation>
          <a-skeleton-line :rows="6" />
        </a-skeleton>

        <template v-else-if="data">
          <div class="ops__metrics">
            <MetricCard
              :title="$t('sys.cpuUsage')"
              :percent="data.cpu.usagePercent"
              :primary="`${data.cpu.cores} × ${data.cpu.model}`"
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
              :percent="busiestDisk?.usedPercent ?? 0"
              :primary="diskText"
              :secondary-label="$t('sys.mountPoint')"
              :secondary-value="busiestDisk?.path ?? '-'"
              :available="Boolean(busiestDisk)"
            />
          </div>

          <a-descriptions
            class="ops__desc"
            :title="$t('sys.host')"
            :column="{ xs: 1, md: 2, xl: 3 }"
            bordered
            size="medium"
          >
            <a-descriptions-item :label="$t('sys.hostname')">
              {{ data.host.hostname || '-' }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.os')">{{ data.host.osName }}</a-descriptions-item>
            <a-descriptions-item :label="$t('sys.kernel')">
              {{ data.host.kernel || '-' }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.arch')">{{ data.host.arch }}</a-descriptions-item>
            <a-descriptions-item :label="$t('sys.bootTime')">
              {{ fmtTime(data.host.bootTime) }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.hostUptime')">
              {{ data.host.uptimeSeconds > 0 ? humanDuration(data.host.uptimeSeconds) : '-' }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.timezone')">
              {{ data.host.timezone || '-' }}
            </a-descriptions-item>
          </a-descriptions>

          <a-descriptions
            class="ops__desc"
            :title="$t('sys.panel')"
            :column="{ xs: 1, md: 2, xl: 3 }"
            bordered
            size="medium"
          >
            <a-descriptions-item :label="$t('sys.version')">
              {{ data.panel.version }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.commit')">
              {{ data.panel.commit || '-' }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.buildTime')">
              {{ data.panel.buildTime || '-' }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.goVersion')">
              {{ data.panel.goVersion }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.pid')">{{ data.panel.pid }}</a-descriptions-item>
            <a-descriptions-item :label="$t('sys.startedAt')">
              {{ fmtTime(data.panel.startedAt) }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.panelUptime')">
              {{ humanDuration(data.panel.uptimeSeconds) }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.goroutines')">
              {{ data.panel.goroutines }}
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.processMemory')">
              {{ $t('sys.heap') }} {{ humanSize(data.panel.heapAllocBytes) }}
              <template v-if="data.panel.rssBytes > 0">
                · {{ $t('sys.rss') }} {{ humanSize(data.panel.rssBytes) }}
              </template>
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.database')">
              <a-tag :color="data.database.ok ? 'green' : 'red'" size="small">
                {{ data.database.driver }} ·
                {{ data.database.ok ? $t('sys.healthy') : $t('sys.degraded') }}
              </a-tag>
              <span v-if="!data.database.ok" class="ops__err">{{ data.database.error }}</span>
            </a-descriptions-item>
            <a-descriptions-item :label="$t('sys.connections')">
              {{ data.database.inUse }} / {{ data.database.idle }} /
              {{ data.database.openConnections }}
            </a-descriptions-item>
            <a-descriptions-item v-if="data.panel.dataDir" :label="$t('sys.dataDir')">
              {{ data.panel.dataDir }}
            </a-descriptions-item>
          </a-descriptions>

          <a-alert type="normal" class="ops__hint">{{ $t('sys.serviceHint') }}</a-alert>
        </template>
      </a-card>

      <a-card :bordered="false" :title="$t('sys.disk')">
        <a-alert v-if="!disks.length" type="normal">
          {{ hostOK ? $t('sys.diskEmpty') : $t('sys.unsupported') }}
        </a-alert>
        <a-table v-else :data="disks" :pagination="false" size="small" row-key="path">
          <template #columns>
            <a-table-column :title="$t('sys.mountPoint')" data-index="path" ellipsis tooltip />
            <a-table-column :title="$t('sys.device')" data-index="device" ellipsis tooltip />
            <a-table-column :title="$t('sys.fsType')" data-index="fsType" :width="110" />
            <a-table-column :title="$t('sys.total')" :width="110">
              <template #cell="{ record }">{{ humanSize(record.total) }}</template>
            </a-table-column>
            <a-table-column :title="$t('sys.used')" :width="110">
              <template #cell="{ record }">{{ humanSize(record.used) }}</template>
            </a-table-column>
            <a-table-column :title="$t('sys.free')" :width="110">
              <template #cell="{ record }">{{ humanSize(record.free) }}</template>
            </a-table-column>
            <a-table-column :title="$t('sys.diskUsage')" :width="180">
              <template #cell="{ record }">
                <div class="ops__usage">
                  <a-progress
                    :percent="record.usedPercent / 100"
                    :color="usageColor(record.usedPercent)"
                    :show-text="false"
                    size="small"
                  />
                  <span>{{ percentText(record.usedPercent) }}</span>
                </div>
              </template>
            </a-table-column>
          </template>
        </a-table>
      </a-card>

      <a-card :bordered="false" :title="$t('sys.network')">
        <a-alert v-if="!nics.length" type="normal">
          {{ hostOK ? $t('sys.nicEmpty') : $t('sys.unsupported') }}
        </a-alert>
        <a-table v-else :data="nics" :pagination="false" size="small" row-key="name">
          <template #columns>
            <a-table-column :title="$t('sys.nicName')" data-index="name" :width="160" />
            <a-table-column :title="$t('sys.bytesRecv')">
              <template #cell="{ record }">{{ humanSize(record.bytesRecv) }}</template>
            </a-table-column>
            <a-table-column :title="$t('sys.bytesSent')">
              <template #cell="{ record }">{{ humanSize(record.bytesSent) }}</template>
            </a-table-column>
            <a-table-column :title="$t('sys.packets')">
              <template #cell="{ record }">
                {{ record.packetsRecv }} / {{ record.packetsSent }}
              </template>
            </a-table-column>
          </template>
        </a-table>
      </a-card>
    </template>
  </div>
</template>

<style lang="scss" scoped>
.ops {
  display: flex;
  flex-direction: column;
  gap: 16px;

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
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 16px;
  }

  &__desc {
    margin-top: 24px;
  }

  &__hint {
    margin-top: 16px;
  }

  &__err {
    margin-left: 8px;
    color: rgb(var(--danger-6));
  }

  &__usage {
    display: flex;
    align-items: center;
    gap: 8px;
    font-variant-numeric: tabular-nums;
  }
}

@media (max-width: 768px) {
  .ops__time,
  .ops__auto {
    display: none;
  }
}
</style>
