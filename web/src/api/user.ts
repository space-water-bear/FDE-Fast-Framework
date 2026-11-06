import request from '@/utils/request'
import type { RoleInfo, UserInfo } from './auth'

export interface UserItem extends UserInfo {
  roles: RoleInfo[]
}

export interface PageResult<T> {
  list: T[]
  total: number
}

export function listUsers(params: { page: number; pageSize: number; keyword?: string }) {
  return request.get<any, PageResult<UserItem>>('/user/list', { params })
}

export interface UserForm {
  username?: string
  password?: string
  nickname: string
  enabled: boolean
  roleIds: number[]
}

export function createUser(data: UserForm) {
  return request.post<any, null>('/user', data)
}

export function updateUser(id: number, data: UserForm) {
  return request.put<any, null>('/user/' + id, data)
}

export function deleteUser(id: number) {
  return request.delete<any, null>('/user/' + id)
}
