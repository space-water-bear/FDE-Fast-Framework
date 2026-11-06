import request from '@/utils/request'

export interface UserInfo {
  id: number
  username: string
  nickname: string
  avatar: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface RoleInfo {
  id: number
  name: string
  code: string
  description: string
  enabled: boolean
}

export interface MenuInfo {
  id: number
  parentId: number
  path: string
  name: string
  component: string
  title: string
  icon: string
  type: number
  sort: number
  hidden: boolean
  keepAlive: boolean
  affix?: boolean
  hideTab?: boolean
  apiPath: string
  apiMethod: string
  children?: MenuInfo[]
}

export interface LoginResult {
  token: string
  refreshToken: string
  user: UserInfo
}

export function getCaptcha() {
  return request.get<any, { captchaEnabled: boolean; captchaId: string; picPath: string }>('/auth/captcha')
}

export function login(data: { username: string; password: string; captchaId?: string; captchaCode?: string }) {
  return request.post<any, LoginResult>('/auth/login', data)
}

export function refreshAccess(refreshToken: string) {
  return request.post<any, LoginResult>('/auth/refresh', { refreshToken })
}

export function getUserInfo() {
  return request.get<any, { user: UserInfo; roles: RoleInfo[]; menus: MenuInfo[] }>('/auth/userinfo')
}

export function logout(refreshToken?: string) {
  return request.post<any, null>('/auth/logout', { refreshToken: refreshToken || '' })
}

export interface DeviceItem {
  id: number
  userId: number
  username: string
  ip: string
  userAgent: string
  expiresAt: string
  createdAt: string
}

export function listDevices() {
  return request.get<any, { list: DeviceItem[] }>('/auth/devices')
}

export function kickDevice(id: number) {
  return request.delete<any, null>('/auth/devices/' + id)
}

export function kickOtherDevices() {
  return request.delete<any, null>('/auth/devices')
}
