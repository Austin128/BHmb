// accessToken 只存内存 + localStorage（15 分钟有效期，泄露窗口有限）。
// refreshToken 由后端写入 HttpOnly Cookie，前端 JS 既不可读也不得复制到 storage。
const TOKEN_KEY = 'nova:token'

let memoryToken = ''

export function getAccessToken(): string {
  if (memoryToken) return memoryToken
  try {
    memoryToken = window.localStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    memoryToken = ''
  }
  return memoryToken
}

export function setAccessToken(token: string): void {
  memoryToken = token
  try {
    window.localStorage.setItem(TOKEN_KEY, token)
  } catch {
    // 隐私模式下 localStorage 不可写：退化为仅内存态，刷新页面需重登
  }
}

export function clearAuth(): void {
  memoryToken = ''
  try {
    window.localStorage.removeItem(TOKEN_KEY)
  } catch {
    // 同上，忽略存储异常
  }
}
