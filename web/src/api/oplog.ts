import request from '@/utils/request'

export interface OplogItem {
  id: number
  userId: number
  username: string
  method: string
  path: string
  query: string
  body: string
  ip: string
  userAgent: string
  status: number
  code: number
  msg: string
  latencyMs: number
  createdAt: string
}

export function listOplogs(params: { page: number; pageSize: number; keyword?: string }) {
  return request.get<any, { list: OplogItem[]; total: number }>('/oplog/list', { params })
}

export function deleteOplog(id: number) {
  return request.delete<any, null>('/oplog/' + id)
}

export function clearOplogs() {
  return request.delete<any, null>('/oplog/clear')
}
