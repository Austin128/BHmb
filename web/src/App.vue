<script setup lang="ts">
import { computed } from 'vue'

import { ARCO_LOCALES } from '@/locales'
import { useAppStore } from '@/store/modules/app'

const app = useAppStore()
// Arco 组件内置文案跟随应用语言
const arcoLocale = computed(() => ARCO_LOCALES[app.locale])
</script>

<template>
  <a-config-provider :locale="arcoLocale">
    <router-view />
  </a-config-provider>
</template>
