# NovaPanel（星舵面板）· 前端架构与 Arco Design 规范

## 文档信息

| 项目 | 内容 |
| --- | --- |
| 文档名称 | 06-前端架构与Arco规范 |
| 文档版本 | v1.0 |
| 更新日期 | 2026-08-17 |
| 状态 | 定稿 |
| 适用版本 | NovaPanel v1.0 |
| 产品代号 | nova |
| 覆盖内容 | 前端分层、工程初始化、请求层、路由与权限、Pinia、布局、Arco 令牌体系、业务组件、实时数据、表单校验、页面范式、i18n、性能、可访问性、质量门禁、Mock |
| 关联文档 | [系统架构设计](./02-系统架构设计.md)、[技术栈与工程规范](./03-技术栈与工程规范.md)、[API 接口规范](./05-API接口规范.md)、[文件管理与 WebSSH](./08-文件管理与WebSSH.md)、[监控告警与可观测](./11-监控告警与可观测.md) |

## 目录

- [6.1 前端整体架构与分层](#61-前端整体架构与分层)
- [6.2 工程初始化与构建配置](#62-工程初始化与构建配置)
- [6.3 目录结构与命名规范](#63-目录结构与命名规范)
- [6.4 请求层设计](#64-请求层设计)
- [6.5 路由设计与菜单生成](#65-路由设计与菜单生成)
- [6.6 状态管理设计](#66-状态管理设计)
- [6.7 布局系统](#67-布局系统)
- [6.8 Arco Design 使用规范与设计令牌](#68-arco-design-使用规范与设计令牌)
- [6.9 通用业务组件封装规范](#69-通用业务组件封装规范)
- [6.10 权限控制](#610-权限控制)
- [6.11 实时数据方案](#611-实时数据方案)
- [6.12 表单与校验](#612-表单与校验)
- [6.13 页面模板与交互范式](#613-页面模板与交互范式)
- [6.14 国际化](#614-国际化)
- [6.15 性能优化](#615-性能优化)
- [6.16 可访问性与响应式](#616-可访问性与响应式)
- [6.17 代码规范与质量保障](#617-代码规范与质量保障)
- [6.18 前后端联调与 Mock](#618-前后端联调与-mock)

## 6.1 前端整体架构与分层

### 6.1.1 分层示意

```text
┌──────────────────────────────────────────────────────────────┐
│ 视图层 views/ + layout/                                       │
│  页面组件只做三件事：组装业务组件、调用 hooks、渲染状态         │
│  禁止：直接 import axios、直接读写 localStorage、拼接 URL       │
├──────────────────────────────────────────────────────────────┤
│ 组件层 components/ + directives/                              │
│  ProTable / ChartWrapper / Terminal / CodeEditor …            │
│  只依赖 props 与 emits，可被任意模块复用，不 import store       │
├──────────────────────────────────────────────────────────────┤
│ 逻辑层 hooks/                                                 │
│  useTable / usePermission / useWebSocket / useNodeScope …      │
│  组合 store 与 api，输出响应式状态与动作，是视图的唯一入口       │
├──────────────────────────────────────────────────────────────┤
│ 状态层 store/modules/                                         │
│  user / permission / app / node / tab / notification / terminal│
│  跨页面共享的状态，禁止存放单页面私有的临时状态                  │
├──────────────────────────────────────────────────────────────┤
│ 服务层 api/                                                   │
│  按模块拆文件，纯函数：入参 → request() → 返回泛型化 data        │
│  唯一允许出现后端 path 字符串的地方                             │
├──────────────────────────────────────────────────────────────┤
│ 基础设施层 utils/ + config/ + types/                          │
│  request.ts（Axios）、ws.ts（WebSocket）、storage.ts、          │
│  validator.ts、echarts.ts、i18n、路由表、常量、全局类型          │
└──────────────────────────────────────────────────────────────┘
```

### 6.1.2 数据流向

1. 读路径：视图挂载 → hooks 触发 → `api/*` 调用 `request()` → 拦截器校验 `code===0` 并解构 `data` → hooks 写入本地 `ref` 或 store → 视图渲染。
2. 写路径：视图交互 → hooks 动作（含危险操作二次确认）→ `api/*` → 成功后 `Message.success` 并刷新列表，失败由拦截器统一提示，视图只处理需要特殊分支的错误码。
3. 推路径：`utils/ws.ts` 单例连接 → 按 `topic` 分发 → 订阅方（monitor 图表、terminal 会话、notification 铃铛）在 `onMounted` 订阅、`onUnmounted` 退订。
4. 节点上下文：`node` store 的 `currentNodeId` 是唯一真源，`request.ts` 与 `ws.ts` 均从它取值注入 `X-Node-Id`，业务代码不得手工传该请求头。

### 6.1.3 分层约束（可被 ESLint 规则固化）

| 规则 | 说明 |
| --- | --- |
| `views` 不得 import `axios` | 只能通过 `api/` |
| `components` 不得 import `store` | 通过 props/emits 传入，保证可测试与可复用 |
| `api` 不得 import `store` | 避免循环依赖，节点上下文由拦截器注入 |
| `utils` 不得 import `views`/`components` | 基础设施层不反向依赖 |
| 单文件 ≤ 400 行，`<script setup>` ≤ 200 行 | 超出即拆 hooks 或子组件 |

## 6.2 工程初始化与构建配置

### 6.2.1 初始化命令

前端工程位于仓库的 `web/` 目录，与后端同仓库。

```bash
pnpm create vite@latest web -- --template vue-ts
cd web
pnpm add @arco-design/web-vue@2.56.3 pinia@2.2.6 pinia-plugin-persistedstate@4.1.3 \
  vue-router@4.4.5 axios@1.7.9 echarts@5.5.1 vue-i18n@9.14.1 \
  @xterm/xterm@5.5.0 @xterm/addon-fit@0.10.0 @xterm/addon-webgl@0.18.0 \
  @xterm/addon-search@0.15.0 monaco-editor@0.52.2 dayjs@1.11.13 lodash-es@4.17.21
pnpm add -D typescript@5.6.3 vite@6.0.7 @vitejs/plugin-vue@5.2.1 \
  vue-tsc@2.1.10 sass-embedded@1.83.0 less@4.2.1 \
  unplugin-vue-components@0.28.0 unplugin-auto-import@0.19.0 \
  vite-plugin-compression@0.5.1 vite-plugin-svg-icons@2.0.1 \
  vitest@2.1.8 @vue/test-utils@2.4.6 jsdom@25.0.1 @playwright/test@1.49.1 \
  msw@2.7.0 @types/lodash-es@4.17.12
pnpm exec playwright install chromium
```

依赖版本一律写死（无 `^`/`~`），升级走评审，与 [技术栈与工程规范](./03-技术栈与工程规范.md) 的版本基线保持一致。

### 6.2.2 package.json

```json
{
  "name": "novapanel-web",
  "private": true,
  "version": "1.0.0",
  "type": "module",
  "packageManager": "pnpm@9.15.0",
  "engines": { "node": ">=20.11.0", "pnpm": ">=9.0.0" },
  "scripts": {
    "dev": "vite",
    "build": "vue-tsc --noEmit -p tsconfig.build.json && vite build",
    "build:analyze": "vite build --mode analyze",
    "preview": "vite preview --port 5174",
    "typecheck": "vue-tsc --noEmit",
    "lint": "eslint . --cache --max-warnings 0",
    "lint:fix": "eslint . --fix",
    "lint:style": "stylelint \"src/**/*.{vue,scss,css}\" --max-warnings 0",
    "format": "prettier --write \"src/**/*.{ts,vue,scss,json}\"",
    "test": "vitest run --coverage",
    "test:watch": "vitest",
    "test:e2e": "playwright test",
    "prepare": "cd .. && husky web/.husky"
  }
}
```

`dependencies` / `devDependencies` 字段按 6.2.1 的安装结果落盘，不在文档内重复罗列，以 `pnpm-lock.yaml` 为准。

### 6.2.3 vite.config.ts

构建产物直接输出到后端 `embed` 目录，Go 侧用 `//go:embed all:dist` 挂载（`all:` 前缀保证以 `_`、`.` 开头的资源不被忽略）。

```ts
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ArcoResolver } from 'unplugin-vue-components/resolvers'
import { createSvgIconsPlugin } from 'vite-plugin-svg-icons'
import compression from 'vite-plugin-compression'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'VITE_')
  return {
    base: env.VITE_PUBLIC_PATH || '/',
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
        '#': fileURLToPath(new URL('./types', import.meta.url)),
      },
    },
    plugins: [
      vue(),
      AutoImport({
        imports: ['vue', 'vue-router', 'pinia', 'vue-i18n'],
        resolvers: [ArcoResolver({ sideEffect: true })],
        dirs: ['./src/hooks/**', './src/store/modules/**'],
        dts: './types/auto-imports.d.ts',
        eslintrc: { enabled: true, filepath: './.eslintrc-auto-import.json' },
      }),
      Components({
        dirs: ['src/components'],
        deep: true,
        resolvers: [ArcoResolver({ sideEffect: true, resolveIcons: true })],
        dts: './types/components.d.ts',
      }),
      createSvgIconsPlugin({
        iconDirs: [fileURLToPath(new URL('./src/assets/icons', import.meta.url))],
        symbolId: 'nova-icon-[name]',
      }),
      compression({ algorithm: 'gzip', ext: '.gz', threshold: 10240, deleteOriginFile: false }),
      compression({ algorithm: 'brotliCompress', ext: '.br', threshold: 10240 }),
    ],
    css: {
      preprocessorOptions: {
        scss: {
          api: 'modern-compiler',
          additionalData: `@use "@/styles/mixin.scss" as *;`,
        },
        less: { javascriptEnabled: true },
      },
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      proxy: {
        '/api': {
          target: env.VITE_PROXY_TARGET || 'https://127.0.0.1:34567',
          changeOrigin: true,
          secure: false,
          ws: true,
        },
      },
    },
    build: {
      outDir: '../internal/web/dist',
      emptyOutDir: true,
      target: 'es2020',
      sourcemap: mode !== 'production',
      chunkSizeWarningLimit: 1024,
      reportCompressedSize: false,
      rollupOptions: {
        output: {
          chunkFileNames: 'assets/js/[name]-[hash].js',
          entryFileNames: 'assets/js/[name]-[hash].js',
          assetFileNames: 'assets/[ext]/[name]-[hash].[ext]',
          manualChunks(id) {
            if (!id.includes('node_modules')) return
            if (id.includes('monaco-editor')) return 'monaco'
            if (id.includes('@xterm')) return 'xterm'
            if (id.includes('echarts') || id.includes('zrender')) return 'echarts'
            if (id.includes('@arco-design')) return 'arco'
            if (/[\\/](vue|vue-router|pinia|@vue)[\\/]/.test(id)) return 'vue'
            return 'vendor'
          },
        },
      },
    },
  }
})
```

> `secure: false` 用于放行面板开发环境的自签名证书；`ws: true` 覆盖 `/api/v1/ws/*` 的握手代理。前端不重写路径，`/api/v1` 前缀原样透传，避免开发与生产环境的路径差异。

分包目标与 gzip 体积预算见 [6.15.3 首屏体积预算](#6153-首屏体积预算)。`monaco` 与 `xterm` 两个 chunk 只在终端页与编辑器页按需加载，不进首屏。

### 6.2.4 tsconfig.json

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "jsx": "preserve",
    "strict": true,
    "noImplicitOverride": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "exactOptionalPropertyTypes": false,
    "verbatimModuleSyntax": true,
    "useDefineForClassFields": true,
    "isolatedModules": true,
    "skipLibCheck": true,
    "resolveJsonModule": true,
    "esModuleInterop": true,
    "allowImportingTsExtensions": false,
    "noEmit": true,
    "baseUrl": ".",
    "paths": { "@/*": ["src/*"], "#/*": ["types/*"] },
    "types": ["vite/client", "vitest/globals"]
  },
  "include": ["src/**/*.ts", "src/**/*.d.ts", "src/**/*.vue", "types/**/*.d.ts", "vite.config.ts"],
  "exclude": ["node_modules", "dist", "e2e"]
}
```

`tsconfig.build.json` 继承上述配置并 `exclude` 测试文件（`**/*.spec.ts`、`**/__tests__/**`），保证生产构建的类型检查不被测试类型污染。

### 6.2.5 环境变量清单

`.env.development`：

```bash
VITE_APP_TITLE=NovaPanel
VITE_PUBLIC_PATH=/
VITE_API_BASE_URL=/api/v1
VITE_WS_BASE_URL=/api/v1/ws
VITE_PROXY_TARGET=https://127.0.0.1:34567
VITE_REQUEST_TIMEOUT=20000
VITE_ENABLE_MOCK=true
VITE_ENABLE_DEVTOOLS=true
VITE_LOG_LEVEL=debug
```

`.env.production`：

```bash
VITE_APP_TITLE=NovaPanel
VITE_PUBLIC_PATH=/
VITE_API_BASE_URL=/api/v1
VITE_WS_BASE_URL=/api/v1/ws
VITE_REQUEST_TIMEOUT=30000
VITE_ENABLE_MOCK=false
VITE_ENABLE_DEVTOOLS=false
VITE_LOG_LEVEL=error
```

| 变量 | 类型 | 说明 |
| --- | --- | --- |
| `VITE_APP_TITLE` | string | 浏览器标题后缀、登录页品牌名 |
| `VITE_PUBLIC_PATH` | string | 资源前缀，面板挂子路径时改此项 |
| `VITE_API_BASE_URL` | string | Axios `baseURL`，生产同源无需域名 |
| `VITE_WS_BASE_URL` | string | WebSocket 路径前缀，协议由页面协议推导为 `wss:` |
| `VITE_PROXY_TARGET` | string | 仅开发生效，指向本机面板 34567 |
| `VITE_REQUEST_TIMEOUT` | number | 毫秒；长任务接口在调用处单独放宽 |
| `VITE_ENABLE_MOCK` | boolean | 是否启动 msw worker |
| `VITE_ENABLE_DEVTOOLS` | boolean | Pinia/Router 调试日志开关 |
| `VITE_LOG_LEVEL` | enum | `debug`/`info`/`warn`/`error`，控制 `utils/logger.ts` |

环境变量类型在 `types/env.d.ts` 中通过 `ImportMetaEnv` 接口声明，禁止在业务代码里 `import.meta.env` 后再做字符串解析，统一由 `config/env.ts` 转型后导出。


## 6.3 目录结构与命名规范

### 6.3.1 目录树

```text
web/
├── src/
│   ├── api/            # 服务层，按业务模块一文件
│   ├── assets/         # 静态资源（icons/ 为 SVG sprite 源目录）
│   ├── components/     # 通用与业务通用组件
│   ├── config/         # 环境转型、常量、字典、菜单图标映射
│   ├── directives/     # v-permission / v-copy / v-loading-text
│   ├── hooks/          # 组合式逻辑
│   ├── layout/         # 布局壳与其子组件
│   ├── router/         # 路由表、守卫、进度
│   ├── store/          # Pinia 根与 modules/
│   ├── styles/         # theme.scss / mixin.scss / arco-override.scss
│   ├── types/          # 跨模块共享类型
│   ├── utils/          # request / ws / storage / validator / echarts
│   └── views/          # 页面，views/<module>/<page>/index.vue
├── types/              # 全局声明与插件生成的 d.ts
├── e2e/                # Playwright 用例
└── public/
```

### 6.3.2 逐目录职责

| 目录 | 职责 | 允许依赖 | 禁止 |
| --- | --- | --- | --- |
| `api/` | 定义接口函数与请求/响应类型，一模块一文件（`website.ts`、`container.ts`…） | `utils/request`、`types/` | 引 store、写业务判断、弹提示 |
| `assets/` | 图片、字体、`icons/*.svg`（sprite 源） | — | 放超过 100KB 的位图，改用 CDN 或 SVG |
| `components/` | 无业务耦合的可复用组件，一组件一目录（`ProTable/index.vue` + `types.ts`） | `hooks/`、`utils/` | 引 store、引 `api/`、硬编码文案 |
| `config/` | `env.ts` 环境转型、`constants.ts`、`dict.ts` 状态字典、`icons.ts` 菜单图标 | `types/` | 放函数逻辑 |
| `directives/` | 全局指令，统一在 `directives/index.ts` 注册 | `store/`、`utils/` | 直接操作业务 DOM 结构 |
| `hooks/` | 组合式逻辑，`useXxx.ts` 一功能一文件 | `api/`、`store/`、`utils/` | 渲染 DOM、引 `views/` |
| `layout/` | 布局壳（`index.vue`）与顶栏/侧栏/页签子组件 | 全部 | 承载业务页面逻辑 |
| `router/` | `routes/` 静态路由表按模块拆分、`guard.ts` 守卫、`index.ts` 实例 | `store/`、`utils/` | 发起业务请求（权限拉取走 store） |
| `store/` | `modules/*.ts`，一 store 一文件，`defineStore` 组合式写法 | `api/`、`utils/` | 引 `components/`、`views/` |
| `styles/` | 令牌、mixin、Arco 覆盖、重置样式 | — | 写业务页面样式（页面样式就近写 `<style scoped>`） |
| `types/` | 跨 3 个以上文件使用的类型、后端实体类型 | — | 放实现代码 |
| `utils/` | 与框架无关的基础设施，纯函数或单例 | `config/`、`types/` | 引 `store/`（`request.ts` 例外，通过懒引用取节点上下文） |
| `views/` | 页面，`views/<module>/<page>/index.vue`，页面私有组件放同级 `components/` | 全部 | 直接引 `axios`、直接读 `localStorage` |


### 6.3.3 命名规范

| 对象 | 规范 | 示例 |
| --- | --- | --- |
| 组件目录与 SFC | PascalCase，目录名即组件名，入口 `index.vue` | `components/ProTable/index.vue` → `<ProTable />` |
| 页面 SFC | 固定 `index.vue`，目录用 kebab-case | `views/website/site-list/index.vue` |
| 页面私有组件 | PascalCase，放页面目录下 `components/` | `views/website/site-list/components/SslPanel.vue` |
| hooks | `use` 前缀 + camelCase 文件名 | `hooks/useTable.ts` 导出 `useTable` |
| store | `modules/<name>.ts`，导出 `use<Name>Store`，`defineStore('<name>')` | `store/modules/node.ts` → `useNodeStore` |
| api | `<module>.ts`，函数名「动词 + 资源」 | `api/website.ts` → `listWebsites`、`createWebsite` |
| 类型 | 接口 PascalCase 无 `I` 前缀，请求体 `<Op><Entity>Payload`，查询 `<Entity>Query` | `CreateWebsitePayload`、`WebsiteQuery` |
| 常量 | `UPPER_SNAKE_CASE`，联合字面量类型优先于 `enum` | `WEBSITE_STATUS_MAP` |
| 事件名 | kebab-case，`emits` 显式声明 | `@row-select`、`@refresh` |
| SCSS class | `nova-<block>__<element>--<modifier>` | `.nova-tabs__item--active` |
| SVG 图标 | kebab-case 文件名，使用时 `<SvgIcon name="node-online" />` | `assets/icons/node-online.svg` |

### 6.3.4 类型放置边界

- 放 `src/types/`：后端实体（`Website`、`Node`、`Container`）、跨模块枚举、`ApiResponse`/`PageResult` 等协议类型、路由 `meta` 类型。
- 放 `api/<module>.ts` 就近导出：仅该模块使用的请求体与查询参数类型，与接口函数同文件便于对照 [API 接口规范](./05-API接口规范.md)。
- 放组件目录 `types.ts`：组件 props/emits 的复合类型（如 `ProTableColumn`），对外通过组件目录导出。
- 放 `types/*.d.ts`：全局声明、`ImportMetaEnv`、第三方库补丁类型；插件生成的 `auto-imports.d.ts`、`components.d.ts` 纳入版本管理但禁止手改。

判定口径：同一类型被 3 个以上文件引用即上提到 `src/types/`；只被单一模块使用则保持就近，避免 `types/` 沦为倾倒场。

## 6.4 请求层设计

### 6.4.1 协议类型（types/api.ts）

```ts
export interface ApiResponse<T = unknown> {
  code: number
  message: string
  data: T
  traceId: string
  timestamp: number
}

export interface PageQuery {
  page?: number
  pageSize?: number
  keyword?: string
  sort?: string
  order?: 'asc' | 'desc'
}

export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  pageSize: number
}

/** 业务错误：拦截器在 code !== 0 时抛出，业务侧可按 code 分支处理 */
export class BizError extends Error {
  constructor(
    public code: number,
    message: string,
    public traceId = '',
    public data: unknown = null,
  ) {
    super(message)
    this.name = 'BizError'
  }
}

export interface RequestExtra {
  /** 跳过全局错误提示，由调用方自行处理 */
  silent?: boolean
  /** 覆盖 X-Node-Id；传 null 表示本次请求不带节点上下文（如登录、集群列表） */
  nodeId?: string | null
  /** 幂等重试次数，仅对 GET 生效，默认 0 */
  retry?: number
  /** 重试基础间隔（毫秒），实际间隔为 delay * 2^n */
  retryDelay?: number
}
```

`RequestExtra` 通过 Axios 配置的自定义字段传递，在 `types/axios.d.ts` 中用模块声明合并扩展 `AxiosRequestConfig`。

### 6.4.2 utils/request.ts

```ts
import axios, { AxiosError, type AxiosInstance, type AxiosRequestConfig } from 'axios'
import { Modal } from '@arco-design/web-vue'
import { toast } from '@/utils/feedback'
import { ApiResponse, BizError } from '@/types/api'
import { ENV } from '@/config/env'
import { ERROR_MESSAGE_MAP, NO_TOAST_CODES } from '@/config/errorCode'
import { getAccessToken, setAccessToken, clearAuth } from '@/utils/auth'
import { genRequestId } from '@/utils/uuid'

const http: AxiosInstance = axios.create({
  baseURL: ENV.apiBaseUrl,
  timeout: ENV.requestTimeout,
  withCredentials: true, // 携带 refreshToken 的 HttpOnly Cookie
  headers: { 'Content-Type': 'application/json;charset=utf-8' },
})

/* ---------- 请求拦截：注入 token、节点上下文、请求 ID ---------- */
http.interceptors.request.use((config) => {
  const token = getAccessToken()
  if (token) config.headers.Authorization = `Bearer ${token}`

  // nodeId 显式为 null 时不带该头；未指定时取当前节点上下文
  if (config.nodeId !== null) {
    const nodeId = config.nodeId ?? nodeIdProvider()
    if (nodeId) config.headers['X-Node-Id'] = nodeId
  }

  config.headers['X-Request-Id'] = genRequestId()
  const locale = localeProvider()
  if (locale) config.headers['Accept-Language'] = locale
  return config
})

/* utils 层不静态依赖 store：由 main.ts 在 Pinia 安装后注入 provider */
let nodeIdProvider: () => string = () => ''
let localeProvider: () => string = () => 'zh-CN'
export function registerContextProvider(node: () => string, locale: () => string) {
  nodeIdProvider = node
  localeProvider = locale
}
```

`main.ts` 中的注册顺序：`createPinia()` → `app.use(pinia)` → `registerContextProvider(() => useNodeStore().currentNodeId, () => useAppStore().locale)` → `app.use(router)`。

### 6.4.3 响应拦截与 401 静默刷新

单飞（single-flight）刷新：并发的 401 只发起一次 `POST /auth/refresh`，其余请求进入队列等待新 token 后重放；刷新失败则清理会话并跳转登录页，且只弹一次提示。

```ts
let refreshing = false
let waiters: Array<(token: string | null) => void> = []

function onRefreshed(token: string | null) {
  waiters.forEach((fn) => fn(token))
  waiters = []
}

async function refreshToken(): Promise<string | null> {
  try {
    // 裸 axios 调用，绕开拦截器，避免刷新接口自身 401 造成递归
    const { data } = await axios.post<ApiResponse<{ accessToken: string }>>(
      `${ENV.apiBaseUrl}/auth/refresh`,
      {},
      { withCredentials: true, timeout: 10000 },
    )
    if (data.code !== 0) return null
    setAccessToken(data.data.accessToken)
    return data.data.accessToken
  } catch {
    return null
  }
}

http.interceptors.response.use(
  (response) => {
    // 二进制流（下载）直接透传，由 download() 处理
    if (response.config.responseType === 'blob') return response
    const body = response.data as ApiResponse
    if (body.code === 0) return body.data as never
    const err = new BizError(body.code, body.message, body.traceId, body.data)
    handleBizError(err, response.config)
    return Promise.reject(err)
  },
  async (error: AxiosError<ApiResponse>) => {
    const config = error.config as AxiosRequestConfig & { _retried?: boolean }
    if (axios.isCancel(error)) return Promise.reject(error)

    // 401：静默刷新后重放一次
    if (error.response?.status === 401 && !config._retried) {
      config._retried = true
      if (refreshing) {
        return new Promise((resolve, reject) => {
          waiters.push((token) => (token ? resolve(http(config)) : reject(error)))
        })
      }
      refreshing = true
      const token = await refreshToken()
      refreshing = false
      onRefreshed(token)
      if (token) return http(config)
      clearAuth()
      redirectToLogin()
      return Promise.reject(new BizError(401, '登录已过期，请重新登录'))
    }

    // GET 幂等重试：网络错误、超时、5xx
    const retry = config.retry ?? 0
    const attempt = (config as { _attempt?: number })._attempt ?? 0
    const retryable = !error.response || error.response.status >= 500
    if (retry > attempt && retryable && (config.method ?? 'get').toLowerCase() === 'get') {
      ;(config as { _attempt?: number })._attempt = attempt + 1
      const delay = (config.retryDelay ?? 500) * 2 ** attempt
      await new Promise((r) => setTimeout(r, delay))
      return http(config)
    }

    const bizErr = toBizError(error)
    handleBizError(bizErr, config)
    return Promise.reject(bizErr)
  },
)
```

`redirectToLogin()` 使用 `router.replace({ name: 'Login', query: { redirect: currentFullPath } })`，并加 500ms 节流，防止多请求同时失败时重复跳转。

### 6.4.4 错误码到用户提示的映射

`config/errorCode.ts` 只维护「需要改写文案」的错误码，其余直接透出后端 `message`（后端已按 `Accept-Language` 返回本地化文案，见 [API 接口规范](./05-API接口规范.md)）。

```ts
export const ERROR_MESSAGE_MAP: Record<number, string> = {
  40001: '请求参数有误，请检查后重试',
  40101: '登录状态已失效，请重新登录',
  40301: '当前账号没有该操作的权限，请联系管理员',
  40302: '二次验证未通过，请重新输入动态验证码',
  40401: '目标资源不存在，可能已被其他会话删除',
  40901: '存在冲突：该名称或端口已被占用',
  42901: '操作过于频繁，请稍后再试',
  50001: '面板内部错误，请携带 traceId 联系管理员',
  50301: '目标节点离线，无法下发指令',
  50302: '节点执行超时，请到任务中心查看最终结果',
}

/** 这些错误码由业务页面自行渲染，不弹全局提示 */
export const NO_TOAST_CODES = new Set([40101, 40302])
```

```ts
function handleBizError(err: BizError, config?: AxiosRequestConfig) {
  if (config?.silent || NO_TOAST_CODES.has(err.code)) return
  const text = ERROR_MESSAGE_MAP[err.code] ?? err.message ?? '请求失败'
  // 节点离线、内部错误用 Modal 强提示；其余用 toast（utils/feedback.ts，见 6.8.1）
  if (err.code === 50001 || err.code === 50301) {
    Modal.error({ title: '操作失败', content: `${text}${err.traceId ? `（traceId: ${err.traceId}）` : ''}` })
  } else {
    toast.error(text)
  }
}

function toBizError(error: AxiosError<ApiResponse>): BizError {
  if (error.code === 'ECONNABORTED' || error.code === 'ETIMEDOUT') {
    return new BizError(-2, '请求超时，请检查网络或稍后重试')
  }
  if (!error.response) return new BizError(-1, '网络连接失败，请确认面板服务是否可达')
  const body = error.response.data
  if (body && typeof body.code === 'number') return new BizError(body.code, body.message, body.traceId)
  return new BizError(error.response.status, `服务异常（HTTP ${error.response.status}）`)
}
```

### 6.4.5 对外 API 与上传下载

```ts
export function request<T>(config: AxiosRequestConfig): Promise<T> {
  return http.request<T, T>(config)
}

export const get = <T>(url: string, params?: object, extra?: AxiosRequestConfig) =>
  request<T>({ url, method: 'get', params, ...extra })
export const post = <T>(url: string, data?: unknown, extra?: AxiosRequestConfig) =>
  request<T>({ url, method: 'post', data, ...extra })
export const put = <T>(url: string, data?: unknown, extra?: AxiosRequestConfig) =>
  request<T>({ url, method: 'put', data, ...extra })
export const del = <T>(url: string, data?: unknown, extra?: AxiosRequestConfig) =>
  request<T>({ url, method: 'delete', data, ...extra })

/** 下载：解析 Content-Disposition 取文件名，失败时把 blob 当 JSON 解析出业务错误 */
export async function download(url: string, params?: object, fallbackName = 'download'): Promise<void> {
  const res = await http.request<Blob>({ url, method: 'get', params, responseType: 'blob', timeout: 0 })
  const blob = res.data
  if (blob.type.includes('application/json')) {
    const body = JSON.parse(await blob.text()) as ApiResponse
    throw new BizError(body.code, body.message, body.traceId)
  }
  const disposition = res.headers['content-disposition'] as string | undefined
  const matched = disposition?.match(/filename\*?=(?:UTF-8'')?"?([^";]+)"?/i)
  const name = matched ? decodeURIComponent(matched[1]) : fallbackName
  const href = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = href
  a.download = name
  a.click()
  URL.revokeObjectURL(href)
}

/** 上传：Arco Upload 的 customRequest 直接对接，onProgress 回传百分比 */
export function upload<T>(
  url: string,
  file: File,
  fields: Record<string, string> = {},
  onProgress?: (percent: number) => void,
  signal?: AbortSignal,
): Promise<T> {
  const form = new FormData()
  form.append('file', file)
  Object.entries(fields).forEach(([k, v]) => form.append(k, v))
  return request<T>({
    url,
    method: 'post',
    data: form,
    timeout: 0,
    signal,
    headers: { 'Content-Type': 'multipart/form-data' },
    onUploadProgress: (e) => e.total && onProgress?.(Math.round((e.loaded / e.total) * 100)),
  })
}
```

### 6.4.6 请求取消

统一用原生 `AbortController`（Axios 1.x 的 `signal`），不使用已废弃的 `CancelToken`。`hooks/useAbort.ts` 在组件卸载或新一次查询发起时自动中止旧请求：

```ts
export function useAbort() {
  let controller: AbortController | null = null
  const next = () => {
    controller?.abort()
    controller = new AbortController()
    return controller.signal
  }
  onUnmounted(() => controller?.abort())
  return { next, abort: () => controller?.abort() }
}
```

搜索联想、日志分页、监控区间切换等高频重入场景必须使用；表单提交类请求不取消，避免后端已执行而前端误判失败。

### 6.4.7 api 层写法（api/website.ts）

```ts
import { get, post, put, del, download } from '@/utils/request'
import type { PageQuery, PageResult } from '@/types/api'
import type { Website, WebsiteStatus } from '@/types/website'

export interface WebsiteQuery extends PageQuery {
  status?: WebsiteStatus
  groupId?: number
  runtimeType?: 'static' | 'php' | 'nodejs' | 'python' | 'java' | 'proxy'
}

export interface CreateWebsitePayload {
  primaryDomain: string
  extraDomains: string[]
  runtimeType: WebsiteQuery['runtimeType']
  runtimeVersion?: string
  rootPath: string
  port: number
  enableSsl: boolean
  remark?: string
}

export const listWebsites = (params: WebsiteQuery) => get<PageResult<Website>>('/websites', params)
export const getWebsite = (id: number) => get<Website>(`/websites/${id}`)
export const createWebsite = (data: CreateWebsitePayload) => post<Website>('/websites', data)
export const updateWebsite = (id: number, data: Partial<CreateWebsitePayload>) => put<Website>(`/websites/${id}`, data)
export const deleteWebsite = (id: number, purgeData = false) => del<null>(`/websites/${id}`, { purgeData })
export const toggleWebsite = (id: number, action: 'start' | 'stop' | 'reload') =>
  post<null>(`/websites/${id}/${action}`)
export const getWebsiteConfig = (id: number) => get<{ content: string; path: string }>(`/websites/${id}/config`)
export const saveWebsiteConfig = (id: number, content: string) => put<null>(`/websites/${id}/config`, { content })
export const exportWebsites = (params: WebsiteQuery) => download('/websites/export', params, 'websites.csv')
```

约定：函数只负责「路径 + 参数 + 返回类型」，不含 `try/catch`、不弹提示、不做字段转换；返回值已由拦截器解构为 `data`，因此泛型直接写业务类型而非 `ApiResponse<T>`。

## 6.5 路由设计与菜单生成

### 6.5.1 meta 字段契约

```ts
// types/router.d.ts
declare module 'vue-router' {
  interface RouteMeta {
    title: string                 // i18n key，如 'menu.website.list'
    icon?: string                 // Arco 图标名或 SvgIcon name，仅一级/菜单项需要
    permissions?: string[]        // module:resource:action，空或缺省表示登录即可访问
    permissionMode?: 'any' | 'all' // 默认 any
    keepAlive?: boolean           // 是否缓存组件实例，默认 false
    hideInMenu?: boolean          // 不在侧边栏出现（详情页、编辑页）
    hideInTab?: boolean           // 不生成页签（登录页、全屏终端）
    activeMenu?: string           // 详情页高亮的父菜单 name
    order?: number                // 菜单排序，越小越靠前
    module: string                // 所属业务模块，用于埋点与页签分组
    nodeScoped?: boolean          // 页面数据是否随 X-Node-Id 变化，切节点时需刷新
    affix?: boolean               // 页签固定不可关闭（仅概览）
    fullscreen?: boolean          // 不套 Layout 内容区内边距（终端、大屏）
  }
}
```

### 6.5.2 路由表

一级路由 `/` 挂 `layout/index.vue`，其下为业务模块。`component` 列均相对 `@/views/`，统一 `() => import()` 懒加载。

| path | name | component | title | permissions | 备注 |
| --- | --- | --- | --- | --- | --- |
| `/login` | `Login` | `login/index.vue` | 登录 | — | 不套 Layout，`hideInTab` |
| `/dashboard` | `Dashboard` | `dashboard/overview/index.vue` | 概览 | `dashboard:overview:read` | `affix`、`keepAlive` |
| `/cluster/nodes` | `ClusterNodeList` | `cluster/node-list/index.vue` | 节点列表 | `cluster:node:list` | `keepAlive` |
| `/cluster/nodes/:id` | `ClusterNodeDetail` | `cluster/node-detail/index.vue` | 节点详情 | `cluster:node:read` | `hideInMenu`、`activeMenu: ClusterNodeList` |
| `/cluster/groups` | `ClusterGroup` | `cluster/group/index.vue` | 节点分组 | `cluster:group:list` | — |
| `/website/list` | `WebsiteList` | `website/site-list/index.vue` | 网站列表 | `website:site:list` | `keepAlive`、`nodeScoped` |
| `/website/create` | `WebsiteCreate` | `website/site-create/index.vue` | 创建网站 | `website:site:create` | 向导页，`hideInMenu` |
| `/website/list/:id` | `WebsiteDetail` | `website/site-detail/index.vue` | 网站详情 | `website:site:read` | `hideInMenu` |
| `/cert/list` | `CertList` | `cert/cert-list/index.vue` | 证书列表 | `cert:cert:list` | — |
| `/cert/acme` | `CertAcme` | `cert/acme-account/index.vue` | ACME 账户 | `cert:acme:list` | — |
| `/database/list` | `DatabaseList` | `database/db-list/index.vue` | 数据库 | `database:instance:list` | `nodeScoped` |
| `/database/console` | `DatabaseConsole` | `database/console/index.vue` | SQL 控制台 | `database:instance:exec` | — |
| `/runtime/list` | `RuntimeList` | `runtime/runtime-list/index.vue` | 运行环境 | `runtime:runtime:list` | `nodeScoped` |
| `/runtime/php` | `RuntimePhp` | `runtime/php/index.vue` | PHP 版本 | `runtime:php:list` | — |

| path | name | component | title | permissions | 备注 |
| --- | --- | --- | --- | --- | --- |
| `/file/manager` | `FileManager` | `file/manager/index.vue` | 文件管理 | `file:file:list` | `nodeScoped`，见 [文件管理与 WebSSH](./08-文件管理与WebSSH.md) |
| `/file/recycle` | `FileRecycle` | `file/recycle/index.vue` | 回收站 | `file:recycle:list` | — |
| `/terminal/ssh` | `TerminalSsh` | `terminal/ssh/index.vue` | 终端 | `terminal:session:exec` | `fullscreen`、`keepAlive` |
| `/terminal/record` | `TerminalRecord` | `terminal/record/index.vue` | 会话录像 | `terminal:record:list` | — |
| `/container/list` | `ContainerList` | `container/container-list/index.vue` | 容器 | `container:container:list` | `nodeScoped`、`keepAlive` |
| `/container/image` | `ContainerImage` | `container/image/index.vue` | 镜像 | `container:image:list` | — |
| `/container/compose` | `ContainerCompose` | `container/compose/index.vue` | 编排 | `container:compose:list` | — |
| `/container/network` | `ContainerNetwork` | `container/network/index.vue` | 网络与卷 | `container:network:list` | — |
| `/monitor/board` | `MonitorBoard` | `monitor/board/index.vue` | 监控大盘 | `monitor:metric:read` | `fullscreen` |
| `/monitor/process` | `MonitorProcess` | `monitor/process/index.vue` | 进程与连接 | `monitor:process:list` | `nodeScoped` |
| `/alert/event` | `AlertEvent` | `alert/event/index.vue` | 告警事件 | `alert:event:list` | `keepAlive` |
| `/alert/rule` | `AlertRule` | `alert/rule/index.vue` | 告警规则 | `alert:rule:list` | — |
| `/alert/channel` | `AlertChannel` | `alert/channel/index.vue` | 通知渠道 | `alert:channel:list` | — |
| `/security/firewall` | `SecurityFirewall` | `security/firewall/index.vue` | 防火墙 | `security:firewall:list` | `nodeScoped` |
| `/security/ssh` | `SecuritySsh` | `security/ssh/index.vue` | SSH 加固 | `security:ssh:read` | — |
| `/security/ban` | `SecurityBan` | `security/ban/index.vue` | 封禁与爆破防护 | `security:ban:list` | — |
| `/audit/log` | `AuditLog` | `audit/log/index.vue` | 审计日志 | `audit:log:list` | `keepAlive` |
| `/audit/login` | `AuditLogin` | `audit/login/index.vue` | 登录记录 | `audit:login:list` | — |
| `/appstore/market` | `AppstoreMarket` | `appstore/market/index.vue` | 应用商店 | `appstore:app:list` | — |
| `/appstore/installed` | `AppstoreInstalled` | `appstore/installed/index.vue` | 已安装 | `appstore:instance:list` | `nodeScoped` |
| `/cron/list` | `CronList` | `cron/task-list/index.vue` | 计划任务 | `cron:task:list` | — |
| `/cron/list/:id/log` | `CronLog` | `cron/task-log/index.vue` | 执行日志 | `cron:task:read` | `hideInMenu` |
| `/backup/task` | `BackupTask` | `backup/task/index.vue` | 备份任务 | `backup:task:list` | — |
| `/backup/storage` | `BackupStorage` | `backup/storage/index.vue` | 存储配置 | `backup:storage:list` | — |
| `/setting/panel` | `SettingPanel` | `setting/panel/index.vue` | 面板设置 | `setting:panel:read` | — |
| `/setting/user` | `SettingUser` | `setting/user/index.vue` | 用户与角色 | `setting:user:list` | — |
| `/setting/profile` | `SettingProfile` | `setting/profile/index.vue` | 个人中心 | — | `hideInMenu` |
| `/403` | `Forbidden` | `error/403/index.vue` | 无权限 | — | `hideInMenu`、`hideInTab` |
| `/404` | `NotFound` | `error/404/index.vue` | 页面不存在 | — | 通配 `/:pathMatch(.*)*` |

模块与后端保持一一对应；新增页面必须同时补齐 `permissions` 与 i18n `menu.*` 文案，缺失任一项 CI 的路由校验脚本会失败。

### 6.5.3 选型：静态路由 + 菜单过滤

| 维度 | 动态 `addRoute` | 静态路由 + 菜单过滤（选用） |
| --- | --- | --- |
| 首屏时序 | 必须等权限接口返回才能 `next()`，刷新时有白屏窗口 | 路由表编译期确定，刷新直达 |
| 类型安全 | 路由由后端数据拼装，`name` 无法类型收敛 | `name` 可枚举，`router.push({ name })` 有补全 |
| 越权风险 | 未授权路由不存在，直接 404 | 路由存在，需守卫拦截并跳 403 |
| 调试成本 | 路由树运行期可变，难以复现 | 路由树固定，问题可静态定位 |
| 后端耦合 | 菜单结构由后端定义，改版需后端发版 | 后端只给权限点，前端控制信息架构 |

选择静态路由 + 菜单过滤：本项目权限点粒度到 action（`module:resource:action`），按钮级控制无论如何都要在前端做，再叠一层动态路由属于重复机制；且面板刷新频繁（长时间停留、切节点），无白屏更重要。越权风险由守卫补齐——`permissions` 校验不通过统一 `next({ name: 'Forbidden' })`，后端接口层仍做二次鉴权，前端拦截只是体验优化，不作为安全边界。

菜单由 `store/modules/permission.ts` 从 `router.getRoutes()` 的一级子树递归过滤生成：剔除 `hideInMenu`，剔除权限不匹配项，父节点在所有子节点被剔除后自身也剔除，最后按 `meta.order` 排序。

### 6.5.4 路由守卫

```ts
// router/guard.ts
import type { Router, RouteLocationNormalized } from 'vue-router'
import { toast } from '@/utils/feedback'
import { useUserStore } from '@/store/modules/user'
import { usePermissionStore } from '@/store/modules/permission'
import { useTabStore } from '@/store/modules/tab'
import { useAppStore } from '@/store/modules/app'
import { startProgress, doneProgress } from '@/utils/progress'
import { i18n } from '@/locale'
import { ENV } from '@/config/env'

const WHITE_LIST = ['Login', 'NotFound']

export function setupGuard(router: Router) {
  router.beforeEach(async (to, from, next) => {
    startProgress()
    const user = useUserStore()
    const perm = usePermissionStore()

    if (WHITE_LIST.includes(to.name as string)) return next()

    if (!user.accessToken) {
      return next({ name: 'Login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } })
    }
    // 首次进入或刷新：补齐用户信息、权限点、节点列表
    if (!user.loaded) {
      try {
        await user.fetchProfile()
        await perm.buildMenus()
      } catch {
        user.reset()
        return next({ name: 'Login' })
      }
    }

    if (!perm.hasRouteAccess(to)) {
      toast.warning(i18n.global.t('common.noPermission'))
      return next({ name: 'Forbidden', query: { from: to.fullPath } })
    }

    useTabStore().open(to)
    next()
  })

  router.afterEach((to) => {
    doneProgress()
    document.title = `${i18n.global.t(to.meta.title)} - ${ENV.appTitle}`
    useAppStore().setBreadcrumb(buildBreadcrumb(router, to))
  })

  router.onError(() => doneProgress())
}

/** 面包屑：优先取 matched 链上的可见节点，详情页借 activeMenu 补齐父级 */
function buildBreadcrumb(router: Router, to: RouteLocationNormalized) {
  const chain = to.matched.filter((r) => r.meta?.title && r.name !== 'Layout')
  if (to.meta.activeMenu) {
    const parent = router.getRoutes().find((r) => r.name === to.meta.activeMenu)
    if (parent && !chain.some((r) => r.name === parent.name)) chain.splice(chain.length - 1, 0, parent)
  }
  return chain.map((r) => ({ title: r.meta!.title as string, name: r.name as string }))
}
```

进度条不引入 nprogress：`utils/progress.ts` 用一个固定在顶栏下方的 2px 元素，`startProgress` 以 `requestAnimationFrame` 递增到 90%，`doneProgress` 补到 100% 后淡出，并对 200ms 内完成的导航不显示，避免闪烁。


## 6.6 状态管理设计

### 6.6.1 store 划分

统一组合式写法 `defineStore('name', () => {...})`，禁用 Options 写法，保证与 `<script setup>` 风格一致且便于类型推导。

| store | id | 职责 | 关键 state | 持久化 |
| --- | --- | --- | --- | --- |
| `useUserStore` | `user` | 登录态、用户资料、accessToken | `accessToken`、`profile`、`loaded` | 仅 `accessToken`（见 6.6.4） |
| `usePermissionStore` | `permission` | 权限点集合、菜单树、路由可达性判定 | `codes`、`menus`、`isSuperAdmin` | 否 |
| `useAppStore` | `app` | 布局与主题：侧栏折叠、主题、语言、密度、面包屑 | `theme`、`collapsed`、`locale`、`density` | `theme`/`collapsed`/`locale`/`density` |
| `useNodeStore` | `node` | 节点列表与当前节点（`X-Node-Id` 真源） | `nodes`、`currentNodeId` | 仅 `currentNodeId` |
| `useTabStore` | `tab` | 多页签列表、缓存名单、右键菜单动作 | `tabs`、`cacheNames` | `tabs`（按 sessionStorage） |
| `useNotificationStore` | `notification` | 告警未读数、消息列表、WS 推送入口 | `unread`、`items` | 否 |
| `useTerminalStore` | `terminal` | 终端会话池：会话元数据与激活项 | `sessions`、`activeId` | 否（连接不可恢复） |

跨 store 依赖只允许单向：`permission` 依赖 `user`，`tab` 依赖 `router`，其余互不依赖。需要联动时用 `watch` 而非互相调用 action。

### 6.6.2 store/modules/user.ts

```ts
import { defineStore } from 'pinia'
import { login as loginApi, logout as logoutApi, getProfile, type LoginPayload } from '@/api/auth'
import type { UserProfile } from '@/types/user'
import { usePermissionStore } from './permission'
import { useTabStore } from './tab'
import { useTerminalStore } from './terminal'

export const useUserStore = defineStore(
  'user',
  () => {
    const accessToken = ref('')
    const profile = ref<UserProfile | null>(null)
    const loaded = ref(false)

    const nickname = computed(() => profile.value?.nickname ?? profile.value?.username ?? '')
    const isSuperAdmin = computed(() => profile.value?.roles.includes('super_admin') ?? false)

    async function login(payload: LoginPayload) {
      // refreshToken 由后端写入 HttpOnly Cookie，前端不接触
      const res = await loginApi(payload)
      accessToken.value = res.accessToken
      return res
    }

    async function fetchProfile() {
      profile.value = await getProfile()
      loaded.value = true
      return profile.value
    }

    async function logout(silent = false) {
      if (!silent) {
        try {
          await logoutApi()
        } catch {
          // 服务端会话可能已失效，忽略
        }
      }
      reset()
    }

    function reset() {
      accessToken.value = ''
      profile.value = null
      loaded.value = false
      usePermissionStore().reset()
      useTabStore().reset()
      useTerminalStore().closeAll()
    }

    return { accessToken, profile, loaded, nickname, isSuperAdmin, login, fetchProfile, logout, reset }
  },
  { persist: { key: 'nova:user', storage: localStorage, pick: ['accessToken'] } },
)
```

### 6.6.3 store/modules/node.ts

`currentNodeId` 是全局节点上下文的唯一真源。切换节点会影响所有 `meta.nodeScoped` 页面的数据，因此切换动作必须集中在此，副作用（关闭终端、清理 WS 订阅、刷新当前页）由 store 统一编排，禁止各页面自行处理。

```ts
import { defineStore } from 'pinia'
import { Modal } from '@arco-design/web-vue'
import { listNodes } from '@/api/cluster'
import type { NodeItem } from '@/types/cluster'
import { wsClient } from '@/utils/ws'
import { useTerminalStore } from './terminal'

export const useNodeStore = defineStore(
  'node',
  () => {
    const nodes = ref<NodeItem[]>([])
    const currentNodeId = ref('')
    const loading = ref(false)

    const current = computed(() => nodes.value.find((n) => n.id === currentNodeId.value) ?? null)
    const onlineNodes = computed(() => nodes.value.filter((n) => n.status === 'online'))
    const isLocal = computed(() => current.value?.isLocal ?? true)

    async function fetchNodes() {
      loading.value = true
      try {
        // 节点列表本身不属于任何节点，显式不带 X-Node-Id
        const res = await listNodes({ page: 1, pageSize: 500 }, { nodeId: null })
        nodes.value = res.list
        const exists = nodes.value.some((n) => n.id === currentNodeId.value)
        if (!exists) currentNodeId.value = (nodes.value.find((n) => n.isLocal) ?? nodes.value[0])?.id ?? ''
      } finally {
        loading.value = false
      }
    }

    /** 切换节点：有活跃终端时先确认，再清理订阅并广播事件 */
    async function switchNode(id: string) {
      if (id === currentNodeId.value) return
      const terminal = useTerminalStore()
      if (terminal.sessions.length > 0) {
        const ok = await new Promise<boolean>((resolve) => {
          Modal.warning({
            title: '切换节点将关闭当前终端会话',
            content: `存在 ${terminal.sessions.length} 个活跃会话，切换后需要重新连接。是否继续？`,
            hideCancel: false,
            onOk: () => resolve(true),
            onCancel: () => resolve(false),
          })
        })
        if (!ok) return
        terminal.closeAll()
      }
      currentNodeId.value = id
      wsClient.resubscribeAll(id) // 带 nodeId 的订阅全部重建
      window.dispatchEvent(new CustomEvent('nova:node-changed', { detail: { nodeId: id } }))
    }

    return { nodes, currentNodeId, loading, current, onlineNodes, isLocal, fetchNodes, switchNode }
  },
  { persist: { key: 'nova:node', storage: localStorage, pick: ['currentNodeId'] } },
)
```

`meta.nodeScoped` 的页面通过 `hooks/useNodeScope.ts` 订阅该事件并调用自身 `refresh()`，事件在 `onUnmounted` 中解绑；不用 `watch(currentNodeId)` 是为了让被 `keepAlive` 缓存但未激活的页面也能在重新激活时拿到正确数据（hook 内部记录 `dirty` 标记，`onActivated` 时补刷）。

### 6.6.4 持久化方案

使用 `pinia-plugin-persistedstate`，逐 store 显式 `pick`，禁止整体持久化。

| 键 | 存储 | 内容 | 理由 |
| --- | --- | --- | --- |
| `nova:user` | localStorage | `accessToken` | 刷新页面免重登；15 分钟过期，泄露窗口有限 |
| `nova:app` | localStorage | `theme`、`collapsed`、`locale`、`density` | 个人偏好，跨会话保留 |
| `nova:node` | localStorage | `currentNodeId` | 运维习惯上常驻同一节点；节点不存在时回落本机 |
| `nova:tab` | sessionStorage | `tabs` | 页签属于当前工作会话，新窗口应从概览开始 |
| `nova:table:<routeName>` | localStorage | 列显隐、列宽、页大小 | ProTable 列设置，见 6.9.1 |

明确禁止持久化：

- `refreshToken`（在 HttpOnly Cookie 内，前端 JS 不可读也不得复制到 storage）
- 用户资料 `profile` 与权限点 `codes`（后端可能已改权限，必须每次刷新重新拉取，避免越权 UI 停留）
- 终端会话、WS 订阅、告警未读列表（连接态与实时数据，恢复后无意义）
- 任何表单草稿中的密码、私钥、数据库连接串（含证书内容与 SSH 私钥）

写入 storage 统一走 `utils/storage.ts`，强制 `nova:` 前缀并带版本号；`utils/storage.ts` 在读取到版本不匹配的键时直接丢弃，避免升级后旧结构导致运行时报错。


## 6.7 布局系统

### 6.7.1 结构

```text
┌─ Header 56px ────────────────────────────────────────────────┐
│ Logo | 折叠 | NodeSelector | 面包屑 ····· 搜索 消息 主题 全屏 用户 │
├────────┬─────────────────────────────────────────────────────┤
│ Sider  │ TabBar 40px（可关闭页签 + 右键菜单 + 刷新/关闭其他）    │
│ 220px  ├─────────────────────────────────────────────────────┤
│ 折叠 48│ Content（router-view + KeepAlive + Transition）        │
│        │  padding 16px；meta.fullscreen 时为 0                 │
├────────┴─────────────────────────────────────────────────────┤
│ GlobalDrawer：任务中心 / 消息中心 / 全局搜索（Command-K）        │
└──────────────────────────────────────────────────────────────┘
```

`layout/` 文件构成：`index.vue`（壳）、`components/` 下 `NavBar.vue`、`SideMenu.vue`、`TabBar.vue`、`GlobalSearch.vue`、`MessageCenter.vue`、`TaskCenter.vue`、`UserDropdown.vue`。

### 6.7.2 layout/index.vue

```vue
<script setup lang="ts">
import { useAppStore } from '@/store/modules/app'
import { useTabStore } from '@/store/modules/tab'
import { useFullscreen } from '@/hooks/useFullscreen'

defineOptions({ name: 'Layout' })

const app = useAppStore()
const tab = useTabStore()
const route = useRoute()
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen()

const searchVisible = ref(false)
const siderWidth = computed(() => (app.collapsed ? 48 : 220))
const contentClass = computed(() => ({ 'nova-layout__content--flush': route.meta.fullscreen }))

// Command-K / Ctrl-K 唤起全局搜索
function onKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    searchVisible.value = true
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>
<template>
  <a-layout class="nova-layout">
    <a-layout-header class="nova-layout__header">
      <NavBar
        v-model:search-visible="searchVisible"
        :fullscreen="isFullscreen"
        @toggle-fullscreen="toggleFullscreen"
      />
    </a-layout-header>
    <a-layout>
      <a-layout-sider
        class="nova-layout__sider"
        :width="siderWidth"
        :collapsed="app.collapsed"
        :collapsible="false"
        breakpoint="lg"
        @collapse="app.setCollapsed"
      >
        <SideMenu :collapsed="app.collapsed" />
      </a-layout-sider>
      <a-layout-content class="nova-layout__main">
        <TabBar />
        <section class="nova-layout__content" :class="contentClass">
          <router-view v-slot="{ Component, route: r }">
            <transition name="nova-fade" mode="out-in">
              <keep-alive :include="tab.cacheNames" :max="12">
                <component :is="Component" :key="r.fullPath" />
              </keep-alive>
            </transition>
          </router-view>
        </section>
      </a-layout-content>
    </a-layout>
    <GlobalSearch v-model:visible="searchVisible" />
    <MessageCenter />
    <TaskCenter />
  </a-layout>
</template>

<style lang="scss" scoped>
.nova-layout {
  height: 100vh;
  &__header { height: 56px; border-bottom: 1px solid var(--nova-color-border); }
  &__main { display: flex; flex-direction: column; overflow: hidden; }
  &__content {
    flex: 1;
    padding: var(--nova-spacing-4);
    overflow: auto;
    background: var(--nova-color-bg-layout);
    &--flush { padding: 0; }
  }
}
</style>

```


### 6.7.3 交互细则

| 能力 | 实现要点 |
| --- | --- |
| 侧栏折叠 | `app.collapsed` 持久化；折叠态菜单用 `a-tooltip` 显示标题；宽度过渡 200ms；窗口 < 992px 自动折叠且不写入偏好 |
| 多页签 | `tab.tabs` 由守卫的 `open(to)` 维护，`key` 用 `fullPath`（同路由不同参数视为不同页签）；上限 15 个，超出淘汰最早的非固定页签 |
| 页签缓存 | `cacheNames` 只收 `meta.keepAlive === true` 且组件已 `defineOptions({ name })` 的路由；关闭页签时从 `cacheNames` 移除以真正销毁实例 |
| 右键菜单 | 刷新当前、关闭当前、关闭其他、关闭左侧、关闭右侧、关闭全部、在新窗口打开；固定页签禁用关闭项 |
| 刷新当前 | 不用 `location.reload()`，通过 `tab.reload(fullPath)` 临时移出 `cacheNames` 并替换 `router-view` 的 `key`，避免整页重载与权限重新拉取 |
| 全局搜索 | Command-K/Ctrl-K 唤起 `a-modal`（`simple` 模式）；数据源为菜单项 + 最近访问 + 网站/容器/节点名称的远端联想（300ms 防抖，`useAbort` 取消旧请求）；上下键选择、回车跳转、Esc 关闭 |
| 节点切换器 | 顶栏 `a-select` 带在线状态圆点与搜索；选中后调 `node.switchNode()`；离线节点可选但提示「离线节点仅可查看缓存数据」；单机模式节点数为 1 时隐藏该组件 |
| 消息中心 | 铃铛显示 `notification.unread`（`a-badge`，>99 显示 99+）；点击开 `a-drawer`，按「告警 / 任务 / 系统」三个 `a-tab-pane` 分组；WS 推新告警时铃铛抖动一次，按级别决定是否额外弹 `Notification` |
| 任务中心 | 长任务（安装运行环境、备份、批量下发）返回 `taskId`，登记到任务中心抽屉，WS 推进度；抽屉关闭后进度仍在后台推进，顶栏图标显示运行中数量 |
| 主题切换 | `app.setTheme('light' \| 'dark' \| 'auto')`，`auto` 跟随 `prefers-color-scheme`；切换时同步 Arco、`--nova-` 令牌与 ECharts 主题（见 6.8.3） |
| 全屏 | `useFullscreen` 封装 Fullscreen API，作用于 `#app`；监控大盘与终端页进入全屏时隐藏顶栏与侧栏 |

## 6.8 Arco Design 使用规范与设计令牌

### 6.8.1 按需引入与注册

组件与图标由 `unplugin-vue-components` 的 `ArcoResolver` 自动按需引入，模板中直接写 `<a-button>`、`<icon-plus>`，不写 `import`。仅以下三类必须手动 `import`：

```ts
// 1) 命令式 API：不直接在业务里调用 Arco 原生方法，统一走 utils/feedback.ts 包一层默认值
import { Message, Notification, Modal } from '@arco-design/web-vue'

const MSG_DEFAULT = { duration: 3000, closable: false, showIcon: true } as const

export const toast = {
  success: (content: string) => Message.success({ ...MSG_DEFAULT, content }),
  error: (content: string) => Message.error({ ...MSG_DEFAULT, content, duration: 4000 }),
  warning: (content: string) => Message.warning({ ...MSG_DEFAULT, content }),
  loading: (content: string) => Message.loading({ ...MSG_DEFAULT, content, duration: 0 }),
}

export const notify = {
  info: (title: string, content: string) =>
    Notification.info({ title, content, duration: 6000, position: 'topRight', closable: true }),
  done: (title: string, content: string, onView?: () => void) =>
    Notification.success({ title, content, duration: 8000, position: 'topRight', closable: true, footer: onView }),
}

export const confirm = (title: string, content: string) =>
  new Promise<boolean>((resolve) => {
    Modal.confirm({ title, content, maskClosable: false, onOk: () => resolve(true), onCancel: () => resolve(false) })
  })

// 2) 类型：用 import type，不产生运行时代码
import type { TableColumnData, FieldRule } from '@arco-design/web-vue'

// 3) 全局样式：只引令牌与我们的样式入口，组件样式由 resolver 按需注入
import '@arco-design/web-vue/dist/arco.css' // 提供完整 CSS 变量表，必须保留
import '@/styles/index.scss'
```

`app.use(ArcoVue)` 全量注册被禁止（约 500KB 未压缩样式进首屏）。图标统一用 `resolveIcons: true` 自动解析，禁止 `import { IconPlus } from '@arco-design/web-vue/es/icon'` 的手写形式。命令式反馈一律通过 `toast`/`notify`/`confirm` 调用，保证时长、位置、可关闭性全站一致，并便于在测试中整体 mock。

### 6.8.2 设计令牌 styles/theme.scss

Arco 2.x 的 CSS 变量以「RGB 三元组」形式定义（如 `--primary-6: 22,93,255`），消费时写 `rgb(var(--primary-6))`。因此覆盖 Arco 令牌必须沿用三元组格式；我们自己的 `--nova-` 令牌则用完整颜色值，供业务样式直接引用。

```scss
:root {
  /* ---- 主色：沿用 Arco 蓝，10 级色阶 ---- */
  --primary-1: 232, 243, 255;
  --primary-2: 190, 218, 255;
  --primary-3: 148, 191, 255;
  --primary-4: 106, 161, 255;
  --primary-5: 64, 128, 255;
  --primary-6: 22, 93, 255;   /* #165DFF 品牌主色 */
  --primary-7: 14, 66, 210;
  --primary-8: 7, 44, 166;
  --primary-9: 3, 26, 121;
  --primary-10: 0, 13, 77;

  /* ---- 语义色：沿用 Arco ---- */
  --nova-color-primary: #165dff;
  --nova-color-success: #00b42a;
  --nova-color-warning: #ff7d00;
  --nova-color-danger: #f53f3f;
  --nova-color-info: #86909c;
  --nova-color-processing: #165dff;

  /* ---- 中性色阶：映射 Arco neutral，业务只用 nova 别名 ---- */
  --nova-color-text-primary: rgb(var(--color-text-1));   /* 标题、关键数值 */
  --nova-color-text-regular: rgb(var(--color-text-2));   /* 正文 */
  --nova-color-text-secondary: rgb(var(--color-text-3)); /* 辅助说明、表头 */
  --nova-color-text-disabled: rgb(var(--color-text-4));
  --nova-color-bg-page: var(--color-bg-1);               /* 页面底色 */
  --nova-color-bg-layout: var(--color-fill-1);           /* 内容区底色 */
  --nova-color-bg-container: var(--color-bg-2);          /* 卡片、表格 */
  --nova-color-bg-hover: var(--color-fill-2);
  --nova-color-border: rgb(var(--color-border-2));
  --nova-color-border-strong: rgb(var(--color-border-3));
  --nova-color-mask: rgb(29 33 41 / 60%);

  /* ---- 圆角 ---- */
  --nova-radius-none: 0;
  --nova-radius-sm: 2px;
  --nova-radius-md: 4px;   /* 默认：按钮、输入框、卡片 */
  --nova-radius-lg: 8px;   /* 弹窗、抽屉、大卡片 */
  --nova-radius-full: 9999px;
  /* ---- 阴影：只允许这 4 级 ---- */
  --nova-shadow-1: 0 1px 2px rgb(0 0 0 / 6%);                        /* 卡片静置 */
  --nova-shadow-2: 0 4px 10px rgb(0 0 0 / 10%);                      /* 悬浮、下拉 */
  --nova-shadow-3: 0 8px 20px rgb(0 0 0 / 14%);                      /* 弹窗、抽屉 */
  --nova-shadow-focus: 0 0 0 2px rgb(var(--primary-6) / 20%);         /* 焦点环 */

  /* ---- 间距：4px 栅格，禁止出现非 4 倍数的间距 ---- */
  --nova-spacing-1: 4px;
  --nova-spacing-2: 8px;
  --nova-spacing-3: 12px;
  --nova-spacing-4: 16px;   /* 默认页面/卡片内边距 */
  --nova-spacing-5: 20px;
  --nova-spacing-6: 24px;   /* 区块之间 */
  --nova-spacing-8: 32px;
  --nova-spacing-12: 48px;

  /* ---- 字体与行高 ---- */
  --nova-font-family: 'Inter', -apple-system, 'PingFang SC', 'Microsoft YaHei', sans-serif;
  --nova-font-mono: 'JetBrains Mono', Menlo, Consolas, 'Courier New', monospace;
  --nova-font-size-xs: 12px;   /* 辅助说明、表格次要列 */
  --nova-font-size-sm: 13px;
  --nova-font-size-md: 14px;   /* 正文基准 */
  --nova-font-size-lg: 16px;   /* 卡片标题 */
  --nova-font-size-xl: 20px;   /* 页面标题 */
  --nova-font-size-num: 24px;  /* 指标卡数值 */
  --nova-line-height-tight: 1.4;
  --nova-line-height-base: 1.5715;  /* 与 Arco 一致 */
  --nova-line-height-loose: 1.8;    /* 长文本、日志 */

  /* ---- 动效 ---- */
  --nova-duration-fast: 120ms;
  --nova-duration-base: 200ms;
  --nova-easing: cubic-bezier(0.34, 0.69, 0.1, 1);
}
```

数值与等宽内容（IP、端口、路径、容器 ID、指标数值）必须加 `font-family: var(--nova-font-mono)` 并配 `font-variant-numeric: tabular-nums`，避免表格中数字跳动。

层级规划（`z-index`）：所有层级只能取下表中的令牌，禁止在业务样式里写字面量。

| 令牌 | 值 | 用途 |
| --- | --- | --- |
| `--nova-z-base` | 0 | 常规内容流 |
| `--nova-z-sticky` | 100 | 表格吸顶表头、页面内粘性工具栏 |
| `--nova-z-sider` | 200 | 侧边栏 |
| `--nova-z-header` | 300 | 顶栏、页签栏 |
| `--nova-z-progress` | 400 | 路由进度条 |
| `--nova-z-dropdown` | 1000 | Arco Select/Dropdown/Tooltip 弹层（Arco 默认） |
| `--nova-z-modal` | 1001 | Modal、Drawer（Arco 默认） |
| `--nova-z-message` | 1003 | Message、Notification（Arco 默认） |
| `--nova-z-terminal-overlay` | 1100 | 终端全屏时的搜索条与断连遮罩 |
| `--nova-z-loading-global` | 1200 | 全局阻塞 Loading（仅登录后初始化使用） |

### 6.8.3 亮色与暗色双主题

Arco 通过 `body[arco-theme='dark']` 切换内置暗色令牌，我们在同一选择器下覆盖 `--nova-` 令牌，两套变量同源切换。

```scss
body[arco-theme='dark'] {
  --nova-color-mask: rgb(0 0 0 / 70%);
  --nova-color-bg-page: var(--color-bg-1);
  --nova-color-bg-layout: #17171a;
  --nova-color-bg-container: var(--color-bg-2);
  --nova-shadow-1: 0 1px 2px rgb(0 0 0 / 30%);
  --nova-shadow-2: 0 4px 10px rgb(0 0 0 / 40%);
  --nova-shadow-3: 0 8px 20px rgb(0 0 0 / 50%);
  /* 暗色下语义色降饱和，避免刺眼 */
  --nova-color-success: #23c343;
  --nova-color-warning: #ff9a2e;
  --nova-color-danger: #f76965;
}
```

切换实现集中在 `hooks/useTheme.ts`，一次切换需要联动四处：

```ts
import { THEME_EVENT } from '@/config/constants'

export function applyTheme(theme: 'light' | 'dark') {
  // 1) Arco 内置令牌
  if (theme === 'dark') document.body.setAttribute('arco-theme', 'dark')
  else document.body.removeAttribute('arco-theme')
  // 2) 供第三方组件与 meta 使用
  document.documentElement.dataset.novaTheme = theme
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#17171a' : '#ffffff')
  // 3) monaco：切换已注册的主题
  monacoRef.value?.editor.setTheme(theme === 'dark' ? 'nova-dark' : 'nova-light')
  // 4) ECharts 实例无法热换主题，广播事件让 ChartWrapper 销毁重建
  window.dispatchEvent(new CustomEvent(THEME_EVENT, { detail: { theme } }))
}
```

`ChartWrapper` 监听 `THEME_EVENT`，执行 `dispose()` → `init(el, themeName)` → `setOption(option, true)`，并保留上一次 `option` 与已选时间区间，用户视觉上只感到配色变化。xterm 侧不重建实例，仅调用 `term.options.theme = novaXtermTheme[theme]` 即可即时生效。

### 6.8.4 组件使用约定

| 组件 | 统一用法 | 禁止 |
| --- | --- | --- |
| `Table` | 一律通过 `ProTable` 使用；`row-key` 必填且用后端主键；超 1000 行开 `virtual-list`；列宽用 `width` 固定关键列，其余自适应 | 直接用 `a-table`（除纯展示的小型内嵌表）、在列 `render` 里发请求、用索引当 `row-key` |
| `Form` | `layout="vertical"`（弹窗内可 `horizontal`），`rules` 从 `utils/validator.ts` 复用，提交按钮 `loading` 绑定请求态 | 手写 `if (!value) Message.error()` 式校验、把校验逻辑写在提交函数里 |
| `Modal` | 用于「需要用户决策」的短交互与危险确认；`width` 取 480/600/800 三档；`:mask-closable="false"` 用于表单类 | 在 Modal 里放多步向导或超过 8 个字段的表单（改用 Drawer 或独立页面） |
| `Drawer` | 用于详情查看、长表单、日志预览；`width` 取 480/640/900 三档；支持 Esc 关闭 | 在 Drawer 中再开 Drawer（最多两层，且第二层必须是 Modal） |
| `Message` | 轻量、非阻塞、可自动消失的操作结果反馈，文案 ≤ 20 字 | 用于必须让用户知晓的失败（改用 Notification/Modal）、同一操作连弹多条 |
| `Notification` | 异步任务完成、告警推送、需要保留可读时间的信息；带标题与操作按钮 | 用于表单校验失败、频繁的轮询结果 |
| `Tree` | 文件树、菜单权限树；节点多于 200 开 `virtual-list-props`；懒加载用 `load-more` | 一次性加载整个文件系统、在节点上绑定重业务逻辑 |
| `Upload` | 统一 `custom-request` 走 `upload()` 封装；显示进度与失败重试；大文件分片由 `FileUploader` 组件处理 | 使用默认 `action` 直传（绕过 token 与 X-Node-Id 注入） |
| `Descriptions` | 详情页只读信息展示，`:column="2"`（窄屏 1）；值为空显示 `-` | 用表格模拟键值对、在 Descriptions 里放可编辑控件 |
| `Tabs` | 页面内维度切换，`type="line"`；受控 `activeKey` 同步到 URL query 便于分享 | 用 Tabs 承载不同权限的内容（应按权限过滤 pane 而非隐藏内容） |
| `Card` | 内容分组容器，`:bordered="false"` + `--nova-shadow-1`；标题用 16px | 卡片套卡片、给 Card 加自定义外边距（用父级 grid gap） |
| `Statistic` | 指标卡数值，配 `precision` 与单位后缀；变化趋势用 `IconArrowRise/Fall` + 语义色 | 用于会频繁跳动的实时值（改用 `MetricCard` 的平滑过渡） |
| `Steps` | 向导页步骤指示，`type="arrow"`（横向）；步骤数 3-5 | 步骤超过 5 步（拆分流程）、允许跳过未完成的必填步骤 |
| `Result` | 操作结果终态页（创建成功、403、500） | 用于列表空态（用 Empty） |
| `Empty` | 列表/图表无数据；必须区分「无数据」与「无匹配结果」两种文案，后者附「清空筛选」按钮 | 只显示图标不给下一步动作 |
| `Spin` | 局部加载，容器高度固定避免抖动；表格加载用 `ProTable` 内建骨架屏 | 全屏遮罩式 Loading（仅初始化允许一次） |

### 6.8.5 密度与间距

运维面板信息密度高，默认采用比 Arco 原生更紧的一档，并允许用户在设置里切换。`app.density` 写入 `<html data-nova-density>`，样式按属性选择器生效。

| 密度 | 表格行高 | 控件尺寸 | 卡片内边距 | 适用 |
| --- | --- | --- | --- | --- |
| `compact` | 40px | `size="small"` | 12px | 大屏、表格密集页（默认） |
| `default` | 48px | `size="medium"` | 16px | 表单页、详情页 |
| `loose` | 56px | `size="large"` | 24px | 演示、投屏 |

其他间距约定：页面内容区四边 16px；卡片之间 16px；区块标题与内容 12px；表单项之间 20px；按钮组之间 8px；表格工具栏与表格 12px。所有取值必须来自 `--nova-spacing-*`。

### 6.8.6 图标方案

- 业务通用语义（增删改查、上下箭头、刷新、设置）用 Arco Icon，模板直接写 `<icon-refresh />`，统一 `:size="16"`，颜色继承 `currentColor`。
- 产品特有语义（Nginx、MySQL、Redis、Docker、PHP、节点状态、备份介质）用自建 SVG sprite：源文件放 `assets/icons/*.svg`，由 `vite-plugin-svg-icons` 编译为 `symbol`，通过 `SvgIcon` 组件消费。

```vue
<!-- components/SvgIcon/index.vue -->
<script setup lang="ts">
const props = withDefaults(defineProps<{ name: string; size?: number | string; color?: string }>(), { size: 16 })
const symbolId = computed(() => `#nova-icon-${props.name}`)
</script>
<template>
  <svg class="nova-svg-icon" :style="{ width: `${size}px`, height: `${size}px`, color }" aria-hidden="true">
    <use :xlink:href="symbolId" />
  </svg>
</template>
```

SVG 规范：画布 24×24，线宽 2，去除 `fill` 硬编码（改 `fill="currentColor"`），提交前用 `svgo` 压缩；文件名 kebab-case 且带语义前缀（`app-nginx`、`node-online`、`storage-s3`）。禁止引入图标字体与外链图标 CDN。


## 6.9 通用业务组件封装规范

所有组件位于 `components/<Name>/`，包含 `index.vue`、`types.ts`（可选）、`__tests__/`。组件不得引入 store 与 api，数据与动作由调用方注入。

### 6.9.1 ProTable

配置式表格，覆盖列表页 90% 场景。

```ts
// components/ProTable/types.ts
import type { TableColumnData } from '@arco-design/web-vue'
import type { PageResult, PageQuery } from '@/types/api'

export interface ProTableColumn extends TableColumnData {
  key: string                       // 唯一键，列设置持久化依赖它
  hideable?: boolean                // 是否允许用户隐藏，默认 true
  defaultHidden?: boolean
  copyable?: boolean                // 值旁显示复制按钮
  mono?: boolean                    // 等宽字体（IP/路径/ID）
  permission?: string               // 缺少该权限点时整列不渲染（字段级权限）
}

export interface ProTableProps<T = Record<string, unknown>> {
  columns: ProTableColumn[]
  request: (
    query: PageQuery & Record<string, unknown>,
    ctx: { signal: AbortSignal },
  ) => Promise<PageResult<T>>
  rowKey?: string                   // 默认 'id'
  searchFields?: SearchFieldConfig[] // 传入即渲染 SearchForm
  rowSelection?: 'checkbox' | 'radio' | false
  defaultPageSize?: number          // 默认 20
  syncUrl?: boolean                 // 分页/筛选写入 URL query，默认 true
  cacheKey?: string                 // 列设置持久化键，默认取 route.name
  autoLoad?: boolean                // 默认 true
  polling?: number                  // 毫秒，>0 时开启轮询，页面隐藏自动暂停
}
```

| 项 | 名称 | 说明 |
| --- | --- | --- |
| props | 见上 `ProTableProps` | `request` 是唯一数据源，组件内部管理 loading/分页/排序 |
| emits | `select-change(keys, rows)` | 选中项变化 |
| emits | `loaded(result)` | 一次请求成功后抛出，供父级统计用 |
| emits | `error(err)` | 请求失败，父级可决定是否降级展示 |
| slots | `toolbar-left` | 主操作区（新建、批量导入），默认空 |
| slots | `toolbar-right` | 附加操作（导出、刷新前置项），列设置按钮固定在最右 |
| slots | `batch-actions` | 有选中项时替换 `toolbar-left` 的批量操作条，接收 `{ keys, rows, clear }` |
| slots | `col-<key>` | 覆写某列渲染，接收 `{ record, rowIndex }` |
| slots | `actions` | 行操作列，接收 `{ record }`；组件自动加 `fixed="right"` |
| slots | `empty` | 覆盖空态，默认按有无筛选条件区分文案 |
| expose | `reload(resetPage?)`、`getQuery()`、`clearSelection()` | 父级通过 `ref` 调用 |

关键实现（分页与筛选双向绑定 URL、列设置持久化、骨架屏）：

```ts
const route = useRoute()
const router = useRouter()
const { hasPermission } = usePermission()
const storageKey = computed(() => `nova:table:${props.cacheKey ?? String(route.name)}`)

const query = reactive<PageQuery & Record<string, unknown>>({
  page: Number(route.query.page ?? 1),
  pageSize: Number(route.query.pageSize ?? props.defaultPageSize ?? 20),
  keyword: (route.query.keyword as string) ?? '',
  sort: route.query.sort as string,
  order: route.query.order as 'asc' | 'desc',
})

const list = ref<unknown[]>([])
const total = ref(0)
const loading = ref(false)
const firstLoaded = ref(false)
const { next: nextSignal } = useAbort()

// 列设置：持久化显隐与顺序，并按字段级权限过滤
const hiddenKeys = ref<string[]>(storage.get(storageKey.value)?.hidden ?? [])
const visibleColumns = computed(() =>
  props.columns
    .filter((c) => !c.permission || hasPermission(c.permission))
    .filter((c) => !hiddenKeys.value.includes(c.key)),
)
watch(hiddenKeys, (v) => storage.set(storageKey.value, { hidden: v }), { deep: true })

async function fetchData() {
  loading.value = true
  try {
    const res = await props.request({ ...query }, { signal: nextSignal() })
    list.value = res.list
    total.value = res.total
    emit('loaded', res)
  } catch (e) {
    emit('error', e)
    list.value = []
    total.value = 0
  } finally {
    loading.value = false
    firstLoaded.value = true
  }
}

// 筛选或分页变化 → 同步 URL（replace，不污染历史）→ 重新请求
watch(query, () => {
  if (props.syncUrl !== false) router.replace({ query: pruneEmpty({ ...route.query, ...query }) })
  fetchData()
})
```

首屏用 `a-skeleton` 渲染 5 行占位（`firstLoaded === false`），后续刷新只在表格上盖 `loading` 遮罩，避免布局跳动。空态区分 `hasFilter`：有筛选时文案为「没有符合条件的记录」并给「清空筛选条件」按钮，无筛选时为「暂无数据」并给主操作按钮插槽。

### 6.9.2 SearchForm

| 项 | 定义 |
| --- | --- |
| props | `fields: SearchFieldConfig[]`（`{ key, label, type: 'input'\|'select'\|'date-range'\|'cascader'\|'number-range', options?, span?, placeholder? }`）、`modelValue: Record<string, unknown>`、`collapsible?: boolean`（默认 true，超过 3 项自动折叠）、`labelWidth?: number` |
| emits | `update:modelValue`、`search(values)`、`reset()` |
| slots | `extra`（自定义筛选项）、`actions`（覆盖搜索/重置按钮区） |
| 要点 | 用 `a-grid` 响应式排布（xs 1 列、md 2 列、xl 4 列）；回车触发 search；`reset` 清空并回到第 1 页；输入型字段 300ms 防抖只用于「即时筛选」模式，默认仍需点搜索 |

### 6.9.3 ModalForm

| 项 | 定义 |
| --- | --- |
| props | `visible: boolean`、`title: string`、`width?: number\|string`（默认 600）、`submit: (values) => Promise<void>`、`initialValues?: object`、`mode?: 'create' \| 'edit'`、`confirmText?: string` |
| emits | `update:visible`、`success(values)`、`cancel()` |
| slots | 默认插槽放 `a-form-item`，接收 `{ form }`；`footer-extra` |
| 要点 | 内部持有 `a-form` 引用，`onOk` 先 `validate()` 再 `submit()`，`submit` 抛错时保持弹窗打开且不清空数据；关闭动画结束后再 `resetFields()`，避免闪现空白；`:mask-closable="false"`；`Esc` 需二次确认（表单已改动时） |

### 6.9.4 StatusTag

| 项 | 定义 |
| --- | --- |
| props | `status: NovaStatus`、`text?: string`（默认取字典文案）、`size?: 'small' \| 'medium'`、`dot?: boolean`（点状而非标签）、`pulse?: boolean`（进行中时呼吸动画） |
| slots | `tooltip`（悬浮补充说明，如异常原因） |

统一状态色映射（`config/dict.ts`，全项目唯一来源，禁止在页面里另建映射）：

| status | 文案 | 颜色令牌 | Arco color | 图标 | 典型场景 |
| --- | --- | --- | --- | --- | --- |
| `running` | 运行中 | `--nova-color-success` | `green` | `icon-play-arrow-fill` | 网站/容器/服务正常 |
| `stopped` | 已停止 | `--nova-color-info` | `gray` | `icon-pause` | 手动停止、未启用 |
| `error` | 异常 | `--nova-color-danger` | `red` | `icon-close-circle-fill` | 启动失败、进程退出 |
| `warning` | 告警 | `--nova-color-warning` | `orange` | `icon-exclamation-circle-fill` | 阈值触发、证书临期 |
| `pending` | 处理中 | `--nova-color-processing` | `blue` | `icon-loading`（旋转） | 安装、备份、下发中 |
| `unknown` | 未知 | `--nova-color-text-disabled` | `gray` | `icon-question-circle` | 节点离线导致状态不可得 |

颜色不是唯一区分手段：每个状态必须同时具备图标与文案，满足色盲友好（见 6.16）。

### 6.9.5 MetricCard

| 项 | 定义 |
| --- | --- |
| props | `label: string`、`value: number \| string`、`unit?: string`、`precision?: number`（默认 2）、`trend?: number`（同比变化百分比）、`threshold?: { warning: number; danger: number }`、`sparkline?: number[]`、`loading?: boolean`、`icon?: string` |
| emits | `click()`（可下钻到监控详情） |
| slots | `extra`（右上角操作）、`footer`（补充说明，如「较昨日」） |
| 要点 | 数值用 `a-statistic` + `:animation="true"`；超过 `threshold` 时数值与卡片左侧色条切换语义色；`sparkline` 用 `ChartWrapper` 渲染无坐标轴的迷你折线，高度固定 32px；`value` 为 `null` 时显示 `-` 而非 0 |

### 6.9.6 ChartWrapper

| 项 | 定义 |
| --- | --- |
| props | `option: EChartsOption`、`height?: number \| string`（默认 320）、`loading?: boolean`、`empty?: boolean`、`autoResize?: boolean`（默认 true）、`notMerge?: boolean`、`group?: string`（同组图表联动 tooltip 与 dataZoom） |
| emits | `chart-ready(instance)`、`datazoom(range)`、`click(params)` |
| slots | `empty`、`toolbar`（右上角时间区间切换） |

```ts
// 关键实现：按需注册 + ResizeObserver + 主题联动 + 销毁
import * as echarts from 'echarts/core'
import { LineChart, BarChart, PieChart, GaugeChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent, DataZoomComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
echarts.use([LineChart, BarChart, PieChart, GaugeChart, GridComponent, TooltipComponent, LegendComponent, DataZoomComponent, CanvasRenderer])

const el = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null
let ro: ResizeObserver | null = null

function mount() {
  if (!el.value) return
  chart = echarts.init(el.value, currentThemeName())
  chart.setOption(props.option)
  if (props.group) chart.group = props.group
  emit('chart-ready', chart)
}

function remount() {   // 主题切换：ECharts 不支持热换主题，必须销毁重建
  const opt = chart?.getOption()
  chart?.dispose()
  chart = null
  mount()
  if (opt) chart?.setOption(opt as EChartsOption, true)
}

onMounted(() => {
  mount()
  ro = new ResizeObserver(debounce(() => chart?.resize(), 100))
  ro.observe(el.value!)
  window.addEventListener(THEME_EVENT, remount)
})
onUnmounted(() => {
  ro?.disconnect()
  window.removeEventListener(THEME_EVENT, remount)
  chart?.dispose()
  chart = null
})
watch(() => props.option, (o) => chart?.setOption(o, props.notMerge ?? false), { deep: true })
```

按需注册（`echarts/core` + `echarts/charts`）而非 `import * as echarts from 'echarts'`，可把 echarts chunk 从约 1MB 压到约 350KB。被 `keepAlive` 的页面在 `onActivated` 中补一次 `chart?.resize()`，否则从隐藏态恢复时尺寸为 0。

### 6.9.7 FileTree

| 项 | 定义 |
| --- | --- |
| props | `path: string`（当前目录，受控）、`loadData: (path) => Promise<FileNode[]>`、`showHidden?: boolean`、`selectable?: 'none' \| 'single' \| 'multiple'`、`filter?: (node) => boolean`、`height?: number` |
| emits | `update:path`、`select(nodes)`、`dblclick(node)`、`context-menu(node, event)` |
| slots | `node-extra`（节点右侧信息，如大小/权限）、`context-menu` |
| 要点 | 基于 `a-tree` 的 `load-more` 懒加载，节点 `key` 用绝对路径；超过 200 个同级节点启用 `virtual-list-props`；目录与文件排序为「目录优先 + 名称自然序」；右键菜单项按 `file:file:*` 权限过滤；拖拽移动需二次确认并展示源/目标路径全文 |

### 6.9.8 CodeEditor

| 项 | 定义 |
| --- | --- |
| props | `modelValue: string`、`language: 'nginx' \| 'yaml' \| 'json' \| 'php' \| 'shell' \| 'ini' \| 'plaintext'`、`readonly?: boolean`、`height?: number \| string`（默认 480）、`diff?: { original: string }`、`minimap?: boolean`（默认 false）、`wordWrap?: boolean` |
| emits | `update:modelValue`、`save(value)`（Cmd/Ctrl+S）、`validate(markers)` |
| slots | `toolbar`（格式化、语法检查、恢复默认按钮） |

```ts
// worker 注册：Vite 的 ?worker 后缀，避免 monaco 走 CDN
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import JsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker'
self.MonacoEnvironment = {
  getWorker(_: string, label: string) {
    return label === 'json' ? new JsonWorker() : new EditorWorker()
  },
}
// yaml / php / shell / ini 使用 basic-languages 的 Monarch 词法（无 worker，够用）
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution'
import 'monaco-editor/esm/vs/basic-languages/php/php.contribution'
import 'monaco-editor/esm/vs/basic-languages/shell/shell.contribution'
import 'monaco-editor/esm/vs/basic-languages/ini/ini.contribution'

// nginx 无内置支持，注册自定义 Monarch
monaco.languages.register({ id: 'nginx' })
monaco.languages.setMonarchTokensProvider('nginx', {
  tokenizer: {
    root: [
      [/#.*$/, 'comment'],
      [/\$[A-Za-z_]\w*/, 'variable'],
      [/\b(server|location|upstream|http|events|if|map)\b/, 'keyword.control'],
      [/\b(listen|server_name|root|index|proxy_pass|ssl_certificate|include|return|rewrite)\b/, 'keyword'],
      [/"([^"\\]|\\.)*"/, 'string'],
      [/\d+[kKmMgG]?\b/, 'number'],
      [/[{}]/, 'delimiter.bracket'],
    ],
  },
})
```

保存快捷键用 `editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => emit('save', getValue()))`；`diff` 模式改用 `monaco.editor.createDiffEditor` 并强制 `readOnly` 右侧只读；只读态下 `domReadOnly: true` 以屏蔽输入法。组件必须在 `onUnmounted` 调用 `editor.dispose()` 与 `model.dispose()`，否则切页后 model 泄漏。

### 6.9.9 Terminal

| 项 | 定义 |
| --- | --- |
| props | `sessionId: string`、`ticket: string`（一次性票据）、`nodeId: string`、`fontSize?: number`（默认 13）、`readonly?: boolean`（录像回放）、`record?: TerminalRecord`（回放数据） |
| emits | `ready()`、`close(reason)`、`title-change(title)`、`resize(cols, rows)` |
| slots | `toolbar`（新建标签、字号、搜索、下载录像） |

```ts
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import { SearchAddon } from '@xterm/addon-search'
import '@xterm/xterm/css/xterm.css'

const term = new XTerm({
  fontSize: props.fontSize,
  fontFamily: 'var(--nova-font-mono)',
  cursorBlink: true,
  scrollback: 5000,
  allowProposedApi: true,
  theme: xtermTheme(currentTheme()),
})
const fit = new FitAddon()
const search = new SearchAddon()
term.loadAddon(fit)
term.loadAddon(search)
try {
  term.loadAddon(new WebglAddon())   // 部分虚拟机无 WebGL，降级为 canvas
} catch { /* 忽略，使用默认渲染器 */ }
term.open(el.value!)
fit.fit()

// WebSocket：二进制帧承载 PTY 数据，文本帧承载控制消息
const ws = new WebSocket(`${wsBase()}/terminal?ticket=${props.ticket}`)
ws.binaryType = 'arraybuffer'
const decoder = new TextDecoder()
const encoder = new TextEncoder()
ws.onmessage = (ev) => {
  if (ev.data instanceof ArrayBuffer) term.write(new Uint8Array(ev.data))
  else handleControl(JSON.parse(ev.data as string))   // 如 { type: 'exit', code: 0 }
}
term.onData((data) => ws.readyState === WebSocket.OPEN && ws.send(encoder.encode(data)))
term.onResize(({ cols, rows }) => ws.send(JSON.stringify({ type: 'resize', cols, rows })))
```

其余要点：

- `resize` 上报用 `ResizeObserver` + 150ms 防抖调用 `fit.fit()`，`onResize` 再上报后端，避免每帧发包。
- 复制粘贴：选中即复制（`term.onSelectionChange` 写 `navigator.clipboard`，非 HTTPS 环境降级为 `document.execCommand`）；`Ctrl+Shift+V` 与右键粘贴走 `navigator.clipboard.readText()`；粘贴内容含换行时弹确认，防止误执行多行命令。
- 搜索：`Ctrl+Shift+F` 唤起搜索条，调用 `search.findNext(keyword, { incremental: true })`。
- 录像回放：`readonly` 模式不建连接，按 `record.frames` 的相对时间戳用 `setTimeout` 队列 `term.write`，支持 0.5x/1x/2x/4x 与拖动进度；回放期间禁用 `onData`。
- 断连：显示居中遮罩「连接已断开」+ 重连按钮，不自动重连（终端会话有状态，静默重连会让用户误以为上下文延续）。
- `onUnmounted` 顺序：`ws.close(1000)` → `term.dispose()`；`keepAlive` 页面在 `onDeactivated` 不断连，仅暂停渲染。

### 6.9.10 LogViewer

| 项 | 定义 |
| --- | --- |
| props | `source: 'ws' \| 'http'`、`fetchPage?: (cursor) => Promise<LogPage>`、`topic?: string`（`source='ws'` 时的订阅主题）、`follow?: boolean`（tail -f，默认 true）、`keyword?: string`、`levels?: LogLevel[]`、`maxLines?: number`（默认 20000）、`wrap?: boolean` |
| emits | `update:follow`、`update:keyword`、`stat(counts)`（各级别行数） |
| slots | `toolbar`（级别筛选、下载、清屏、时间范围） |
| 要点 | 自实现虚拟滚动：定高 20px 行，只渲染可视区 ± 20 行，`translateY` 定位；`follow` 为真时新行到达自动滚底，用户向上滚动超过 40px 立即置 `follow=false` 并显示「回到底部」浮标；超过 `maxLines` 从头部丢弃并提示「已省略较早日志」；关键字高亮用 `<mark>` 包裹（先 HTML 转义再替换，防注入），级别用左侧 3px 色条而非整行背景色；下载调 `download()` 直取后端原始文件，不用前端缓冲内容 |

### 6.9.11 NodeSelector

| 项 | 定义 |
| --- | --- |
| props | `modelValue: string \| string[]`、`multiple?: boolean`、`onlyOnline?: boolean`、`showGroup?: boolean`（按分组 optgroup）、`size?: 'small' \| 'medium'`、`disabledIds?: string[]` |
| emits | `update:modelValue`、`change(node \| nodes)` |
| slots | `option-extra`（显示负载、版本） |
| 要点 | 数据来自 `useNodeStore().nodes`（组件例外允许读 node store，因其为全局上下文而非业务数据）；顶栏实例为单选且变更即调 `switchNode`；批量下发场景用 `multiple` + 全选/反选 + 「仅在线」开关；离线节点显示灰点并在 tooltip 说明「最后心跳时间」 |

### 6.9.12 PermissionWrap

| 项 | 定义 |
| --- | --- |
| props | `permissions: string \| string[]`、`mode?: 'any' \| 'all'`（默认 any）、`fallback?: 'hide' \| 'disable'`（默认 hide）、`tip?: string` |
| slots | 默认插槽为受控内容；`denied` 自定义无权限展示 |
| 要点 | `fallback='disable'` 时给子元素套 `a-tooltip` 并注入 `disabled`，用于「用户知道功能存在但无权使用」的场景；`hide` 用于不该暴露的能力（如超管专属的用户管理入口）。判断口径见 6.10.3 |

其余通用组件（不逐一展开，规范同上）：`PageHeader`（标题 + 面包屑 + 主操作）、`DangerConfirm`（见 6.12.3）、`DynamicForm`（见 6.12.2）、`FileUploader`（分片上传）、`CopyText`、`SvgIcon`、`TimeAgo`（相对时间，1 分钟内显示「刚刚」，悬浮显示绝对时间）、`SizeText`（字节自动换算单位）。


## 6.10 权限控制

### 6.10.1 权限点模型

权限标识为 `module:resource:action`，`action` 取 `list`/`read`/`create`/`update`/`delete`/`exec`/`export`。后端在 `GET /auth/profile` 返回扁平字符串数组，前端不做任何推导（不假设 `update` 蕴含 `read`）。超级管理员返回 `["*"]`，前端在判定函数首位短路。

```ts
// store/modules/permission.ts 片段
const codes = ref<string[]>([])
const codeSet = computed(() => new Set(codes.value))
const isSuperAdmin = computed(() => codeSet.value.has('*'))

function has(perm: string | string[], mode: 'any' | 'all' = 'any') {
  if (isSuperAdmin.value) return true
  const list = Array.isArray(perm) ? perm : [perm]
  if (list.length === 0) return true
  return mode === 'all' ? list.every((p) => codeSet.value.has(p)) : list.some((p) => codeSet.value.has(p))
}

function hasRouteAccess(to: RouteLocationNormalized) {
  return to.matched.every((r) => has(r.meta?.permissions ?? [], r.meta?.permissionMode ?? 'any'))
}
```

### 6.10.2 usePermission 与 v-permission

```ts
// hooks/usePermission.ts
export function usePermission() {
  const perm = usePermissionStore()
  return {
    hasPermission: (p: string | string[], mode?: 'any' | 'all') => perm.has(p, mode),
    /** 权限不足时返回 true，便于直接绑 :disabled */
    denied: (p: string | string[], mode?: 'any' | 'all') => !perm.has(p, mode),
  }
}
```

```ts
// directives/permission.ts
import type { Directive, DirectiveBinding } from 'vue'
import { usePermissionStore } from '@/store/modules/permission'

type Value = string | string[] | { value: string | string[]; mode?: 'any' | 'all' }

function check(binding: DirectiveBinding<Value>) {
  const raw = binding.value
  const perms = typeof raw === 'object' && !Array.isArray(raw) ? raw.value : raw
  const mode = (typeof raw === 'object' && !Array.isArray(raw) ? raw.mode : undefined)
    ?? (binding.modifiers.all ? 'all' : 'any')
  return usePermissionStore().has(perms, mode)
}

export const vPermission: Directive<HTMLElement, Value> = {
  mounted(el, binding) {
    if (check(binding)) return
    if (binding.modifiers.disable) {
      el.setAttribute('disabled', 'disabled')
      el.classList.add('nova-permission-disabled')
      el.setAttribute('aria-disabled', 'true')
      el.title = '当前账号无此操作权限'
      // 捕获阶段吞掉点击，防止子组件自身的 handler 仍然触发
      el.addEventListener('click', (e) => { e.stopPropagation(); e.preventDefault() }, true)
      return
    }
    el.remove()   // 默认移除节点，而非 display:none
  },
}
```

用法：`v-permission="'website:site:create'"`、`v-permission.all="['a:b:c','d:e:f']"`、`v-permission.disable="'website:site:delete'"`。指令只在 `mounted` 判定一次；权限变更（改角色）会触发重新登录或 `perm.buildMenus()` 后的路由刷新，不需要响应式重算。

### 6.10.3 隐藏还是禁用

| 场景 | 处理 | 理由 |
| --- | --- | --- |
| 菜单项、页面入口 | 隐藏 | 无权限的导航项只会制造挫败感 |
| 列表行的破坏性操作（删除、重置密码） | 隐藏 | 避免误点与试探 |
| 列表行的常规操作（启停、重载、查看日志） | 禁用 + Tooltip 说明缺少的权限 | 保持行操作列宽度一致，避免不同行按钮数量不同导致错位 |
| 表单中的个别字段 | 禁用 + 显示当前值 | 用户需要知道配置现状，只是不能改 |
| 敏感字段值（密钥、密码、连接串） | 脱敏展示（`****`）+ 「申请查看」入口 | 字段级权限，由列 `permission` 与 `PermissionWrap` 共同实现 |
| 批量操作工具栏 | 隐藏整个批量条 | 无权限时选中行也无意义 |
| 只读账号浏览全站 | 全站按钮禁用（`fallback='disable'`） | 审计/只读角色需要完整了解系统面貌 |

字段级控制的两种落点：表格列用 `ProTableColumn.permission`（整列不渲染），详情页字段用 `<PermissionWrap permissions="..." fallback="disable">` 包裹 `a-descriptions-item`。两者都不依赖后端是否返回该字段——后端也会对无权限字段做脱敏，前端控制只是减少无效展示。


## 6.11 实时数据方案

### 6.11.1 消息格式与主题

除终端（独立连接、二进制帧）外，其余实时数据共用一条多路复用连接 `/api/v1/ws/hub`。信封格式：

```json
{
  "v": 1,
  "type": "sub",
  "id": "c-17",
  "topic": "monitor.metrics",
  "nodeId": "node-01",
  "payload": { "interval": 5, "metrics": ["cpu", "mem", "load"] },
  "ts": 1755412800000
}
```

| 字段 | 说明 |
| --- | --- |
| `v` | 协议版本，当前固定 1；服务端不识别时回 `error` 且不断开 |
| `type` | `sub`/`unsub`/`ping`/`pong`/`event`/`ack`/`error` |
| `id` | 客户端生成的请求 ID，`ack`/`error` 原样回带；`event` 无 `id` |
| `topic` | `<module>.<subject>`，如 `monitor.metrics`、`alert.event`、`task.progress`、`container.stats`、`file.transfer`、`node.status` |
| `nodeId` | 主题需要节点上下文时必填，等价于 HTTP 的 `X-Node-Id`；跨节点主题（`node.status`）省略 |
| `payload` | `sub` 时为订阅参数，`event` 时为数据体，`error` 时为 `{ code, message }` |
| `ts` | 服务端毫秒时间戳，客户端用于计算延迟与丢帧判断 |

握手用一次性 ticket：`POST /api/v1/ws/ticket` 返回 `{ ticket, expiresIn }`（默认 30 秒、一次性），连接地址为 `wss://<host>/api/v1/ws/hub?ticket=<ticket>`。不在 URL 上带 `accessToken`，避免出现在网关与浏览器历史的日志里。

### 6.11.2 utils/ws.ts

```ts
import { getWsTicket } from '@/api/auth'
import { ENV } from '@/config/env'

export interface WsEnvelope<T = unknown> {
  v: number
  type: 'sub' | 'unsub' | 'ping' | 'pong' | 'event' | 'ack' | 'error'
  id?: string
  topic?: string
  nodeId?: string
  payload?: T
  ts?: number
}

type Handler<T = unknown> = (payload: T, env: WsEnvelope<T>) => void
interface Subscription { key: string; topic: string; nodeId?: string; params?: object; handler: Handler }

class WsClient {
  private ws: WebSocket | null = null
  private subs = new Map<string, Subscription>()
  private queue: WsEnvelope[] = []
  private attempt = 0
  private heartbeatTimer?: number
  private pongDeadline?: number
  private seq = 0
  private connecting = false

  async connect() {
    if (this.connecting || this.ws?.readyState === WebSocket.OPEN) return
    this.connecting = true
    try {
      const { ticket } = await getWsTicket()
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
      this.ws = new WebSocket(`${proto}//${location.host}${ENV.wsBaseUrl}/hub?ticket=${ticket}`)
      this.bind()
    } catch {
      this.scheduleReconnect()
    } finally {
      this.connecting = false
    }
  }

  private bind() {
    const ws = this.ws!
    ws.onopen = () => {
      this.attempt = 0
      this.startHeartbeat()
      this.subs.forEach((s) => this.send({ v: 1, type: 'sub', id: this.nextId(), topic: s.topic, nodeId: s.nodeId, payload: s.params }))
      this.queue.splice(0).forEach((env) => this.send(env))
    }
    ws.onmessage = (ev) => this.dispatch(JSON.parse(ev.data as string) as WsEnvelope)
    ws.onclose = (ev) => {
      this.stopHeartbeat()
      if (ev.code !== 1000) this.scheduleReconnect()
    }
    ws.onerror = () => ws.close()
  }
  /** 订阅：同一 topic+nodeId+params 只占一条服务端订阅，返回退订函数 */
  subscribe<T>(topic: string, handler: Handler<T>, opts: { nodeId?: string; params?: object } = {}) {
    const key = `${topic}|${opts.nodeId ?? ''}|${JSON.stringify(opts.params ?? {})}`
    this.subs.set(key, { key, topic, nodeId: opts.nodeId, params: opts.params, handler: handler as Handler })
    this.send({ v: 1, type: 'sub', id: this.nextId(), topic, nodeId: opts.nodeId, payload: opts.params })
    void this.connect()
    return () => this.unsubscribe(key)
  }

  unsubscribe(key: string) {
    const sub = this.subs.get(key)
    if (!sub) return
    this.subs.delete(key)
    this.send({ v: 1, type: 'unsub', id: this.nextId(), topic: sub.topic, nodeId: sub.nodeId })
    if (this.subs.size === 0) this.close()   // 无订阅者时释放连接
  }

  /** 切换节点：重建所有带 nodeId 的订阅 */
  resubscribeAll(nodeId: string) {
    ;[...this.subs.values()].filter((s) => s.nodeId).forEach((s) => {
      this.unsubscribe(s.key)
      this.subscribe(s.topic, s.handler, { nodeId, params: s.params })
    })
  }

  private dispatch(env: WsEnvelope) {
    if (env.type === 'pong') { this.pongDeadline = undefined; return }
    if (env.type === 'error') { logger.warn('[ws]', env.payload); return }
    if (env.type !== 'event') return
    this.subs.forEach((s) => {
      if (s.topic === env.topic && (!s.nodeId || s.nodeId === env.nodeId)) s.handler(env.payload, env)
    })
  }

  private send(env: WsEnvelope) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(env))
    else if (env.type === 'sub' || env.type === 'unsub') this.queue.push(env)
  }

  private startHeartbeat() {
    this.heartbeatTimer = window.setInterval(() => {
      if (this.pongDeadline && Date.now() > this.pongDeadline) { this.ws?.close(); return }
      this.pongDeadline = Date.now() + 10_000   // 10s 内未收到 pong 视为死连接
      this.send({ v: 1, type: 'ping', id: this.nextId() })
    }, 25_000)
  }

  private scheduleReconnect() {
    // 指数退避：1s、2s、4s… 上限 30s，叠加 ±20% 抖动避免集体重连
    const base = Math.min(1000 * 2 ** this.attempt++, 30_000)
    const delay = base * (0.8 + Math.random() * 0.4)
    setTimeout(() => this.connect(), delay)
  }
  private nextId() { return `c-${++this.seq}` }
}

export const wsClient = new WsClient()
```

页面隐藏降频：`document.visibilitychange` 触发时，对 `interval` 类订阅重发 `sub` 并把 `payload.interval` 放大到 60 秒（隐藏）或恢复原值（可见）；隐藏超过 5 分钟则整连接 `close(1000)`，可见时重新握手。心跳与重连由 `WsClient` 内部承担，业务侧只用 `useWebSocket` hook：

```ts
export function useWebSocket<T>(topic: string, handler: (p: T) => void, opts?: { nodeId?: string; params?: object }) {
  let off: (() => void) | undefined
  onMounted(() => { off = wsClient.subscribe<T>(topic, handler, opts ?? {}) })
  onUnmounted(() => off?.())
  onDeactivated(() => off?.())
  onActivated(() => { off = wsClient.subscribe<T>(topic, handler, opts ?? {}) })
}
```

### 6.11.3 SSE 与轮询兜底

| 方案 | 使用场景 | 不用于 |
| --- | --- | --- |
| WebSocket | 双向或高频：终端、监控指标、容器 stats、告警推送、任务进度、文件传输进度 | 一次性的长任务结果查询 |
| SSE | 单向、一次性的长输出流：应用安装日志、`docker build` 输出、备份执行日志、批量下发的逐节点结果。用 `EventSource` + `Last-Event-ID` 断点续传，服务端可直接复用 HTTP 中间件与鉴权 | 需要客户端上行的场景 |
| 轮询 | 兜底：WS 连续 3 次重连失败（企业网关拦截 Upgrade）后自动切换。切换时顶栏显示「实时通道降级，数据刷新间隔 15 秒」的 `a-alert`，并每 60 秒尝试恢复 WS | 秒级刷新的大盘（降级后大盘改为 15 秒并提示） |

SSE 因浏览器对同域连接数有限制（HTTP/1.1 下 6 条），同一时刻最多允许 2 条 SSE 流，超出时排队并提示用户。

### 6.11.4 刷新策略

| 页面 | 通道 | 间隔 | 隐藏时 | 备注 |
| --- | --- | --- | --- | --- |
| 概览仪表盘 | WS `monitor.metrics` | 5s | 暂停 | 首屏先用 HTTP 拉一次历史，再由 WS 增量追加 |
| 监控大盘（全屏） | WS `monitor.metrics` | 2s | 60s | 投屏场景不暂停，仅降频 |
| 节点列表 | WS `node.status` | 事件驱动 | 暂停 | 心跳变化才推，不定时全量 |
| 容器列表 | WS `container.stats` | 5s | 暂停 | 仅当前页可见的容器 ID 在订阅参数里 |
| 网站/数据库/运行环境列表 | HTTP 轮询 | 30s | 暂停 | 状态变化不频繁，轮询更省 |
| 告警事件 | WS `alert.event` | 事件驱动 | 保持 | 未读数必须实时，页面隐藏也要收 |
| 任务中心 | WS `task.progress` | 事件驱动 | 保持 | 任务完成需要通知用户 |
| 日志 / 终端 | WS 专用连接 | 流式 | 保持 | 用户主动打开，隐藏时不断流 |
| 审计日志 | 无自动刷新 | 手动 | — | 历史数据，自动刷新会打断阅读 |


## 6.12 表单与校验

### 6.12.1 校验规则库

规则集中在 `utils/validator.ts`，导出「规则工厂」而非裸正则，供 `a-form-item` 的 `:rules` 直接使用。所有正则必须有对应单测。

```ts
import type { FieldRule } from '@arco-design/web-vue'

const RE = {
  domain: /^(?!-)[a-zA-Z0-9-]{1,63}(?<!-)(\.(?!-)[a-zA-Z0-9-]{1,63}(?<!-))+$/,
  wildcardDomain: /^(\*\.)?(?!-)[a-zA-Z0-9-]{1,63}(?<!-)(\.[a-zA-Z0-9-]{1,63})+$/,
  ipv4: /^((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/,
  cidr: /^((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\/(3[0-2]|[12]?\d)$/,
  absPath: /^\/(?!.*\/\.\.(\/|$))[^\0]*$/,          // 绝对路径且禁止 .. 穿越
  linuxName: /^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$/,
  dbName: /^[a-zA-Z_][a-zA-Z0-9_]{0,63}$/,
  envKey: /^[A-Z_][A-Z0-9_]*$/,
}

export const required = (msg = '此项为必填'): FieldRule => ({ required: true, message: msg })
export const domain = (msg = '请输入合法域名，如 example.com'): FieldRule => ({ match: RE.domain, message: msg })
export const ipOrDomain = (): FieldRule => ({
  validator: (v, cb) => (!v || RE.ipv4.test(v) || RE.domain.test(v) ? cb() : cb('请输入合法的 IP 或域名')),
})
export const port = (opts: { reserved?: number[] } = {}): FieldRule => ({
  validator: (v: number, cb) => {
    if (v == null || v === ('' as never)) return cb()
    if (!Number.isInteger(v) || v < 1 || v > 65535) return cb('端口需为 1-65535 的整数')
    if ((opts.reserved ?? [34567, 34568]).includes(v)) return cb('该端口已被面板占用，请更换')
    cb()
  },
})
export const absPath = (msg = '请输入绝对路径，且不得包含 ..'): FieldRule => ({ match: RE.absPath, message: msg })
export const cidr = (): FieldRule => ({ match: RE.cidr, message: '请输入合法 CIDR，如 192.168.1.0/24' })

/** 密码强度：长度 ≥ 12，且大写/小写/数字/符号至少三类 */
export const passwordStrength = (min = 12): FieldRule => ({
  validator: (v: string, cb) => {
    if (!v) return cb()
    if (v.length < min) return cb(`密码长度至少 ${min} 位`)
    const kinds = [/[a-z]/, /[A-Z]/, /\d/, /[^\w\s]/].filter((r) => r.test(v)).length
    if (kinds < 3) return cb('需包含大写字母、小写字母、数字、符号中的至少三类')
    cb()
  },
})

/** cron：支持 5 段与 6 段（含秒），逐段按取值域校验 */
export const cron = (): FieldRule => ({
  validator: (v: string, cb) => {
    if (!v) return cb()
    const parts = v.trim().split(/\s+/)
    if (parts.length !== 5 && parts.length !== 6) return cb('cron 表达式需为 5 段或 6 段')
    const ranges: Array<[number, number]> = parts.length === 6
      ? [[0, 59], [0, 59], [0, 23], [1, 31], [1, 12], [0, 7]]
      : [[0, 59], [0, 23], [1, 31], [1, 12], [0, 7]]
    const seg = /^(\*|\d+|\d+-\d+)(\/\d+)?(,(\*|\d+|\d+-\d+)(\/\d+)?)*$/
    for (let i = 0; i < parts.length; i++) {
      if (!seg.test(parts[i])) return cb(`第 ${i + 1} 段格式不合法`)
      const nums = parts[i].match(/\d+/g)?.map(Number) ?? []
      if (nums.some((n) => n < ranges[i][0] || n > ranges[i][1])) return cb(`第 ${i + 1} 段取值超出范围`)
    }
    cb()
  },
})
```

密码强度同时在 UI 上给出 `a-progress` 强度条（弱/中/强三档），文案说明缺哪一类；cron 输入框下方实时显示「下次执行时间」（前端用 `utils/cronNext.ts` 计算未来 3 次，仅作预览，权威调度以后端为准）。

### 6.12.2 动态表单（DynamicForm）

应用商店的应用参数由后端模板描述，前端按 JSON Schema 子集渲染，不为每个应用写页面。

```json
{
  "version": 1,
  "fields": [
    { "key": "PANEL_PORT", "label": "服务端口", "type": "number", "default": 8080,
      "required": true, "rules": ["port"], "help": "映射到宿主机的端口" },
    { "key": "DB_PASSWORD", "label": "数据库密码", "type": "password",
      "required": true, "rules": ["passwordStrength:12"], "generate": "random:16" },
    { "key": "ENABLE_REDIS", "label": "启用 Redis 缓存", "type": "switch", "default": false },
    { "key": "DATA_DIR", "label": "数据目录", "type": "path", "default": "/opt/nova/data/{{app}}",
      "required": true, "rules": ["absPath"] },
    { "key": "TIMEZONE", "label": "时区", "type": "select", "default": "Asia/Shanghai",
      "options": [{ "label": "上海", "value": "Asia/Shanghai" }, { "label": "UTC", "value": "UTC" }] },
    { "key": "REDIS_HOST", "label": "Redis 地址", "type": "input",
      "visibleWhen": { "key": "ENABLE_REDIS", "eq": true }, "rules": ["ipOrDomain"] }
  ]
}
```

| 支持的 type | 渲染控件 |
| --- | --- |
| `input` / `textarea` / `password` | `a-input` / `a-textarea` / `a-input-password`（带生成随机值按钮） |
| `number` | `a-input-number`，`min`/`max`/`step` 来自 schema |
| `switch` / `radio` / `select` / `multi-select` | 对应 Arco 控件，`options` 支持远端 `optionsFrom`（接口路径） |
| `path` | `a-input` + 文件选择器按钮（唤起 `FileTree` 抽屉） |
| `domain` / `cidr` / `port` | `a-input` 并自动挂对应规则 |

实现要点：`rules` 数组里的字符串按 `名称:参数` 解析后从 `validator.ts` 工厂表取函数，schema 里不允许出现正则或代码（避免执行不可信内容）；`visibleWhen` 支持 `eq`/`ne`/`in` 三种断言，隐藏字段不参与校验且提交时剔除；`default` 中的 `{{app}}`、`{{node}}` 占位在渲染前用上下文替换；`generate: "random:16"` 渲染出「生成」按钮，调 `crypto.getRandomValues` 本地生成。schema 版本不匹配时降级为「该应用需要升级面板后安装」并禁止提交。

### 6.12.3 危险操作确认（DangerConfirm）

| 项 | 定义 |
| --- | --- |
| props | `visible`、`title`、`resourceName: string`（要求用户输入的名称）、`impacts: string[]`（逐条列出后果）、`confirmWord?: string`（默认取 `resourceName`）、`checkboxes?: string[]`（额外勾选项，如「我已备份数据」）、`onConfirm: () => Promise<void>` |
| emits | `update:visible`、`done()` |
| 要点 | 输入完全匹配（区分大小写、去首尾空格）才启用确认按钮；确认按钮 `status="danger"` 且不是默认焦点；`impacts` 用 `a-list` 展示，含不可逆项时前置 `icon-exclamation-circle-fill` 红色标注「此操作不可撤销」；提交中禁用关闭 |

必须使用该组件的操作：删除网站（含数据）、删除数据库、删除容器与卷、移除节点、重置管理员密码、清空回收站、恢复备份（会覆盖现有数据）、批量停止服务。一般删除（删除告警规则、删除计划任务）用 `Modal.confirm` 即可，无需输入名称。


## 6.13 页面模板与交互范式

### 6.13.1 六类页面骨架

| 类型 | 骨架 | 要点 |
| --- | --- | --- |
| 列表页 | `PageHeader` → `SearchForm` → `ProTable`（工具栏 + 表格 + 分页） | 一屏内至少看到 8 行数据；主操作只有一个且在左上；筛选条件与分页同步 URL 可分享 |
| 详情页 | `PageHeader`（含返回、状态标签、主操作） → `a-descriptions` 概要 → `a-tabs` 分维度（基本信息/配置/日志/监控/操作记录） | Tab 切换写 URL query；概要区固定不随 Tab 滚动；状态由 WS 或 30s 轮询更新 |
| 向导页 | `a-steps`（横向）→ 当前步骤表单 → 底部固定「上一步/下一步/完成」 | 每步离开时校验并缓存到 `reactive` 草稿；最后一步给完整配置预览；提交后跳 `Result` 页并给「查看详情/再创建一个」 |
| 配置页 | 左侧 `a-anchor` 分组导航 → 右侧分组卡片 → 底部固定保存条（有改动才出现） | 表单脏检查用 `JSON.stringify` 快照比对；离开前 `onBeforeRouteLeave` 拦截未保存改动；危险开关（关闭防火墙、开放 SSH root）单独加二次确认 |
| 终端页 | 顶部会话标签条（多会话）→ `Terminal` 占满剩余高度 → 底部状态条（节点、连接状态、延迟） | `meta.fullscreen`；不加内容区 padding；标签关闭需确认；会话数上限 8 |
| 仪表盘页 | 顶部时间范围与节点筛选 → `MetricCard` 指标行（4 列）→ 图表网格（`a-grid`，2 列，窄屏 1 列）→ 底部 Top N 列表 | 所有图表共享一个时间范围与 `group` 联动；单图可全屏放大；数据为空时图表区显示 `Empty` 而非空坐标系 |

### 6.13.2 关键页面线框

- 概览仪表盘：第一行 4 个 `MetricCard`（CPU、内存、磁盘、负载，带 sparkline）；第二行左 2/3 为「系统资源趋势」多轴折线（CPU/内存/网络），右 1/3 为「节点状态」环形图 + 在线数；第三行左为「待处理告警」列表（最多 5 条，按级别色条，点击跳事件详情），右为「快捷入口」（创建网站、打开终端、备份、应用商店）；第四行「最近操作」审计摘要 8 条。首屏不放需要二次请求的重内容。
- 节点列表：`ProTable`，列为「名称/IP、状态、角色、CPU、内存、磁盘、Agent 版本、最后心跳、操作」；资源列用细进度条 + 百分比；行操作为「详情、终端、重启 Agent、移除」；工具栏有「添加节点」（弹出安装命令，带一键复制）与「批量升级 Agent」。
- 节点详情：概要 `a-descriptions`（主机名、OS、内核、架构、启动时间、时区）；Tab 为「监控（复用仪表盘图表组件）/进程/服务/磁盘与挂载/网卡/日志/操作记录」。
- 网站创建向导：4 步——基本信息（域名、类型、根目录）→ 运行环境（PHP/Node 版本、端口、反代目标）→ 数据库（可选，创建或关联）→ 证书与确认（申请 Let's Encrypt / 上传 / 暂不启用 + 完整配置预览）。域名输入即校验并异步查重，重复时行内报错并给「查看已有站点」链接。
- 容器管理：`ProTable` + 顶部 4 个统计（运行中、已停止、镜像数、占用磁盘）；行内嵌 CPU/内存迷你条（WS `container.stats`）；行操作「启动/停止/重启、日志、终端、详情、删除」；批量操作「批量停止、批量删除（DangerConfirm）」；`a-tabs` 切换容器/镜像/编排/网络/卷。
- 监控大盘：全屏深色优先；顶部时间范围（最近 1h/6h/24h/7d/自定义）+ 节点多选 + 自动刷新开关；图表区 6 宫格（CPU、内存、磁盘 IO、网络吞吐、负载、TCP 连接数）；支持任意图表 `dataZoom` 后其余图表同步区间。
- 告警事件：左侧筛选（级别、状态、模块、时间）；右侧事件列表（级别色条 + 标题 + 节点 + 首次/最近发生时间 + 次数），行展开显示触发值与规则详情；行操作「确认、静默 1h/8h/24h、转任务、查看关联指标」；顶部三个数字概览（未确认严重、未确认警告、今日已恢复）。
- 审计日志：`ProTable` + 高级筛选（操作人、模块、动作、节点、结果、时间范围、IP）；关键列「时间、操作人、节点、模块:动作、目标、结果、耗时、traceId」；行展开显示请求参数与响应摘要（敏感字段脱敏）；支持按当前筛选导出 CSV；`traceId` 可复制并用于向后端定位。

### 6.13.3 四态统一处理

所有数据区域必须显式处理四种状态，由 `components/StateBoundary` 统一渲染，页面不各写一套。

| 状态 | 展示 | 必须给出的下一步 |
| --- | --- | --- |
| 加载中 | 首次进入用骨架屏（结构与真实内容同形），刷新用局部 `Spin` 遮罩且容器高度不变 | 超过 8 秒显示「仍在加载，可能是节点响应较慢」 |
| 空 | `Empty` + 插画尺寸 120px；区分「暂无数据」与「无匹配结果」 | 无数据给主操作按钮；无匹配给「清空筛选条件」 |
| 错误 | `a-result status="error"`，标题为用户可理解的原因，正文含 `traceId` | 「重试」按钮 + 「查看帮助」链接；节点离线时额外给「切换节点」 |
| 无权限 | `a-result status="403"` | 「返回概览」+ 「申请权限」（跳到设置页说明由管理员分配） |

### 6.13.4 操作反馈选择

| 反馈方式 | 适用 | 判定依据 |
| --- | --- | --- |
| 行内状态变更（无提示） | 开关类即时生效且结果在界面上可见（启停开关变色、状态标签变化） | 用户能直接看到结果，弹提示属噪声 |
| `Message.success` | 短操作成功且页面未跳转（保存配置、重载 Nginx、复制成功） | 结果不改变页面结构，3 秒后可消失 |
| `Message.error` | 可重试的失败（校验不通过、名称冲突） | 用户改一下就能继续 |
| `Notification` | 异步任务完成、需要保留信息（备份完成并给下载链接、证书签发成功且写明有效期） | 用户可能已切页，需要跨页面可见 |
| `Modal.confirm` | 有副作用但可逆的操作（重启服务、停止容器） | 需要用户确认一次 |
| `DangerConfirm` | 不可逆或影响面大的操作（见 6.12.3 清单） | 需要输入名称二次确认 |
| `Modal.error` | 阻断性失败（面板内部错误、节点离线导致指令无法下发） | 用户必须知晓且当前流程无法继续 |
| 跳转 `Result` 页 | 创建向导完成、初始化完成 | 流程终点，需要引导下一步 |

### 6.13.5 长任务与任务中心

后端返回 `{ taskId, status: 'pending' }` 的接口一律走任务中心：前端 `taskStore.track(taskId, meta)` 登记 → 订阅 WS `task.progress` → 顶栏图标显示运行中数量并带进度环 → 完成时按结果弹 `Notification`（成功给「查看」，失败给「查看日志」）。约定：

- 发起长任务后立即关闭表单弹窗并显示「任务已提交，可在任务中心查看进度」，不阻塞用户。
- 任务详情展示逐节点结果（批量下发场景），部分失败时显示「成功 8 / 失败 2」并可只看失败项，禁止聚合成一句「操作失败」。
- 刷新页面后任务中心从 `GET /tasks?status=running` 恢复列表，不依赖前端内存。
- 同一资源上的互斥任务（同一网站同时改配置与续证）由后端拒绝，前端展示后端返回的冲突提示并给「查看进行中的任务」。


## 6.14 国际化

### 6.14.1 配置

```ts
// locale/index.ts
import { createI18n } from 'vue-i18n'
import zhCN from './lang/zh-CN'
import enUS from './lang/en-US'
import arcoZhCN from '@arco-design/web-vue/es/locale/lang/zh-cn'
import arcoEnUS from '@arco-design/web-vue/es/locale/lang/en-us'

export const ARCO_LOCALE = { 'zh-CN': arcoZhCN, 'en-US': arcoEnUS }

export const i18n = createI18n({
  legacy: false,              // 组合式 API 模式，配合 <script setup> 的 useI18n()
  globalInjection: true,      // 模板中可直接用 $t
  locale: getStoredLocale() ?? navigatorLocale(),
  fallbackLocale: 'zh-CN',
  messages: { 'zh-CN': zhCN, 'en-US': enUS },
  missingWarn: import.meta.env.DEV,
})
```

Arco 组件文案通过 `<a-config-provider :locale="ARCO_LOCALE[app.locale]">` 包裹根组件切换，与 vue-i18n 的 `locale` 由 `useAppStore().setLocale()` 同时更新，`<html lang>` 一并同步。

### 6.14.2 key 命名与语言包组织

key 三段式 `module.page.field`，公共文案用 `common.*`，菜单用 `menu.<module>.<page>`，校验提示用 `validate.*`，业务操作用 `<module>.action.*`。

```text
locale/lang/zh-CN/
├── index.ts        # 聚合导出
├── common.ts       # 按钮、四态、通用字段（confirm/cancel/created_at…）
├── menu.ts         # 与路由 meta.title 一一对应
├── validate.ts     # 校验提示
├── errorCode.ts    # 后端错误码文案覆盖表
└── modules/website.ts  # 每业务模块一文件，与 views/<module> 同名
```

约束：语言包只放静态文案，不放业务逻辑；禁止拼接 key（`t('a.' + type)` 不可静态分析），需要动态选择时用显式映射表；复数与插值用 vue-i18n 语法 `t('website.tip.deleteCount', { n })`；`zh-CN` 为基准，CI 用脚本比对 `en-US` 缺失/多余 key 并失败。

### 6.14.3 本地化细节与错误码文案

- 时间：统一 `dayjs` + `utils/datetime.ts`，展示格式由语言决定（`zh-CN` 为 `YYYY-MM-DD HH:mm:ss`，`en-US` 为 `MMM D, YYYY HH:mm:ss`）；时区一律用浏览器本地时区渲染，并在页面右上角显示当前时区，跨时区排查时可切换为 UTC。
- 数字：用 `Intl.NumberFormat`；字节走 `SizeText`（1024 进制，`KiB/MiB/GiB`）；百分比保留 1 位小数；速率单位 `Mbps` 与 `MB/s` 不混用。
- 切换语言：`setLocale` 更新 store → 更新 `i18n.global.locale` → 更新 `<html lang>` 与 dayjs locale → 重设 `document.title`。不刷新页面，因此所有文案必须通过 `t()` 获取，禁止在 `setup` 中把 `t('x')` 的结果存进普通变量（应用 `computed`）。
- 后端错误码文案：后端按 `Accept-Language` 返回本地化 `message`，前端只对需要改写的错误码做覆盖。落地时 `ERROR_MESSAGE_MAP` 的值写成 i18n key（如 `errorCode.40301`），由 `handleBizError` 调 `t()` 取文案，6.4.4 的示例直接列中文仅为便于阅读。两侧不一致时以后端为准，前端覆盖表只用于把技术性描述改写为面向运维的说法（如把「exec format error」写成「二进制架构与目标节点不匹配」）。


## 6.15 性能优化

### 6.15.1 加载与渲染

| 手段 | 落地方式 |
| --- | --- |
| 路由懒加载 | 全部路由 `component: () => import('@/views/...')`，禁止静态 import |
| 组件异步 | `CodeEditor`、`Terminal`、`LogViewer`、大盘图表用 `defineAsyncComponent(() => import(...))` 并给 `loadingComponent` 骨架 |
| 预加载 | 侧边栏菜单项 `mouseenter` 时触发对应路由的 `import()` 预取；登录成功后空闲期（`requestIdleCallback`）预取概览与网站列表 chunk |
| 虚拟列表 | 文件列表 > 200 项用 `a-table` 的 `virtual-list`；日志用 `LogViewer` 自实现虚拟滚动；`a-select` 选项 > 100 开 `virtual-list-props` |
| 防抖节流 | 搜索输入 300ms 防抖；`ResizeObserver` 回调 100ms 防抖；滚动监听 `passive: true` + 16ms 节流；WS 高频指标在 `ChartWrapper` 内按帧合并（`requestAnimationFrame` 内只 `setOption` 一次） |
| 图表按需渲染 | `IntersectionObserver` 惰性初始化：图表进入视口才 `echarts.init`；页面隐藏时 `chart.clear()` 不销毁；大盘超过 6 个图表时改用 `canvas` 渲染器并关闭动画 |
| 长列表转换 | 后端已分页的数据不在前端二次排序/过滤；必须前端处理时用 `computed` 缓存，禁止在模板里调用带循环的函数 |
| 表格性能 | 列 `render` 函数不创建新对象/数组字面量；行内组件（`StatusTag`、迷你图）用 `v-memo` 或提取为 `functional` 风格组件 |

### 6.15.2 请求层面

- 列表页离开时 `useAbort` 中止在途请求，避免返回后的无效渲染。
- 字典类数据（节点列表、分组、PHP 版本）在 store 中缓存 5 分钟，`force` 参数可强刷。
- 同一时刻相同 URL + 参数的 GET 请求做去重（`request.ts` 维护 in-flight Map，命中则复用 Promise），解决多个组件同时拉同一份数据。
- 首屏并行发起 `profile` 与 `nodes`，不串行等待。

### 6.15.3 首屏体积预算

| chunk | 内容 | gzip 目标 | 硬上限 |
| --- | --- | --- | --- |
| `vue` | vue、vue-router、pinia | ≤ 45 KB | 55 KB |
| `arco` | Arco 组件与样式（按需后） | ≤ 120 KB | 150 KB |
| `vendor` | axios、dayjs、lodash-es、vue-i18n 等 | ≤ 55 KB | 70 KB |
| `index` | 布局、路由表、store、utils | ≤ 60 KB | 80 KB |
| CSS（首屏） | 令牌 + 布局 + 首屏组件样式 | ≤ 35 KB | 45 KB |
| 首屏合计 | 以上之和 | ≤ 315 KB | 400 KB |
| `echarts` | 按需注册后的图表库 | ≤ 130 KB | 160 KB |
| `monaco` | 编辑器 + worker | ≤ 320 KB | 400 KB |
| `xterm` | 终端 + 三个插件 | ≤ 60 KB | 80 KB |

`echarts`/`monaco`/`xterm` 不计入首屏（按路由懒加载）。CI 用 `scripts/check-bundle.ts` 读取 `dist` 下 `.gz` 体积比对上表，超硬上限则失败；超目标值仅告警。

### 6.15.4 Lighthouse 目标

以 `/login` 与 `/dashboard`（本地 HTTPS、模拟 4x CPU 降速、Fast 3G 关闭）为基准页：Performance ≥ 90、Accessibility ≥ 95、Best Practices ≥ 95、SEO 不作要求（内网面板，允许 ≥ 70）。核心指标：FCP ≤ 1.2s、LCP ≤ 2.0s、TBT ≤ 200ms、CLS ≤ 0.05。每次发版前跑一次并把报告归档到 `docs/perf/`。


## 6.16 可访问性与响应式

### 6.16.1 可访问性

| 项 | 要求 |
| --- | --- |
| 键盘可达 | 所有可操作元素能 Tab 到达且有可见焦点环（`--nova-shadow-focus`）；表格行操作、下拉菜单项、页签关闭按钮不得只响应鼠标；禁止 `outline: none` 而不补焦点样式 |
| 焦点管理 | Modal/Drawer 打开时焦点移到首个可交互元素并做焦点陷阱（Arco 已内建，自定义弹层需自行实现）；关闭后焦点归还触发元素；`DangerConfirm` 的默认焦点在输入框而非确认按钮 |
| 快捷键 | Command-K 全局搜索、`Esc` 关闭弹层、`Ctrl+S` 编辑器保存、`Ctrl+Shift+F` 终端搜索；快捷键列表在设置页可查；不覆盖浏览器原生快捷键 |
| aria | 图标按钮必须有 `aria-label`；`SvgIcon` 装饰性图标 `aria-hidden="true"`；状态区域用 `role="status" aria-live="polite"`（告警计数、任务进度）；表格 `scope="col"`；错误提示与表单项用 `aria-describedby` 关联 |
| 色盲友好 | 状态一律「颜色 + 图标 + 文案」三重编码（见 6.9.4）；图表折线用不同线型（实线/虚线/点线）而非仅颜色区分，且开启数据标记；对比度满足 WCAG AA（正文 4.5:1，大字 3:1），暗色主题单独校验 |
| 文本 | 不使用纯图片承载信息；禁止用 `title` 属性替代可见文案；错误信息必须说明「哪里错了、怎么改」 |

### 6.16.2 响应式与断点

| 断点 | 范围 | 适配策略 |
| --- | --- | --- |
| `xs` | < 576px | 不做完整适配。仅保证登录、概览、告警事件、终端可用；列表页降级为卡片流（`ProTable` 的 `mobileCard` 模式），隐藏非关键列 |
| `sm` | 576-767px | 侧栏改抽屉式；页签栏隐藏；搜索表单单列 |
| `md` | 768-991px | 侧栏自动折叠为图标；搜索表单 2 列；图表 1 列 |
| `lg` | 992-1279px | 完整布局，侧栏展开；搜索表单 3 列；图表 2 列 |
| `xl` | 1280-1599px | 目标分辨率；指标卡 4 列 |
| `xxl` | ≥ 1600px | 内容区最大宽度不限（运维表格需要横向空间），但仪表盘图表网格最多 3 列，避免图形过扁 |

取舍说明：面板的主场景是桌面浏览器，移动端只保证「查看状态 + 应急操作」。文件管理、容器编排、监控大盘、代码编辑在 < 768px 时直接给出「该功能建议在桌面端使用」的提示页，不做残缺适配。断点值与 Arco `a-grid` 的 `responsive` 保持一致，统一在 `styles/mixin.scss` 中以 `@mixin respond-to($bp)` 提供，禁止在业务样式里写裸 `@media`。


## 6.17 代码规范与质量保障

### 6.17.1 ESLint / Prettier / stylelint

ESLint 9 扁平配置 `eslint.config.ts`，按顺序组合：`@eslint/js` 推荐集 → `typescript-eslint` 的 `recommendedTypeChecked` → `eslint-plugin-vue` 的 `flat/recommended` → `.eslintrc-auto-import.json`（unplugin 生成的全局变量）→ `eslint-config-prettier` 关闭格式规则。项目自定规则：

| 规则 | 设置 | 目的 |
| --- | --- | --- |
| `vue/component-api-style` | `['script-setup']` | 禁止 Options API |
| `vue/block-order` | `script` → `template` → `style` | 统一 SFC 结构 |
| `vue/define-macros-order` | `defineOptions` → `defineProps` → `defineEmits` → `defineSlots` | 宏顺序一致 |
| `vue/no-undef-components` | error（允许 Arco 自动引入的前缀） | 防止漏注册 |
| `vue/multi-word-component-names` | error | 组件名必须多词 |
| `@typescript-eslint/no-explicit-any` | error（测试文件降为 warn） | 禁 any |
| `@typescript-eslint/consistent-type-imports` | error | 类型导入用 `import type` |
| `no-restricted-imports` | 禁止 `views` 引 `axios`、`components` 引 `store`、`api` 引 `store` | 固化 6.1.3 分层约束 |
| `complexity` | `['error', 12]` | 函数圈复杂度上限 |
| `max-lines-per-function` | `['warn', 80]` | 超长函数拆分 |

Prettier：`printWidth: 120`、`semi: false`、`singleQuote: true`、`trailingComma: 'all'`、`arrowParens: 'always'`、`vueIndentScriptAndStyle: false`。stylelint 16：`stylelint-config-standard-scss` + `stylelint-config-recommended-vue` + `stylelint-config-recess-order`，自定规则禁止颜色字面量（必须用 `var(--nova-*)`）、禁止 `z-index` 字面量、禁止非 4 倍数的 `px` 间距。

提交前钩子：`husky` + `lint-staged`，对 `*.{ts,vue}` 跑 `eslint --fix`，对 `*.{scss,vue}` 跑 `stylelint --fix`，并在 pre-push 跑 `pnpm typecheck`。

### 6.17.2 组件复杂度约束

单文件 ≤ 400 行；`<script setup>` ≤ 200 行；props ≤ 12 个（更多则合并为配置对象）；`watch` ≤ 5 个（更多说明状态设计有问题）；模板嵌套 ≤ 6 层；一个组件只暴露一个主职责，出现「且」字描述时拆分。超限由 ESLint 与 CR 双重把关，不允许用注释豁免。

### 6.17.3 Vitest 单测

`vitest.config.ts` 复用 `vite.config.ts` 的 alias 与插件，`environment: 'jsdom'`，覆盖率阈值：语句 70%、分支 60%、`utils/` 与 `store/` 单独要求 85%。

```ts
// components/StatusTag/__tests__/StatusTag.spec.ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import StatusTag from '../index.vue'

describe('StatusTag', () => {
  it('渲染字典中的文案与语义色', () => {
    const w = mount(StatusTag, { props: { status: 'running' } })
    expect(w.text()).toContain('运行中')
    expect(w.find('.arco-tag-green').exists()).toBe(true)
  })

  it('未知状态不抛错并回落为「未知」', () => {
    const w = mount(StatusTag, { props: { status: 'not-exist' as never } })
    expect(w.text()).toContain('未知')
  })

  it('同时输出图标，保证不只靠颜色传达状态', () => {
    const w = mount(StatusTag, { props: { status: 'error' } })
    expect(w.find('svg').exists()).toBe(true)
  })
})
```

```ts
// store/modules/__tests__/node.spec.ts
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useNodeStore } from '../node'

vi.mock('@/api/cluster', () => ({
  listNodes: vi.fn().mockResolvedValue({
    list: [
      { id: 'n1', name: 'local', isLocal: true, status: 'online' },
      { id: 'n2', name: 'web-02', isLocal: false, status: 'offline' },
    ],
    total: 2, page: 1, pageSize: 500,
  }),
}))
vi.mock('@/utils/ws', () => ({ wsClient: { resubscribeAll: vi.fn() } }))

describe('useNodeStore', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('拉取节点后默认选中本机', async () => {
    const store = useNodeStore()
    await store.fetchNodes()
    expect(store.currentNodeId).toBe('n1')
    expect(store.isLocal).toBe(true)
  })

  it('持久化的节点已不存在时回落本机', async () => {
    const store = useNodeStore()
    store.currentNodeId = 'gone'
    await store.fetchNodes()
    expect(store.currentNodeId).toBe('n1')
  })

  it('无活跃终端时切换节点直接生效并重建订阅', async () => {
    const { wsClient } = await import('@/utils/ws')
    const store = useNodeStore()
    await store.fetchNodes()
    await store.switchNode('n2')
    expect(store.currentNodeId).toBe('n2')
    expect(wsClient.resubscribeAll).toHaveBeenCalledWith('n2')
  })
})
```

```ts
// hooks/__tests__/usePermission.spec.ts
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { usePermission } from '../usePermission'
import { usePermissionStore } from '@/store/modules/permission'

describe('usePermission', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('精确匹配，不做 action 蕴含推导', () => {
    usePermissionStore().codes = ['website:site:update']
    const { hasPermission } = usePermission()
    expect(hasPermission('website:site:update')).toBe(true)
    expect(hasPermission('website:site:read')).toBe(false)
  })

  it('mode=all 要求全部命中，any 命中其一即可', () => {
    usePermissionStore().codes = ['a:b:list']
    const { hasPermission } = usePermission()
    expect(hasPermission(['a:b:list', 'a:b:create'], 'all')).toBe(false)
    expect(hasPermission(['a:b:list', 'a:b:create'], 'any')).toBe(true)
  })

  it('超管通配符短路一切判定', () => {
    usePermissionStore().codes = ['*']
    expect(usePermission().hasPermission('any:thing:delete')).toBe(true)
  })
})
```

### 6.17.4 Playwright E2E

`playwright.config.ts`：`baseURL: 'https://127.0.0.1:34567'`、`ignoreHTTPSErrors: true`、`use.storageState` 复用登录态、`webServer` 启动本地面板二进制、失败自动截图与 trace。用例只覆盖关键路径，不覆盖细节交互（那是单测的职责）。

```ts
// e2e/website-create.spec.ts
import { expect, test } from '@playwright/test'

test('登录后完成网站创建向导', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(process.env.E2E_USER!)
  await page.getByLabel('密码').fill(process.env.E2E_PASSWORD!)
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/dashboard/)

  await page.getByRole('link', { name: '网站' }).click()
  await page.getByRole('link', { name: '网站列表' }).click()
  await page.getByRole('button', { name: '创建网站' }).click()

  const domain = `e2e-${Date.now()}.example.com`
  await page.getByLabel('主域名').fill(domain)
  await page.getByLabel('运行环境').click()
  await page.getByRole('option', { name: '静态站点' }).click()
  await page.getByRole('button', { name: '下一步' }).click()

  await page.getByLabel('端口').fill('8081')
  await page.getByRole('button', { name: '下一步' }).click()
  await page.getByRole('button', { name: '跳过' }).click()       // 数据库步骤
  await page.getByRole('button', { name: '完成创建' }).click()

  await expect(page.getByText('创建成功')).toBeVisible()
  await page.getByRole('button', { name: '查看详情' }).click()
  await expect(page.getByText(domain)).toBeVisible()
  await expect(page.getByText('运行中')).toBeVisible()

  // 清理：避免用例间互相污染
  await page.goto('/website/list')
  await page.getByRole('row', { name: new RegExp(domain) }).getByRole('button', { name: '删除' }).click()
  await page.getByLabel('请输入网站域名以确认').fill(domain)
  await page.getByRole('button', { name: '确认删除' }).click()
  await expect(page.getByText(domain)).toBeHidden()
})
```

选择器优先级：`getByRole` / `getByLabel` > `getByTestId`（`data-testid`）> CSS 选择器。禁止用 Arco 内部 class 做选择器（版本升级即失效）。

### 6.17.5 Storybook

可选，不纳入 v1.0 必做项。理由：通用组件仅 20 个左右且都有单测与真实使用页面，维护一套独立的 story 环境收益不足以抵消成本。若后续要做，仅为 `components/` 下的 12 个通用组件建 story，不为业务页面建；`ProTable`、`ChartWrapper`、`Terminal` 因依赖异步数据与 WebSocket，需配 msw handler 才能在 Storybook 中运行。


## 6.18 前后端联调与 Mock

### 6.18.1 选型：msw

| 维度 | vite-plugin-mock | msw（选用） |
| --- | --- | --- |
| 拦截层 | Vite 开发服务器中间件 | Service Worker（浏览器）/ `setupServer`（Node） |
| 复用范围 | 仅 dev server | dev、Vitest、Playwright 三处共用同一套 handler |
| 与真实后端切换 | 需改配置重启 | `VITE_ENABLE_MOCK` 开关 + 单接口 `passthrough()` 可混合 |
| WebSocket | 不支持 | msw 2.x 支持 `ws` handler，可模拟指标推送与任务进度 |
| Vite 版本耦合 | 插件需跟随 Vite 大版本 | 与构建工具无关 |

选 msw：本项目最大的联调痛点是 WebSocket 推送与长任务进度，需要能在单测中复现；一套 handler 三处复用避免 mock 数据漂移。

### 6.18.2 组织方式

```text
web/mocks/
├── browser.ts          # setupWorker，dev 环境按 VITE_ENABLE_MOCK 启动
├── server.ts           # setupServer，vitest setupFiles 引入
├── handlers/
│   ├── index.ts        # 聚合，顺序即优先级
│   ├── auth.ts         # 登录、refresh、profile、ws ticket
│   ├── cluster.ts
│   ├── website.ts
│   └── ws.ts           # hub 的 sub/event 模拟
├── fixtures/           # 静态数据，按实体拆文件，字段与后端 DTO 一致
└── utils.ts            # ok()/err()/paginate() 三个工厂
```

`utils.ts` 强制所有 mock 响应遵守统一信封，避免前端在 mock 下写出真实环境跑不通的代码：

```ts
export const ok = <T>(data: T) =>
  HttpResponse.json({ code: 0, message: 'ok', data, traceId: `mock-${Date.now()}`, timestamp: Date.now() })

export const err = (code: number, message: string, status = 200) =>
  HttpResponse.json({ code, message, data: null, traceId: `mock-${Date.now()}`, timestamp: Date.now() }, { status })

export const paginate = <T>(all: T[], url: URL) => {
  const page = Number(url.searchParams.get('page') ?? 1)
  const pageSize = Number(url.searchParams.get('pageSize') ?? 20)
  return { list: all.slice((page - 1) * pageSize, page * pageSize), total: all.length, page, pageSize }
}
```

规范：fixtures 字段命名与后端 DTO 完全一致（含 `snake_case`/`camelCase` 差异，以 [API 接口规范](./05-API接口规范.md) 为准）；每个 handler 必须至少提供一条失败分支（用 `?_mock=error` 触发），便于验证四态；mock 数据量按真实规模造（网站 60 条、容器 40 条、审计日志 5000 条），暴露分页与虚拟滚动问题；禁止在业务代码里出现任何 mock 判断分支。

### 6.18.3 联调 checklist

| 项 | 判据 |
| --- | --- |
| 信封一致 | 所有接口返回 `code/message/data/traceId/timestamp`，成功 `code=0`；错误也返回 HTTP 200 除鉴权类 |
| 分页参数 | 请求 `page/pageSize/keyword/sort/order`，响应 `list/total/page/pageSize`，字段名与大小写一致 |
| 鉴权 | `Authorization: Bearer` 生效；accessToken 过期返回 401 且 `/auth/refresh` 可续；refreshToken Cookie 带 `HttpOnly; Secure; SameSite=Strict` |
| 节点头 | 所有 `nodeScoped` 接口正确响应 `X-Node-Id`；缺失时后端返回明确错误而非默认本机 |
| 请求 ID | 后端日志能通过 `X-Request-Id` 与 `traceId` 关联，前端错误提示中的 traceId 可检索到 |
| 时间字段 | 统一 RFC3339 带时区（`2026-08-17T10:30:00+08:00`）或毫秒时间戳，全项目只用一种 |
| 枚举值 | 状态枚举字面量与 `config/dict.ts` 完全对齐，后端新增枚举需前端同步，未知值回落 `unknown` 不报错 |
| 错误码 | 6.4.4 映射表中的错误码后端确实会返回；后端新增错误码在联调时补入映射表 |
| WebSocket | ticket 一次性且 30 秒过期；`sub` 有 `ack`；`ping/pong` 周期一致；断连后重连能恢复订阅 |
| 长任务 | 返回 `taskId`；`task.progress` 推送含 `percent/stage/nodeResults`；任务可通过 `GET /tasks` 恢复 |
| 上传下载 | 大文件上传带进度且可取消；下载响应含 `Content-Disposition`，中文文件名用 `filename*=UTF-8''` 编码 |
| 权限点 | `/auth/profile` 返回的权限点与前端路由 `meta.permissions` 逐一对得上，无孤立权限点或缺失项 |
| 跨源 | 生产同源无 CORS；开发经 Vite 代理，`withCredentials` 与 Cookie 能正常带上 |

