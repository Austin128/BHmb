import { beforeEach, describe, expect, it, vi } from 'vitest'

import { uploadLargeFile } from '@/api/file'
import { request } from '@/utils/request'

vi.mock('@/utils/request', () => ({ request: vi.fn() }))

const mocked = vi.mocked(request)

/** entry 是 complete 与秒传都会回传的落地条目，字段只保留断言需要的部分。 */
const entry = { name: 'big.bin', path: '/www/big.bin', size: 24 } as never

function calls(url: string) {
  return mocked.mock.calls.filter((c) => String(c[0].url).startsWith(url)).map((c) => c[0])
}

/** 从 FormData 里取出分片序号，用于断言只补传了缺失的那几片。 */
function chunkIndexes() {
  return calls('/file/upload/chunk').map((cfg) => Number((cfg.data as FormData).get('index')))
}

describe('uploadLargeFile', () => {
  beforeEach(() => mocked.mockReset())

  it('秒传命中时不再投递分片', async () => {
    mocked.mockResolvedValueOnce({
      uploadId: 'up_1',
      chunkSize: 8,
      totalChunks: 3,
      uploadedChunks: [],
      expireAt: 0,
      quickUpload: true,
      entry,
    } as never)

    const percents: number[] = []
    const file = new File([new Uint8Array(24)], 'big.bin')
    await expect(uploadLargeFile('/www', file, 'reject', (p) => percents.push(p))).resolves.toBe(
      entry,
    )

    expect(calls('/file/upload/chunk')).toHaveLength(0)
    expect(calls('/file/upload/complete')).toHaveLength(0)
    expect(percents.at(-1)).toBe(100)
  })

  it('续传时跳过服务端已有的分片', async () => {
    mocked
      .mockResolvedValueOnce({
        uploadId: 'up_2',
        chunkSize: 8,
        totalChunks: 3,
        uploadedChunks: [0], // 第 0 片已在服务端
        expireAt: 0,
        quickUpload: false,
      } as never)
      .mockResolvedValue({ entry } as never)

    const file = new File([new Uint8Array(24)], 'big.bin')
    await uploadLargeFile('/www', file)

    expect(chunkIndexes()).toEqual([1, 2])
    expect(calls('/file/upload/complete')).toHaveLength(1)
  })

  it('init 请求带上目标目录、大小与冲突策略', async () => {
    mocked
      .mockResolvedValueOnce({
        uploadId: 'up_3',
        chunkSize: 16,
        totalChunks: 1,
        uploadedChunks: [],
        expireAt: 0,
        quickUpload: false,
      } as never)
      .mockResolvedValue({ entry } as never)

    await uploadLargeFile('/www', new File([new Uint8Array(16)], 'big.bin'), 'overwrite')

    const [init] = calls('/file/upload/init')
    expect(init.data).toMatchObject({
      path: '/www',
      filename: 'big.bin',
      size: 16,
      conflict: 'overwrite',
    })
  })
})
