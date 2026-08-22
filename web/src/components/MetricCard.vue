<script setup lang="ts">
/**
 * 资源占用卡片：总览页与运维页共用。
 * available 为 false 时显示「不可用」而不是 0%，避免让人以为资源真的空闲。
 */
import { computed } from 'vue'

import { clampPercent, percentText, usageColor, usageStatus } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    title: string
    percent: number
    /** 主标注，如「1.2 GB / 3.8 GB」 */
    primary?: string
    /** 次标注，如负载或交换分区 */
    secondaryLabel?: string
    secondaryValue?: string
    available?: boolean
  }>(),
  { available: true, primary: '', secondaryLabel: '', secondaryValue: '' },
)

const value = computed(() => clampPercent(props.percent))
const text = computed(() => (props.available ? percentText(props.percent) : '—'))
const color = computed(() => usageColor(props.percent))
const status = computed(() => usageStatus(props.percent))
</script>

<template>
  <div class="metric" :class="[`metric--${status}`, { 'metric--off': !available }]">
    <div class="metric__head">
      <span class="metric__title">{{ title }}</span>
      <span class="metric__value">{{ text }}</span>
    </div>

    <a-progress
      :percent="available ? value / 100 : 0"
      :color="color"
      :show-text="false"
      size="large"
      track-color="var(--color-fill-3)"
    />

    <div class="metric__foot">
      <span class="metric__primary">{{ available ? primary || '-' : $t('sys.unavailable') }}</span>
      <span v-if="available && secondaryLabel" class="metric__secondary">
        {{ secondaryLabel }} {{ secondaryValue }}
      </span>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.metric {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 16px;
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
  background: var(--color-bg-2);

  &__head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }

  &__title {
    color: var(--color-text-2);
    font-size: 13px;
  }

  &__value {
    font-size: 22px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    line-height: 1.2;
  }

  &__foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    color: var(--color-text-3);
    font-size: 12px;
    /* 长挂载路径不能把卡片撑破 */
    min-width: 0;
  }

  &__primary,
  &__secondary {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  &--danger &__value {
    color: rgb(var(--danger-6));
  }

  &--warning &__value {
    color: rgb(var(--warning-6));
  }

  &--off &__value {
    color: var(--color-text-4);
  }
}
</style>
