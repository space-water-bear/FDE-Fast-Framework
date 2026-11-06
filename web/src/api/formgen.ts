import request from '@/utils/request'
import type { BusinessFieldType, FormRelation, FormSchema, LinkageRule, XUi } from '@/components/form-render/types'

export interface FormgenObjectItem {
  id: number
  key: string
  name: string
  tableName: string
  remark: string
  version: number
  activeVersion: number
  createdAt: string
}

export interface FormgenFieldInput {
  name: string
  title?: string
  type: BusinessFieldType
  required?: boolean
  rules?: Record<string, unknown>
  'x-ui'?: XUi
  searchable?: boolean
  inList?: boolean
  inForm?: boolean
  sort?: number
}

export type FormgenRelationInput = FormRelation

// 与设计文档 6.1 generate_form_schema 参数一致
export interface FormgenGenerateRequest {
  objectKey: string
  name: string
  fields: FormgenFieldInput[]
  relations?: FormgenRelationInput[]
  linkages?: LinkageRule[]
  schema?: FormSchema
  expectedVersion?: number
  mode?: 'create' | 'update'
  dryRun?: boolean
}

export type FormgenObjectPayload = FormgenGenerateRequest
export type FormgenPreviewRequest = FormgenGenerateRequest

export interface FormgenSavedObject {
  object: FormgenObjectItem
  schema: FormSchema
}

export interface FormgenPreviewResult {
  ok: boolean
  schema?: FormSchema
  warnings?: string[]
  diff?: {
    added?: string[]
    updated?: string[]
    modified?: number
    conflicts?: string[]
  }
}

export function listFormgenObjects(params: { page: number; pageSize: number; keyword?: string }) {
  return request.get<any, { list: FormgenObjectItem[]; total: number }>('/formgen/objects', { params })
}

export function createFormgenObject(data: FormgenObjectPayload) {
  return request.post<any, FormgenSavedObject>('/formgen/objects', data, { silent404: true } as any)
}

export function updateFormgenObject(objectKey: string, data: FormgenObjectPayload) {
  return request.put<any, FormgenSavedObject>('/formgen/objects/' + encodeURIComponent(objectKey), data, {
    silent404: true,
  } as any)
}

export function getFormgenSchema(objectKey: string, version: 'draft' | number | undefined = undefined) {
  return request.get<any, FormSchema>('/formgen/schema/' + encodeURIComponent(objectKey), { params: { version } })
}

export function activateFormgenSchema(objectKey: string, data: { version: number; expectedActiveVersion: number }) {
  return request.post<any, FormgenSavedObject>(
    '/formgen/objects/' + encodeURIComponent(objectKey) + '/activate', data,
  )
}

export function previewFormgenSchema(data: FormgenPreviewRequest) {
  return request.post<any, FormgenPreviewResult>('/formgen/schema/preview', data)
}
