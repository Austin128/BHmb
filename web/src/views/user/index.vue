<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted, ref } from 'vue'

import {
  fetchPermissions,
  fetchRoles,
  fetchUsers,
  type PermissionItem,
  type RoleItem,
  type UserItem,
} from '@/api/admin'
import { useUserStore } from '@/store/modules/user'
import { BizError } from '@/types/api'

const store = useUserStore()

/* 写操作只在 novactl / bh 提供，这里全部是只读视图，不放无效按钮。 */
const canListUsers = computed(() => store.hasPermission('user:user:list'))
const canListRoles = computed(() => store.hasPermission('user:role:list'))
const canListPermissions = computed(() => store.hasPermission('user:permission:list'))
const anyAllowed = computed(
  () => canListUsers.value || canListRoles.value || canListPermissions.value,
)

const users = ref<UserItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const roles = ref<RoleItem[]>([])
const permissions = ref<PermissionItem[]>([])

const loading = ref(false)
const error = ref('')

/** 权限点按模块分组展示，避免 30 多个 code 平铺成一堆标签。 */
const permissionGroups = computed(() => {
  const groups = new Map<string, PermissionItem[]>()
  for (const item of permissions.value) {
    const list = groups.get(item.module) ?? []
    list.push(item)
    groups.set(item.module, list)
  }
  return [...groups.entries()].map(([module, items]) => ({ module, items }))
})

const grantedCount = computed(() => permissions.value.filter((p) => p.granted).length)

function statusText(u: UserItem) {
  if (u.lockedUntil && dayjs(u.lockedUntil).isAfter(dayjs())) return 'locked'
  return u.status
}

function statusLabel(u: UserItem) {
  const s = statusText(u)
  if (s === 'locked') return 'user.statusLocked'
  if (s === 'active') return 'user.statusActive'
  return 'user.statusDisabled'
}

function statusColor(u: UserItem) {
  const s = statusText(u)
  if (s === 'locked') return 'orange'
  return s === 'active' ? 'green' : 'red'
}

function scopeLabel(scope: string) {
  if (scope === 'all') return 'user.dataScopeAll'
  if (scope === 'self') return 'user.dataScopeSelf'
  return 'user.dataScopeTenant'
}

function fmtTime(value?: string) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '-'
}

async function loadUsers() {
  if (!canListUsers.value) return
  const res = await fetchUsers({ page: page.value, pageSize: pageSize.value })
  users.value = res.list ?? []
  total.value = res.total
  page.value = res.page
  pageSize.value = res.pageSize
}

async function load() {
  loading.value = true
  try {
    await Promise.all([
      loadUsers(),
      canListRoles.value ? fetchRoles().then((r) => (roles.value = r ?? [])) : Promise.resolve(),
      canListPermissions.value
        ? fetchPermissions().then((p) => (permissions.value = p ?? []))
        : Promise.resolve(),
    ])
    error.value = ''
  } catch (e) {
    error.value = e instanceof BizError ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

async function onPageChange(next: number) {
  page.value = next
  loading.value = true
  try {
    await loadUsers()
    error.value = ''
  } catch (e) {
    error.value = e instanceof BizError ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <a-card :bordered="false" :title="$t('menu.user')">
    <template #extra>
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><icon-refresh /></template>
        {{ $t('sys.refresh') }}
      </a-button>
    </template>

    <a-alert v-if="!anyAllowed" type="info">{{ $t('error.forbidden') }}</a-alert>

    <template v-else>
      <a-alert v-if="error" type="error" class="usr__alert">{{ error }}</a-alert>

      <a-tabs>
        <a-tab-pane v-if="canListUsers" key="accounts" :title="$t('user.accounts')">
          <a-table
            :data="users"
            :loading="loading"
            :pagination="{
              current: page,
              pageSize,
              total,
              showTotal: true,
              hideOnSinglePage: true,
            }"
            row-key="id"
            size="small"
            @page-change="onPageChange"
          >
            <template #columns>
              <a-table-column :title="$t('user.username')" :width="200">
                <template #cell="{ record }">
                  {{ record.username }}
                  <a-tag v-if="record.isSuper" color="arcoblue" size="small">super</a-tag>
                </template>
              </a-table-column>
              <a-table-column :title="$t('user.nickname')" data-index="nickname" ellipsis tooltip />
              <a-table-column :title="$t('user.roles')" :width="180">
                <template #cell="{ record }">
                  <a-space wrap size="mini">
                    <a-tag v-for="name in record.roles" :key="name" size="small">{{ name }}</a-tag>
                    <span v-if="!record.roles.length">-</span>
                  </a-space>
                </template>
              </a-table-column>
              <a-table-column :title="$t('user.status')" :width="150">
                <template #cell="{ record }">
                  <a-space size="mini" wrap>
                    <a-tag :color="statusColor(record)" size="small">
                      {{ $t(statusLabel(record)) }}
                    </a-tag>
                    <a-tag v-if="record.mustChangePassword" color="orange" size="small">
                      {{ $t('user.mustChangePassword') }}
                    </a-tag>
                  </a-space>
                </template>
              </a-table-column>
              <a-table-column :title="$t('user.twoFA')" :width="110">
                <template #cell="{ record }">
                  {{ record.twoFABound ? $t('user.twoFAOn') : $t('user.twoFAOff') }}
                </template>
              </a-table-column>
              <a-table-column :title="$t('user.lastLogin')" :width="220">
                <template #cell="{ record }">
                  {{ fmtTime(record.lastLoginAt) }}
                  <span v-if="record.lastLoginIp" class="usr__ip">{{ record.lastLoginIp }}</span>
                </template>
              </a-table-column>
              <a-table-column :title="$t('user.loginFail')" data-index="loginFailCount" :width="110" />
              <a-table-column :title="$t('user.createdAt')" :width="190">
                <template #cell="{ record }">{{ fmtTime(record.createdAt) }}</template>
              </a-table-column>
            </template>
          </a-table>
          <a-alert type="normal" class="usr__hint">{{ $t('user.manageHint') }}</a-alert>
        </a-tab-pane>

        <a-tab-pane v-if="canListRoles" key="roles" :title="$t('user.roles')">
          <a-table :data="roles" :loading="loading" :pagination="false" row-key="id" size="small">
            <template #columns>
              <a-table-column :title="$t('user.roleName')" data-index="name" :width="160" />
              <a-table-column :title="$t('user.roleCode')" data-index="code" :width="160" />
              <a-table-column :title="$t('user.dataScope')" :width="140">
                <template #cell="{ record }">{{ $t(scopeLabel(record.dataScope)) }}</template>
              </a-table-column>
              <a-table-column :title="$t('user.permissionCount')" :width="130">
                <template #cell="{ record }">
                  <a-tag v-if="record.allPermissions" color="arcoblue" size="small">
                    {{ $t('user.allPermissions') }}
                  </a-tag>
                  <span v-else>{{ record.permissionCount }}</span>
                </template>
              </a-table-column>
              <a-table-column :title="$t('user.builtin')" :width="120">
                <template #cell="{ record }">
                  <a-tag :color="record.isBuiltin ? 'blue' : 'gray'" size="small">
                    {{ record.isBuiltin ? $t('user.builtin') : $t('user.custom') }}
                  </a-tag>
                </template>
              </a-table-column>
              <a-table-column
                :title="$t('user.description')"
                data-index="description"
                ellipsis
                tooltip
              />
            </template>
          </a-table>
        </a-tab-pane>

        <a-tab-pane v-if="canListPermissions" key="permissions" :title="$t('user.permissions')">
          <a-space class="usr__summary" size="medium">
            <span>{{ $t('user.myPermissions') }}</span>
            <a-tag color="arcoblue">
              {{ store.permissions.includes('*') ? $t('user.allPermissions') : grantedCount }}
              / {{ permissions.length }}
            </a-tag>
          </a-space>

          <a-spin :loading="loading" style="width: 100%">
            <div v-for="group in permissionGroups" :key="group.module" class="usr__group">
              <div class="usr__module">{{ group.module }}</div>
              <a-space wrap size="mini">
                <a-tooltip v-for="item in group.items" :key="item.code" :content="item.code">
                  <a-tag
                    :color="item.granted ? 'arcoblue' : undefined"
                    :class="{ 'usr__perm--off': !item.granted }"
                  >
                    {{ item.name }}
                    <template v-if="item.isSensitive"> · {{ $t('user.sensitive') }}</template>
                  </a-tag>
                </a-tooltip>
              </a-space>
            </div>
            <a-empty v-if="!loading && permissionGroups.length === 0" />
          </a-spin>
        </a-tab-pane>
      </a-tabs>
    </template>
  </a-card>
</template>

<style lang="scss" scoped>
.usr {
  &__alert {
    margin-bottom: 16px;
  }

  &__hint {
    margin-top: 16px;
  }

  &__ip {
    margin-left: 8px;
    color: var(--color-text-3);
    font-size: 12px;
  }

  &__summary {
    margin-bottom: 16px;
    color: var(--color-text-2);
  }

  &__group {
    padding-bottom: 12px;

    & + & {
      border-top: 1px solid var(--color-border-2);
      padding-top: 12px;
    }
  }

  &__module {
    margin-bottom: 8px;
    color: var(--color-text-3);
    font-size: 12px;
    text-transform: uppercase;
  }

  &__perm--off {
    opacity: 0.55;
  }
}
</style>
