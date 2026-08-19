<script setup lang="ts">
import dayjs from 'dayjs'
import { computed } from 'vue'

import { useUserStore } from '@/store/modules/user'

const user = useUserStore()

const lastLogin = computed(() => {
  const at = user.user?.lastLoginAt
  return at ? dayjs(at).format('YYYY-MM-DD HH:mm:ss') : ''
})

const roleNames = computed(() => user.roles.map((r) => r.name).join('、'))
</script>

<template>
  <div class="dash">
    <a-card :bordered="false">
      <a-typography-title :heading="5" class="dash__title">
        {{ $t('dashboard.welcome', { name: user.nickname }) }}
      </a-typography-title>
      <a-descriptions :column="{ xs: 1, md: 2 }" bordered size="medium">
        <a-descriptions-item :label="$t('dashboard.account')">
          {{ user.user?.username }}
          <a-tag v-if="user.isSuperAdmin" color="arcoblue" size="small">super</a-tag>
        </a-descriptions-item>
        <a-descriptions-item :label="$t('dashboard.roles')">
          {{ roleNames || '-' }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('dashboard.permissions')">
          {{ user.permissions.includes('*') ? '*' : user.permissions.length }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('dashboard.lastLogin')">
          {{ lastLogin || $t('dashboard.never') }}
        </a-descriptions-item>
      </a-descriptions>
    </a-card>

    <a-card :bordered="false" class="dash__hint">
      <a-empty :description="$t('dashboard.placeholder')" />
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

  &__hint {
    flex: 1;
  }
}
</style>
