import request from '@/utils/request'
import type { MenuInfo, RoleInfo } from './auth'

export interface RoleListItem extends RoleInfo {
  menus: MenuInfo[]
}

export function listRoles() {
  return request.get<any, { list: RoleListItem[] }>('/role/list')
}

export function getRole(id: number) {
  return request.get<any, { role: RoleInfo; menuIds: number[] }>('/role/' + id)
}

export interface RoleForm {
  name: string
  code?: string
  description: string
  enabled: boolean
  menuIds: number[]
}

export function createRole(data: RoleForm) {
  return request.post<any, null>('/role', data)
}

export function updateRole(id: number, data: RoleForm) {
  return request.put<any, null>('/role/' + id, data)
}

export function deleteRole(id: number) {
  return request.delete<any, null>('/role/' + id)
}
