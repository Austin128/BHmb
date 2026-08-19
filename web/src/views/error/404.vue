<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <a-result status="404" :subtitle="$t('error.notFound')" class="page-result">
    <template #extra>
      <a-button type="primary" @click="router.replace('/')">{{ $t('common.back') }}</a-button>
    </template>
  </a-result>
</template>

<style scoped>
.page-result {
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-height: 60vh;
}
</style>
