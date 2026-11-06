import request from '@/utils/request'

export interface DictTypeItem {
  id: number
  name: string
  code: string
  remark: string
}

export interface DictDataItem {
  id: number
  typeId: number
  label: string
  value: string
  sort: number
  enabled: boolean
  remark: string
}

export interface DictTypeForm {
  name: string
  code: string
  remark: string
}

export interface DictDataForm {
  typeId: number
  label: string
  value: string
  sort: number
  enabled: boolean
  remark: string
}

export function listDictTypes() {
  return request.get<any, { list: DictTypeItem[] }>('/dict/types')
}

export function createDictType(data: DictTypeForm) {
  return request.post<any, null>('/dict/type', data)
}

export function updateDictType(id: number, data: DictTypeForm) {
  return request.put<any, null>('/dict/type/' + id, data)
}

export function deleteDictType(id: number) {
  return request.delete<any, null>('/dict/type/' + id)
}

export function listDictData(typeId: number) {
  return request.get<any, { list: DictDataItem[] }>('/dict/data/list', { params: { typeId } })
}

export function createDictData(data: DictDataForm) {
  return request.post<any, null>('/dict/data', data)
}

export function updateDictData(id: number, data: DictDataForm) {
  return request.put<any, null>('/dict/data/' + id, data)
}

export function deleteDictData(id: number) {
  return request.delete<any, null>('/dict/data/' + id)
}
