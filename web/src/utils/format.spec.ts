import { describe, expect, it } from 'vitest'

import { clampPercent, humanDuration, humanSize, percentText, usageStatus } from '@/utils/format'

describe('humanSize', () => {
  it('小于 1KB 时按字节显示', () => {
    expect(humanSize(0)).toBe('0 B')
    expect(humanSize(512)).toBe('512 B')
  })

  it('按 1024 进制逐级换算', () => {
    expect(humanSize(1024)).toBe('1.0 KB')
    expect(humanSize(1024 * 1024 * 3.5)).toBe('3.5 MB')
    expect(humanSize(1024 ** 3 * 2)).toBe('2.0 GB')
  })

  it('负数与非法值不产生 NaN', () => {
    expect(humanSize(-1)).toBe('0 B')
    expect(humanSize(Number.NaN)).toBe('0 B')
  })
})

describe('humanDuration', () => {
  it('不足一分钟显示秒', () => {
    expect(humanDuration(0)).toBe('0s')
    expect(humanDuration(59)).toBe('59s')
  })

  it('只保留最大的两个量级', () => {
    expect(humanDuration(3600 * 26 + 120)).toBe('1d 2h')
    expect(humanDuration(3600 * 5 + 61)).toBe('5h 1m')
    expect(humanDuration(600)).toBe('10m')
  })

  it('整天整小时不带多余单位', () => {
    expect(humanDuration(86400)).toBe('1d')
    expect(humanDuration(7200)).toBe('2h')
  })

  it('非法值返回占位符', () => {
    expect(humanDuration(-5)).toBe('-')
    expect(humanDuration(Number.NaN)).toBe('-')
  })
})

describe('percent helpers', () => {
  it('夹取到 0-100 防止进度条越界', () => {
    expect(clampPercent(-3)).toBe(0)
    expect(clampPercent(142)).toBe(100)
    expect(clampPercent(42.5)).toBe(42.5)
  })

  it('百分比文本保留一位小数', () => {
    expect(percentText(42.567)).toBe('42.6%')
    expect(percentText(Number.NaN)).toBe('0.0%')
  })

  it('按阈值给出状态', () => {
    expect(usageStatus(10)).toBe('normal')
    expect(usageStatus(75)).toBe('warning')
    expect(usageStatus(90)).toBe('danger')
    expect(usageStatus(120)).toBe('danger')
  })
})
