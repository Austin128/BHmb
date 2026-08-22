<script setup lang="ts">
/**
 * 文件管理页。顶部为白名单根切换与面包屑，主体为列表与操作区。
 * 所有路径都用后端返回的绝对路径，前端不自行拼接父目录，避免与 pathguard 的判定产生偏差。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import dayjs from 'dayjs'
import { useI18n } from 'vue-i18n'
import { Message, Modal } from '@arco-design/web-vue'

import {
  compressEntries,
  copyEntries,
  createDir,
  createFile,
  decompressEntry,
  deleteEntries,
  downloadFile,
  fetchFileContent,
  fetchFileList,
  fetchRoots,
  moveEntries,
  renameEntry,
  saveFileContent,
  searchFiles,
  updatePermission,
  uploadFile,
  uploadLargeFile,
  CHUNK_UPLOAD_THRESHOLD,
  type FileBatchResponse,
  type FileEntry,
  type FileListResponse,
} from '@/api/file'
import { BizError } from '@/types/api'
import { useUserStore } from '@/store/modules/user'
import { errorText } from '@/utils/errorText'
import { humanSize } from '@/utils/format'

const { t } = useI18n()
const userStore = useUserStore()

/**
 * 按权限点控制入口：无权直接隐藏按钮，避免点下去必然收到 403。
 * 后端仍会在路由层再校一次，这里只是体验层的前置收敛。
 */
const canRead = computed(() => userStore.hasPermission('file:file:read'))
const canCreate = computed(() => userStore.hasPermission('file:file:create'))
const canUpdate = computed(() => userStore.hasPermission('file:file:update'))
const canDelete = computed(() => userStore.hasPermission('file:file:delete'))
const canExec = computed(() => userStore.hasPermission('file:file:exec'))
const canChmod = computed(() => userStore.hasPermission('file:permission:update'))

const roots = ref<string[]>([])
const booting = ref(true)
/** 根目录清单加载失败的原因：内联展示，避免整页只剩一个空表格。 */
const bootError = ref('')
const cwd = ref('')
const data = ref<FileListResponse | null>(null)
const loading = ref(false)
const selectedKeys = ref<string[]>([])
const showHidden = ref(false)
const keyword = ref('')
const searching = ref(false)

const items = computed(() => data.value?.items ?? [])
const selected = computed(() => items.value.filter((it) => selectedKeys.value.includes(it.path)))

/** 面包屑：把绝对路径按分隔符拆开，只允许回到白名单根之内。 */
const crumbs = computed(() => {
  const path = data.value?.path ?? ''
  const root = roots.value.find((r) => path === r || path.startsWith(`${r}/`)) ?? ''
  if (!root) return []
  const rest = path.slice(root.length).split('/').filter(Boolean)
  let acc = root
  return [
    { label: root, path: root },
    ...rest.map((seg) => {
      acc = `${acc}/${seg}`
      return { label: seg, path: acc }
    }),
  ]
})

function reportError(e: unknown) {
  if (e instanceof BizError) {
    Message.error(errorText(e.code, e.message))
    return
  }
  Message.error(e instanceof Error ? e.message : t('common.requestFailed'))
}

async function load(path = cwd.value) {
  if (!path) return
  loading.value = true
  try {
    data.value = await fetchFileList({ path, showHidden: showHidden.value })
    cwd.value = data.value.path
    selectedKeys.value = []
    searching.value = false
    if (data.value.degraded) Message.warning(t('file.degraded'))
  } catch (e) {
    reportError(e)
  } finally {
    loading.value = false
  }
}

async function doSearch() {
  const kw = keyword.value.trim()
  if (!kw) {
    await load()
    return
  }
  loading.value = true
  try {
    const res = await searchFiles({ path: cwd.value, keyword: kw })
    // 搜索结果复用列表渲染，但它不是一个真实目录，因此禁用目录级操作
    data.value = {
      path: cwd.value,
      parent: data.value?.parent ?? '',
      items: res.items,
      total: res.total,
      paged: false,
      degraded: false,
    }
    selectedKeys.value = []
    searching.value = true
    if (res.truncated) Message.warning(t('file.searchTruncated'))
  } catch (e) {
    reportError(e)
  } finally {
    loading.value = false
  }
}

function open(entry: FileEntry) {
  if (entry.isDir) {
    void load(entry.path)
    return
  }
  if (!canRead.value) {
    Message.warning(t('file.noReadPermission'))
    return
  }
  void openEditor(entry)
}

/* ---------- 文本编辑 ---------- */
const editor = reactive({
  visible: false,
  path: '',
  content: '',
  etag: '',
  saving: false,
  backup: true,
})

async function openEditor(entry: FileEntry) {
  try {
    const res = await fetchFileContent(entry.path)
    editor.path = res.path
    editor.content = res.content
    editor.etag = res.etag
    editor.visible = true
    if (res.truncated) Message.warning(t('file.contentTruncated'))
  } catch (e) {
    reportError(e)
  }
}

async function save() {
  editor.saving = true
  try {
    const res = await saveFileContent({
      path: editor.path,
      content: editor.content,
      etag: editor.etag,
      createBackup: editor.backup,
    })
    editor.etag = res.etag // 换新 ETag，支持连续保存
    Message.success(t('file.saved'))
    await load()
  } catch (e) {
    reportError(e)
  } finally {
    editor.saving = false
  }
}

/* ---------- 新建 / 重命名 / 权限 / 压缩 ---------- */
type DialogKind = 'dir' | 'file' | 'rename' | 'permission' | 'compress'

const dialog = reactive({
  visible: false,
  kind: 'dir' as DialogKind,
  name: '',
  mode: '0644',
  target: '',
  format: 'zip' as 'zip' | 'tar.gz' | 'tar',
})

const dialogTitle = computed(() => t(`file.dialog.${dialog.kind}`))

function openDialog(kind: DialogKind, entry?: FileEntry) {
  dialog.kind = kind
  dialog.target = entry?.path ?? ''
  dialog.name = kind === 'rename' ? (entry?.name ?? '') : kind === 'compress' ? 'archive.zip' : ''
  dialog.mode = entry?.modeOctal ?? '0644'
  dialog.visible = true
}

async function submitDialog() {
  try {
    switch (dialog.kind) {
      case 'dir':
        await createDir(`${cwd.value}/${dialog.name}`)
        break
      case 'file':
        await createFile(`${cwd.value}/${dialog.name}`)
        break
      case 'rename':
        await renameEntry(dialog.target, dialog.name)
        break
      case 'permission':
        await updatePermission({ path: dialog.target, mode: dialog.mode })
        break
      case 'compress':
        await compressEntries({
          paths: selected.value.map((it) => it.path),
          dest: `${cwd.value}/${dialog.name}`,
          format: dialog.format,
        })
        break
    }
    dialog.visible = false
    Message.success(t('file.done'))
    await load()
  } catch (e) {
    reportError(e)
  }
}

/* ---------- 上传 / 下载 ---------- */
const uploading = ref(false)
const uploadPercent = ref(0)
// uploadName 用于多文件逐个上传时提示当前进展到哪个文件
const uploadName = ref('')
const fileInput = ref<HTMLInputElement | null>(null)

async function onPickFiles(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = '' // 允许重复选择同一个文件
  if (!files.length) return

  uploading.value = true
  try {
    for (const f of files) {
      uploadPercent.value = 0
      uploadName.value = f.name
      // 大文件走分片：单请求上传一旦中断就得从头来，分片可断点续传
      const send = f.size > CHUNK_UPLOAD_THRESHOLD ? uploadLargeFile : uploadFile
      await send(cwd.value, f, 'reject', (p) => (uploadPercent.value = p))
    }
    Message.success(t('file.uploaded'))
    await load()
  } catch (e) {
    reportError(e)
  } finally {
    uploading.value = false
    uploadPercent.value = 0
    uploadName.value = ''
  }
}

async function download(entry: FileEntry) {
  try {
    await downloadFile(entry.path, entry.name)
  } catch (e) {
    reportError(e)
  }
}

/* ---------- 批量操作 ---------- */
function reportBatch(res: FileBatchResponse) {
  if (!res.failed) {
    Message.success(t('file.done'))
    return
  }
  const first = res.results.find((r) => !r.ok)
  Message.warning(
    t('file.partialFailed', {
      ok: res.succeeded,
      failed: res.failed,
      reason: first ? errorText(first.code ?? 0, first.message ?? '') : '',
    }),
  )
}

function remove(entries: FileEntry[]) {
  if (!entries.length) return
  Modal.warning({
    title: t('file.deleteTitle'),
    content: t('file.deleteTip', { count: entries.length }),
    hideCancel: false,
    onOk: async () => {
      try {
        reportBatch(await deleteEntries(entries.map((it) => it.path)))
        await load()
      } catch (e) {
        reportError(e)
      }
    },
  })
}

/** 剪贴板：move/copy 统一走「先标记来源，再到目标目录粘贴」的交互。 */
const clipboard = reactive<{ paths: string[]; mode: 'move' | 'copy' }>({ paths: [], mode: 'copy' })

// 粘贴落地为复制时算新建，为移动时算修改，权限要求随剪贴板模式变
const canPaste = computed(() => (clipboard.mode === 'copy' ? canCreate.value : canUpdate.value))

function mark(mode: 'move' | 'copy') {
  clipboard.paths = selected.value.map((it) => it.path)
  clipboard.mode = mode
  Message.info(t('file.marked', { count: clipboard.paths.length }))
}

async function paste() {
  if (!clipboard.paths.length) return
  try {
    const fn = clipboard.mode === 'move' ? moveEntries : copyEntries
    reportBatch(await fn(clipboard.paths, cwd.value, 'rename'))
    if (clipboard.mode === 'move') clipboard.paths = []
    await load()
  } catch (e) {
    reportError(e)
  }
}

async function unpack(entry: FileEntry) {
  try {
    // 默认解压到同名目录，避免污染当前目录
    const dir = entry.name.replace(/\.(zip|tar\.gz|tgz|tar\.bz2|tar)$/i, '')
    await decompressEntry(entry.path, `${cwd.value}/${dir || 'unpacked'}`)
    Message.success(t('file.done'))
    await load()
  } catch (e) {
    reportError(e)
  }
}

function isArchive(entry: FileEntry) {
  return !entry.isDir && /\.(zip|tar\.gz|tgz|tar\.bz2|tar)$/i.test(entry.name)
}

/* mtime 是后端返回的 Unix 毫秒（model.FileEntry.Mtime），不能再乘 1000。 */
function humanTime(ms: number) {
  if (!ms) return '-'
  return dayjs(ms).format('YYYY-MM-DD HH:mm:ss')
}

onMounted(async () => {
  if (!canRead.value) {
    booting.value = false
    return
  }
  try {
    const res = await fetchRoots()
    roots.value = res.allowRoots
    if (roots.value.length) await load(roots.value[0])
  } catch (e) {
    bootError.value = e instanceof BizError ? errorText(e.code, e.message) : t('common.requestFailed')
  } finally {
    booting.value = false
  }
})
</script>

<template>
  <a-card :bordered="false" :title="$t('menu.file')">
    <template #extra>
      <a-space>
        <a-select
          v-if="roots.length"
          v-model="cwd"
          :style="{ width: '200px' }"
          :aria-label="$t('file.rootSelect')"
          @change="(v) => load(String(v))"
        >
          <a-option v-for="r in roots" :key="r" :value="r">{{ r }}</a-option>
        </a-select>
        <a-input-search
          v-if="roots.length"
          v-model="keyword"
          allow-clear
          :placeholder="$t('file.searchPlaceholder')"
          :style="{ width: '220px' }"
          @search="doSearch"
          @clear="load()"
        />
        <a-button v-if="roots.length" :loading="loading" @click="load()">
          <template #icon><icon-refresh /></template>
        </a-button>
      </a-space>
    </template>

    <!-- 首屏四种情形各自有明确提示：加载中、无权限、接口失败、未配置白名单根目录 -->
    <a-skeleton v-if="booting" animation>
      <a-skeleton-line :rows="5" />
    </a-skeleton>
    <a-alert v-else-if="!canRead" type="info">{{ $t('file.noPermission') }}</a-alert>
    <a-alert v-else-if="bootError" type="error">{{ bootError }}</a-alert>
    <a-alert v-else-if="!roots.length" type="warning">{{ $t('file.notConfigured') }}</a-alert>

    <template v-else>
      <a-space wrap :style="{ marginBottom: '12px' }">
      <a-breadcrumb v-if="!searching">
        <a-breadcrumb-item v-for="c in crumbs" :key="c.path">
          <a-link @click="load(c.path)">{{ c.label }}</a-link>
        </a-breadcrumb-item>
      </a-breadcrumb>
      <a-tag v-else color="arcoblue">{{ $t('file.searchResult') }}</a-tag>
    </a-space>

    <a-space wrap :style="{ marginBottom: '12px' }">
      <a-button
        v-if="data?.parent"
        :disabled="searching"
        size="small"
        @click="load(data.parent)"
      >
        <template #icon><icon-arrow-up /></template>
        {{ $t('file.up') }}
      </a-button>
      <a-button v-if="canCreate" size="small" :disabled="searching" @click="openDialog('dir')">
        {{ $t('file.newDir') }}
      </a-button>
      <a-button v-if="canCreate" size="small" :disabled="searching" @click="openDialog('file')">
        {{ $t('file.newFile') }}
      </a-button>
      <a-button
        v-if="canCreate"
        size="small"
        :disabled="searching || uploading"
        :loading="uploading"
        :title="uploading ? uploadName : ''"
        @click="fileInput?.click()"
      >
        {{ uploading ? `${uploadPercent}%` : $t('file.upload') }}
      </a-button>
      <a-button v-if="canCreate" size="small" :disabled="!selected.length" @click="mark('copy')">
        {{ $t('file.copy') }}
      </a-button>
      <a-button v-if="canUpdate" size="small" :disabled="!selected.length" @click="mark('move')">
        {{ $t('file.move') }}
      </a-button>
      <a-button
        v-if="canPaste"
        size="small"
        :disabled="!clipboard.paths.length || searching"
        @click="paste"
      >
        {{ $t('file.paste', { count: clipboard.paths.length }) }}
      </a-button>
      <a-button v-if="canExec" size="small" :disabled="!selected.length" @click="openDialog('compress')">
        {{ $t('file.compress') }}
      </a-button>
      <a-button
        v-if="canDelete"
        size="small"
        status="danger"
        :disabled="!selected.length"
        @click="remove(selected)"
      >
        {{ $t('file.delete') }}
      </a-button>
      <a-checkbox v-model="showHidden" @change="load()">{{ $t('file.showHidden') }}</a-checkbox>
    </a-space>

    <!-- 隐藏的原生 input：Arco 的 Upload 会自行发请求，这里需要走带鉴权头的封装 -->
    <input
      v-if="canCreate"
      ref="fileInput"
      type="file"
      multiple
      hidden
      :aria-label="$t('file.upload')"
      @change="onPickFiles"
    />

    <a-table
      v-model:selected-keys="selectedKeys"
      :data="items"
      :loading="loading"
      :pagination="false"
      :row-selection="{ type: 'checkbox', showCheckedAll: true }"
      row-key="path"
      :scroll="{ y: 460 }"
    >
      <template #columns>
        <a-table-column :title="$t('file.name')" data-index="name">
          <template #cell="{ record }">
            <a-link @click="open(record)">
              <component :is="record.isDir ? 'icon-folder' : 'icon-file'" />
              {{ record.name }}
            </a-link>
            <a-tag v-if="record.isSymlink" size="small" :style="{ marginLeft: '6px' }">
              {{ $t('file.symlink') }}
            </a-tag>
          </template>
        </a-table-column>
        <a-table-column :title="$t('file.size')" :width="110">
          <template #cell="{ record }">
            {{ record.isDir ? '-' : humanSize(record.size) }}
          </template>
        </a-table-column>
        <a-table-column :title="$t('file.mode')" data-index="modeOctal" :width="90" />
        <a-table-column :title="$t('file.owner')" :width="140">
          <template #cell="{ record }">{{ record.owner }}:{{ record.group }}</template>
        </a-table-column>
        <a-table-column :title="$t('file.mtime')" :width="180">
          <template #cell="{ record }">{{ humanTime(record.mtime) }}</template>
        </a-table-column>
        <a-table-column :title="$t('file.actions')" :width="240">
          <template #cell="{ record }">
            <a-space>
              <a-link v-if="!record.isDir && canRead" @click="download(record)">{{ $t('file.download') }}</a-link>
              <a-link v-if="isArchive(record) && canExec" @click="unpack(record)">{{ $t('file.unpack') }}</a-link>
              <a-link v-if="canUpdate" @click="openDialog('rename', record)">{{ $t('file.rename') }}</a-link>
              <a-link v-if="canChmod" @click="openDialog('permission', record)">{{ $t('file.permission') }}</a-link>
              <a-link v-if="canDelete" status="danger" @click="remove([record])">{{ $t('file.delete') }}</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>
    </template>

    <a-modal v-model:visible="dialog.visible" :title="dialogTitle" @ok="submitDialog">
      <a-form :model="dialog" layout="vertical">
        <a-form-item v-if="dialog.kind !== 'permission'" :label="$t('file.nameLabel')">
          <a-input v-model="dialog.name" allow-clear />
        </a-form-item>
        <a-form-item v-if="dialog.kind === 'compress'" :label="$t('file.format')">
          <a-select v-model="dialog.format">
            <a-option value="zip">zip</a-option>
            <a-option value="tar.gz">tar.gz</a-option>
            <a-option value="tar">tar</a-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="dialog.kind === 'permission'" :label="$t('file.mode')">
          <a-input v-model="dialog.mode" placeholder="0644" allow-clear />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-drawer
      v-model:visible="editor.visible"
      :width="720"
      :title="editor.path"
      :ok-text="$t('file.save')"
      :ok-loading="editor.saving"
      @ok="save"
    >
      <a-space :style="{ marginBottom: '8px' }">
        <a-checkbox v-model="editor.backup">{{ $t('file.createBackup') }}</a-checkbox>
      </a-space>
      <a-textarea
        v-model="editor.content"
        :auto-size="{ minRows: 22, maxRows: 40 }"
        :aria-label="editor.path"
        spellcheck="false"
        class="file-editor"
      />
    </a-drawer>
  </a-card>
</template>

<style scoped>
.file-editor :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
  line-height: 1.6;
}
</style>
