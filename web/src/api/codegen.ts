import request from '@/utils/request'
import type { MenuInfo } from './auth'

export interface FieldTypeOption {
  value: string
  label: string
}

export interface GenFieldDef {
  name: string
  title: string
  type: string
  search: boolean
  inList: boolean
  inForm: boolean
  required: boolean
}

export interface GenRequest {
  tableName: string
  business: string
  structName: string
  parentMenuId: number
  fields: GenFieldDef[]
}

export interface GenFile {
  path: string
  content: string
}

export function getCodegenOptions() {
  return request.get<any, { fieldTypes: FieldTypeOption[]; parentMenus: MenuInfo[] }>('/codegen/options')
}

export function previewCode(data: GenRequest) {
  return request.post<any, { files: GenFile[] }>('/codegen/preview', data)
}

export function generateCode(data: GenRequest) {
  return request.post<any, { files: GenFile[]; menuId: number; restartRequired: boolean }>('/codegen/generate', data)
}
