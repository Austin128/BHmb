// 后端认证相关实体类型，字段与 internal/model/dto_auth.go 一一对应。
export interface UserBrief {
  id: string
  username: string
  nickname: string
  avatar: string | null
  email?: string
  isSuper: boolean
  language: string
  theme: string
  mustChangePassword: boolean
  twoFABound: boolean
  lastLoginAt?: string
  lastLoginIp?: string
}

export interface RoleBrief {
  code: string
  name: string
  dataScope: string
}

export interface LoginPayload {
  username: string
  password: string
  remember?: boolean
  captchaId?: string
  captchaCode?: string
}

export interface LoginResult {
  accessToken: string
  expiresIn: number
  tokenType: string
  user: UserBrief
  roles: string[]
  permissions: string[]
}

/** 110009 的 data：登录一阶段票据，refreshToken 此时不下发。 */
export interface TwoFAChallenge {
  twoFAToken: string
  expiresIn: number
  methods: string[]
}

export interface TwoFAVerifyPayload {
  twoFAToken: string
  method: 'totp' | 'recovery'
  code: string
}

export interface RefreshResult {
  accessToken: string
  expiresIn: number
  tokenType: string
}

export interface ProfileResult {
  user: UserBrief
  roles: RoleBrief[]
  permissions: string[]
  nodeScope: string[]
  menus: string[]
}
