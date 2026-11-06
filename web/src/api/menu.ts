import request from '@/utils/request'
import type { MenuInfo } from './auth'

export function listMenuTree() {
  return request.get<any, { list: MenuInfo[] }>('/menu/tree')
}

export interface MenuForm {
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
  affix: boolean
  hideTab: boolean
  apiPath: string
  apiMethod: string
}

export function createMenu(data: MenuForm) {
  return request.post<any, null>('/menu', data)
}

export function updateMenu(id: number, data: MenuForm) {
  return request.put<any, null>('/menu/' + id, data)
}

export function deleteMenu(id: number) {
  return request.delete<any, null>('/menu/' + id)
}
