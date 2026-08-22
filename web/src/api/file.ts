import { ENV } from '@/config/env'
import { request } from '@/utils/request'
import { getAccessToken } from '@/utils/auth'

/* 类型与后端 internal/model/dto_file.go 一一对应。 */

export interface FileEntry {
  name: string
  path: string
  isDir: boolean
  size: number
  mode: string
  modeOctal: string
  uid: number
  gid: number
  owner: string
  group: string
  /** Unix 秒 */
  mtime: number
  linkTarget?: string
  linkBroken?: boolean
  isSymlink: boolean
  mime: string
  ext: string
  icon: string
  writable: boolean
}

export interface FileListResponse {
  path: string
  parent: string
  allowRoots?: string[]
  items: FileEntry[]
  total: number
  paged: boolean
  /** 目录过大时服务端降级返回，前端提示只展示部分条目 */
  degraded: boolean
  nextCursor?: string
}

export interface FileContentResponse {
  path: string
  content: string
  size: number
  etag: string
  mime: string
  truncated: boolean
}

export interface FileBatchItem {
  path: string
  dest?: string
  ok: boolean
  code?: number
  message?: string
}

export interface FileBatchResponse {
  succeeded: number
  failed: number
  results: FileBatchItem[]
}

export interface FileDuResponse {
  path: string
  size: number
  files: number
  dirs: number
  truncated: boolean
}

export type FileConflict = 'reject' | 'overwrite' | 'rename'

export interface FileListQuery {
  path: string
  keyword?: string
  showHidden?: boolean
  sort?: 'name' | 'size' | 'mtime' | 'type'
  order?: 'asc' | 'desc'
  pageSize?: number
  cursor?: string
}

export function fetchRoots() {
  return request<{ allowRoots: string[] }>({ url: '/file/roots', method: 'GET' })
}

export function fetchFileList(params: FileListQuery) {
  return request<FileListResponse>({ url: '/file/list', method: 'GET', params })
}

export function fetchFileStat(path: string) {
  return request<FileEntry>({ url: '/file/stat', method: 'GET', params: { path } })
}

export function fetchFileContent(path: string) {
  return request<FileContentResponse>({ url: '/file/content', method: 'GET', params: { path } })
}

export interface FileSaveResponse {
  path: string
  size: number
  etag: string
}

export function saveFileContent(body: {
  path: string
  content: string
  etag?: string
  createBackup?: boolean
}) {
  return request<FileSaveResponse>({ url: '/file/content', method: 'PUT', data: body })
}

export function createDir(path: string, mode?: string) {
  return request<FileEntry>({ url: '/file/dir', method: 'POST', data: { path, mode } })
}

export function createFile(path: string, mode?: string) {
  return request<FileEntry>({ url: '/file/file', method: 'POST', data: { path, mode } })
}

export function renameEntry(path: string, newName: string) {
  return request<FileEntry>({ url: '/file/rename', method: 'POST', data: { path, newName } })
}

export function moveEntries(paths: string[], dest: string, conflict?: FileConflict) {
  return request<FileBatchResponse>({ url: '/file/move', method: 'POST', data: { paths, dest, conflict } })
}

export function copyEntries(paths: string[], dest: string, conflict?: FileConflict) {
  return request<FileBatchResponse>({ url: '/file/copy', method: 'POST', data: { paths, dest, conflict } })
}

export function deleteEntries(paths: string[]) {
  return request<FileBatchResponse>({ url: '/file/items', method: 'DELETE', data: { paths } })
}

export function updatePermission(body: {
  path: string
  mode?: string
  owner?: string
  group?: string
  recursive?: boolean
}) {
  return request<FileEntry>({ url: '/file/permission', method: 'PUT', data: body })
}

export function compressEntries(body: {
  paths: string[]
  dest: string
  format?: 'zip' | 'tar.gz' | 'tar'
}) {
  return request<FileEntry>({ url: '/file/compress', method: 'POST', data: body })
}

export function decompressEntry(path: string, dest: string) {
  return request<FileEntry>({ url: '/file/decompress', method: 'POST', data: { path, dest } })
}

export function searchFiles(params: {
  path: string
  keyword: string
  maxDepth?: number
  limit?: number
}) {
  return request<{ items: FileEntry[]; total: number; truncated: boolean }>({
    url: '/file/search',
    method: 'GET',
    params,
  })
}

export function fetchDu(path: string) {
  return request<FileDuResponse>({ url: '/file/du', method: 'GET', params: { path } })
}

/** 上传单个文件。onProgress 回调用于展示百分比。 */
export function uploadFile(
  dir: string,
  file: File,
  conflict: FileConflict = 'reject',
  onProgress?: (percent: number) => void,
) {
  const form = new FormData()
  form.append('path', dir)
  form.append('conflict', conflict)
  form.append('file', file)
  return request<FileEntry>({
    url: '/file/upload',
    method: 'POST',
    data: form,
    // 交给浏览器生成 multipart 边界，不能沿用实例上的 JSON 头
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 0,
    onUploadProgress: (e) => {
      if (!onProgress || !e.total) return
      onProgress(Math.round((e.loaded / e.total) * 100))
    },
  })
}

/* ---------- 分片上传（docs/05 5.8.4） ---------- */

export interface FileUploadInitResponse {
  uploadId: string
  chunkSize: number
  totalChunks: number
  uploadedChunks: number[]
  /** RFC3339 时间，会话过期后需重新 init */
  expireAt: string
  /** 服务端已存在同名同哈希文件，无需再传分片 */
  quickUpload: boolean
  entry?: FileEntry
}

export interface FileUploadStatusResponse {
  uploadId: string
  path: string
  filename: string
  size: number
  chunkSize: number
  totalChunks: number
  uploadedChunks: number[]
  missingChunks: number[]
  expireAt: string
}

export interface FileUploadCompleteResponse {
  entry: FileEntry
  path: string
  size: number
  hash?: string
  durationMs: number
}

/** 超过该阈值的文件走分片上传，小文件仍用单请求上传以少两次往返。 */
export const CHUNK_UPLOAD_THRESHOLD = 16 * 1024 * 1024
/** 默认分片大小，与服务端默认值一致。 */
export const DEFAULT_CHUNK_SIZE = 8 * 1024 * 1024

export function initChunkUpload(body: {
  path: string
  filename: string
  size: number
  chunkSize?: number
  hash?: string
  conflict?: FileConflict
}) {
  return request<FileUploadInitResponse>({ url: '/file/upload/init', method: 'POST', data: body })
}

export function fetchUploadStatus(uploadId: string) {
  return request<FileUploadStatusResponse>({
    url: '/file/upload/status',
    method: 'GET',
    params: { uploadId },
  })
}

/** 投递单个分片。checksum 形如 sha256:<hex>，服务端据此拒收损坏分片。 */
export function uploadChunk(
  uploadId: string,
  index: number,
  blob: Blob,
  checksum?: string,
  onProgress?: (loaded: number) => void,
) {
  const form = new FormData()
  form.append('uploadId', uploadId)
  form.append('index', String(index))
  if (checksum) form.append('checksum', checksum)
  form.append('chunk', blob)
  return request<{ uploadedCount: number; totalChunks: number }>({
    url: '/file/upload/chunk',
    method: 'POST',
    data: form,
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 0,
    onUploadProgress: (e) => onProgress?.(e.loaded),
  })
}

export function completeChunkUpload(uploadId: string, hash?: string) {
  return request<FileUploadCompleteResponse>({
    url: '/file/upload/complete',
    method: 'POST',
    data: { uploadId, hash },
  })
}

export function abortChunkUpload(uploadId: string) {
  return request<Record<string, never>>({ url: `/file/upload/${uploadId}`, method: 'DELETE' })
}

/**
 * sha256Hex 用 WebCrypto 计算摘要。非安全上下文（纯 HTTP 访问）下 crypto.subtle
 * 不可用、旧浏览器缺 Blob.arrayBuffer 时返回空串：
 * 秒传与分片校验都只是优化项，缺了不影响上传本身。
 */
async function sha256Hex(data: Blob): Promise<string> {
  if (!globalThis.crypto?.subtle || typeof data.arrayBuffer !== 'function') return ''
  try {
    const buf = await crypto.subtle.digest('SHA-256', await data.arrayBuffer())
    return Array.from(new Uint8Array(buf))
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('')
  } catch {
    return ''
  }
}

/**
 * uploadLargeFile 走 init → chunk → complete 三步，并具备：
 * 断点续传（服务端复用未过期会话，已传分片直接跳过）、逐片校验、整文件哈希兜底。
 * onProgress 汇报的是整体百分比。
 */
export async function uploadLargeFile(
  dir: string,
  file: File,
  conflict: FileConflict = 'reject',
  onProgress?: (percent: number) => void,
): Promise<FileEntry> {
  const digest = await sha256Hex(file)
  const hash = digest ? `sha256:${digest}` : undefined

  const init = await initChunkUpload({
    path: dir,
    filename: file.name,
    size: file.size,
    chunkSize: DEFAULT_CHUNK_SIZE,
    hash,
    conflict,
  })
  // 秒传：服务端已有同名同哈希文件
  if (init.quickUpload && init.entry) {
    onProgress?.(100)
    return init.entry
  }

  const done = new Set(init.uploadedChunks)
  // 已在服务端的分片计入基线进度，续传时进度条不会从 0 开始
  let uploaded = done.size * init.chunkSize
  const report = (extra = 0) => {
    onProgress?.(Math.min(100, Math.round(((uploaded + extra) / file.size) * 100)))
  }
  report()

  for (let i = 0; i < init.totalChunks; i++) {
    if (done.has(i)) continue
    const start = i * init.chunkSize
    const blob = file.slice(start, Math.min(start + init.chunkSize, file.size))
    const sum = await sha256Hex(blob)
    await uploadChunk(
      init.uploadId,
      i,
      blob,
      sum ? `sha256:${sum}` : undefined,
      (loaded) => report(loaded),
    )
    uploaded += blob.size
    report()
  }

  const res = await completeChunkUpload(init.uploadId, hash)
  onProgress?.(100)
  return res.entry
}

/**
 * 触发下载。下载接口不走统一信封，且需要 Authorization 头，
 * 因此用 fetch 取 blob 后再触发浏览器保存，避免把令牌拼进 URL。
 */
export async function downloadFile(path: string, filename: string) {
  const url = `${ENV.apiBaseUrl}/file/download?path=${encodeURIComponent(path)}`
  const res = await fetch(url, {
    headers: { Authorization: `Bearer ${getAccessToken() ?? ''}` },
    credentials: 'include',
  })
  if (!res.ok) {
    // 失败时后端仍返回统一信封，交给调用方按 code 提示
    const body = (await res.json().catch(() => null)) as { code?: number; message?: string } | null
    throw new Error(body?.message || `HTTP ${res.status}`)
  }
  const blob = await res.blob()
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(link.href)
}
