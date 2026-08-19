<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { fetchHealth, type HealthStatus } from '@/api/system'

const health = ref<HealthStatus | null>(null)
const loading = ref(true)

async function load() {
  loading.value = true
  try {
    health.value = await fetchHealth()
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <a-card :bordered="false" :title="$t('menu.ops')">
    <template #extra>
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><icon-refresh /></template>
      </a-button>
    </template>

    <a-spin :loading="loading" style="width: 100%">
      <a-descriptions v-if="health" :column="{ xs: 1, md: 2 }" bordered>
        <a-descriptions-item label="status">
          <a-tag :color="health.status === 'ok' ? 'green' : 'orange'">{{ health.status }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="version">{{ health.version }}</a-descriptions-item>
        <a-descriptions-item label="uptime">{{ health.uptimeSeconds }}s</a-descriptions-item>
        <a-descriptions-item label="database">
          {{ health.database.driver }} · {{ health.database.ok ? 'ok' : health.database.error }}
        </a-descriptions-item>
      </a-descriptions>
    </a-spin>
  </a-card>
</template>
