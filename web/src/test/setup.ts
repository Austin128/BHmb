/**
 * 组件单测的 jsdom 补齐：Arco 的栅格与部分弹层依赖 matchMedia / ResizeObserver，
 * jsdom 里没有这两个 API，缺失时会在 mounted 钩子里抛错并把组件卸载掉。
 * 刻意用普通函数而不是 vi.fn：restoreMocks 会在每个用例前清掉 mock 实现，
 * 用 mock 写 polyfill 会从第二个用例起返回 undefined。
 */
if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
}

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}
