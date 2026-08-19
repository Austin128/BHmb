<script setup lang="ts">
import { IconDashboard, IconSettings, IconUserGroup } from '@arco-design/web-vue/es/icon'
import { computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import ThemeSwitch from '@/components/ThemeSwitch.vue'
import { ENV } from '@/config/env'
import { MENU_META } from '@/config/menu'
import { setI18nLocale } from '@/locales'
import { useAppStore } from '@/store/modules/app'
import { useUserStore } from '@/store/modules/user'
import { toast } from '@/utils/feedback'

const app = useAppStore()
const user = useUserStore()
const router = useRouter()
const route = useRoute()
const { t } = useI18n()

// 图标必须静态引用，否则不会进 bundle
const ICONS: Record<string, Component> = {
  dashboard: IconDashboard,
  user: IconUserGroup,
  ops: IconSettings,
}

// 菜单 = 后端 profile.menus ∩ 前端已实现模块，顺序以后端为准
const menus = computed(() =>
  user.menus
    .map((module) => MENU_META.find((m) => m.module === module))
    .filter((m): m is (typeof MENU_META)[number] => Boolean(m)),
)

const activeKey = computed(() => [route.path])

function onMenuClick(key: string) {
  if (key !== route.path) void router.push(key)
}

async function onLogout() {
  await user.logout()
  toast(t('common.logoutSuccess'), 'success')
  void router.push({ name: 'Login' })
}

function switchLocale() {
  const next = app.locale === 'zh-CN' ? 'en-US' : 'zh-CN'
  app.setLocale(next)
  setI18nLocale(next)
}
</script>

<template>
  <a-layout class="nova-layout">
    <a-layout-sider
      class="nova-sider"
      :width="220"
      :collapsed="app.collapsed"
      :collapsed-width="48"
      collapsible
      :trigger="null"
      breakpoint="lg"
      @collapse="app.setCollapsed"
    >
      <div class="nova-logo">
        <icon-storage class="nova-logo__mark" />
        <span v-show="!app.collapsed" class="nova-logo__text">{{ ENV.appTitle }}</span>
      </div>
      <a-menu
        :selected-keys="activeKey"
        :collapsed="app.collapsed"
        auto-open-selected
        @menu-item-click="onMenuClick"
      >
        <a-menu-item v-for="item in menus" :key="item.path">
          <template #icon>
            <component :is="ICONS[item.module]" />
          </template>
          {{ $t(item.titleKey) }}
        </a-menu-item>
      </a-menu>
      <!-- 后端未下发任何可见模块时给出明确空态，而不是一片空白 -->
      <div v-if="menus.length === 0 && !app.collapsed" class="nova-sider__empty">
        {{ $t('menu.empty') }}
      </div>
    </a-layout-sider>

    <a-layout>
      <a-layout-header class="nova-header">
        <a-button
          shape="circle"
          type="text"
          :aria-label="app.collapsed ? $t('common.expandSidebar') : $t('common.collapseSidebar')"
          @click="app.toggleCollapsed()"
        >
          <template #icon>
            <icon-menu-unfold v-if="app.collapsed" />
            <icon-menu-fold v-else />
          </template>
        </a-button>

        <div class="nova-header__right">
          <a-button
            shape="circle"
            type="text"
            :aria-label="$t('common.switchLanguage')"
            @click="switchLocale"
          >
            <template #icon><icon-language /></template>
          </a-button>
          <ThemeSwitch />
          <a-dropdown trigger="click">
            <div class="nova-user">
              <a-avatar :size="28" :image-url="user.user?.avatar ?? undefined">
                {{ user.nickname.slice(0, 1) }}
              </a-avatar>
              <span class="nova-user__name">{{ user.nickname }}</span>
            </div>
            <template #content>
              <a-doption disabled>
                <template #icon><icon-user /></template>
                {{ user.user?.username }}
              </a-doption>
              <a-doption @click="onLogout">
                <template #icon><icon-poweroff /></template>
                {{ $t('common.logout') }}
              </a-doption>
            </template>
          </a-dropdown>
        </div>
      </a-layout-header>

      <a-layout-content class="nova-content">
        <router-view v-slot="{ Component: Page }">
          <transition name="fade" mode="out-in">
            <component :is="Page" />
          </transition>
        </router-view>
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<style lang="scss" scoped>
.nova-layout {
  height: 100%;
}

.nova-sider {
  border-right: 1px solid var(--color-border-2);

  &__empty {
    padding: 12px 16px;
    font-size: 12px;
    color: var(--color-text-3);
  }
}

.nova-logo {
  display: flex;
  gap: 8px;
  align-items: center;
  height: var(--nova-header-height);
  padding: 0 16px;
  overflow: hidden;
  color: var(--color-text-1);

  &__mark {
    font-size: 22px;
    color: rgb(var(--primary-6));
  }

  &__text {
    font-size: 16px;
    font-weight: 600;
    white-space: nowrap;
  }
}

.nova-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--nova-header-height);
  padding: 0 16px;
  background-color: var(--color-bg-2);
  border-bottom: 1px solid var(--color-border-2);

  &__right {
    display: flex;
    gap: 8px;
    align-items: center;
  }
}

.nova-user {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 4px 8px;
  cursor: pointer;
  border-radius: var(--nova-radius);

  &:hover {
    background-color: var(--color-fill-2);
  }

  &__name {
    max-width: 120px;
    @include ellipsis;
  }
}

.nova-content {
  padding: 16px;
  overflow: auto;
  background-color: var(--color-fill-1);
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
