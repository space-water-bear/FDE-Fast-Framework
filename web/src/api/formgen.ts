import request from '@/utils/request'
import type { FormSchema, XUi } from '@/components/form-render/types'

export interface FormgenObjectItem {
  id: number
  key: string
  name: string
  tableName: string
  remark: string
  version: number
  createdAt: string
}

export interface FormgenFieldInput {
  name: string
  title?: string
  type: 'string' | 'text' | 'int' | 'float' | 'bool' | 'datetime' | 'enum' | 'relation'
  required?: boolean
  rules?: Record<string, unknown>
  'x-ui'?: XUi
}

export interface FormgenRelationInput {
  field: string
  targetObject: string
  kind: 'many_to_one' | 'one_to_many'
}

// 与设计文档 6.1 generate_form_schema 参数一致
export interface FormgenGenerateRequest {
  objectKey: string
  name: string
  fields: FormgenFieldInput[]
  relations?: FormgenRelationInput[]
  mode?: 'create' | 'update'
  dryRun?: boolean
}

export type FormgenObjectPayload = FormgenGenerateRequest
export type FormgenPreviewRequest = FormgenGenerateRequest

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
  return request.post<any, null>('/formgen/objects', data, { silent404: true } as any)
}

export function updateFormgenObject(objectKey: string, data: FormgenObjectPayload) {
  return request.put<any, null>('/formgen/objects/' + encodeURIComponent(objectKey), data, {
    silent404: true,
  } as any)
}

export function getFormgenSchema(objectKey: string) {
  return request.get<any, FormSchema>('/formgen/schema/' + encodeURIComponent(objectKey))
}

export function previewFormgenSchema(data: FormgenPreviewRequest) {
  return request.post<any, FormgenPreviewResult>('/formgen/schema/preview', data)
}
