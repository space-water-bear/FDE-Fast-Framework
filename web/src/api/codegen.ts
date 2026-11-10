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

export interface GenPreview {
  ok: boolean
  batchId: string
  files: GenFile[]
  diff?: { conflicts?: unknown[]; [key: string]: unknown }
  menuPlan?: unknown
  warnings?: string[]
  restartRequired?: boolean
  error?: { msg?: string; errCode?: string }
}

export interface GenApplyResult {
  ok: boolean
  applied?: { path: string; status: string }[]
  menuId?: number
  casbinRebuilt?: boolean
  rollback?: unknown
  error?: { msg?: string; errCode?: string }
}

export function getCodegenOptions() {
  return request.get<any, { fieldTypes: FieldTypeOption[]; parentMenus: MenuInfo[] }>('/codegen/options')
}

export function previewCode(data: GenRequest) {
  return request.post<any, GenPreview>('/codegen/preview', data)
}

export function generateCode(data: { batchId: string; approve: true }) {
  return request.post<any, GenApplyResult>('/codegen/generate', data)
}
