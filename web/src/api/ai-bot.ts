import request from '@/utils/request'
import type { AiPageResult } from './ai'

export type AiBotPlatform = 'wecom' | 'dingtalk'

export interface AiBotConnection {
  state: string
  reconnectAttempt: number
  lastConnectedAt?: string
  lastHeartbeatAt?: string
}

export interface AiBotItem {
  id: number
  name: string
  platform: AiBotPlatform
  agentId: number
  enabled: boolean
  remark: string
  botId: string
  hasSecret: boolean
  clientId: string
  hasClientSecret: boolean
  connection: AiBotConnection
  createdAt: string
}

export interface AiBotForm {
  name: string
  platform: AiBotPlatform
  agentId: number
  enabled: boolean
  remark: string
  botId: string
  secret: string
  clientId: string
  clientSecret: string
}

export function listAiBots(params?: { keyword?: string }) {
  return request.get<any, AiPageResult<AiBotItem>>('/ai/bot/list', { params })
}

export function createAiBot(data: AiBotForm) {
  return request.post<any, AiBotItem>('/ai/bot', data)
}

export function updateAiBot(id: number, data: AiBotForm) {
  return request.put<any, AiBotItem>('/ai/bot/' + id, data)
}

export function deleteAiBot(id: number) {
  return request.delete<any, null>('/ai/bot/' + id)
}
