// 请求 ID 供后端 access 日志串联，格式需通过后端 idgen.IsRequestID 校验：
// ^[A-Za-z0-9._:-]{8,64}$。这里生成 32 位十六进制串。
export function genRequestId(): string {
  const bytes = new Uint8Array(16)
  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(bytes)
  } else {
    for (let i = 0; i < bytes.length; i += 1) bytes[i] = Math.floor(Math.random() * 256)
  }
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}
