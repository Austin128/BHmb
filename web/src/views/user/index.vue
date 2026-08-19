<script setup lang="ts">
import { useUserStore } from '@/store/modules/user'

// M0 只交付认证链路：用户与角色的增删改在「用户与权限」里程碑落地。
// 这里展示当前登录者的权限点，便于验证 RBAC 下发是否正确。
const user = useUserStore()
</script>

<template>
  <a-card :bordered="false" :title="$t('menu.user')">
    <a-space wrap>
      <a-tag v-for="code in user.permissions" :key="code" color="arcoblue">{{ code }}</a-tag>
    </a-space>
    <a-empty v-if="user.permissions.length === 0" />
  </a-card>
</template>
