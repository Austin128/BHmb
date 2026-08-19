<script setup lang="ts">
import { computed } from 'vue'

import { useAppStore } from '@/store/modules/app'
import type { ThemeMode } from '@/utils/theme'

// 三态主题切换：浅色 / 深色 / 跟随系统。选中值持久化在 app store。
const app = useAppStore()

const options = computed<Array<{ value: ThemeMode; label: string; icon: string }>>(() => [
  { value: 'light', label: 'theme.light', icon: 'icon-sun' },
  { value: 'dark', label: 'theme.dark', icon: 'icon-moon' },
  { value: 'auto', label: 'theme.auto', icon: 'icon-desktop' },
])

function onSelect(value: string | number | Record<string, unknown> | undefined) {
  const mode = String(value) as ThemeMode
  if (mode === 'light' || mode === 'dark' || mode === 'auto') app.setTheme(mode)
}
</script>

<template>
  <a-dropdown trigger="click" @select="onSelect">
    <a-tooltip :content="$t(`theme.${app.theme}`)">
      <a-button shape="circle" type="text" :aria-label="$t(`theme.${app.theme}`)">
        <template #icon>
          <icon-moon-fill v-if="app.isDark" />
          <icon-sun-fill v-else />
        </template>
      </a-button>
    </a-tooltip>
    <template #content>
      <a-doption v-for="item in options" :key="item.value" :value="item.value">
        <template #icon>
          <icon-check v-if="app.theme === item.value" />
        </template>
        {{ $t(item.label) }}
      </a-doption>
    </template>
  </a-dropdown>
</template>
