import request from '@/utils/request'

export interface AiProviderItem {
  id: number
  name: string
  baseUrl: string
  apiKey: string
  apiMode: string
  model: string
  models: string[]
  temperature: number
  enabled: boolean
  remark: string
  createdAt: string
}

export interface AiAgentItem {
  id: number
  name: string
  providerId: number
  systemPrompt: string
  temperature: number
  enabled: boolean
  remark: string
  skills?: AiSkillItem[]
  tools?: AiToolItem[]
  mcpServers?: AiMcpItem[]
}

export interface AiSkillItem {
  id: number
  name: string
  description: string
  prompt: string
  remark: string
}

export interface AiToolItem {
  id: number
  name: string
  description: string
  schema: string
  enabled: boolean
  remark: string
}

export interface AiMcpItem {
  id: number
  name: string
  transport: string
  url: string
  command: string
  args: string
  headers: string
  enabled: boolean
  remark: string
}

export interface AiSessionItem {
  id: number
  title: string
  agentId: number
  userId: number
  createdAt: string
}

export interface AiMessageItem {
  id: number
  sessionId: number
  role: string
  content: string
  createdAt: string
}

export interface AiPageResult<T> {
  list: T[]
  total: number
}

export interface AiProviderForm {
  name: string
  baseUrl: string
  apiKey: string
  apiMode: string
  model: string
  temperature: number
  enabled: boolean
  remark: string
}

export interface AiAgentForm {
  name: string
  providerId: number
  systemPrompt: string
  temperature: number
  enabled: boolean
  remark: string
  skillIds: number[]
  toolIds: number[]
  mcpIds: number[]
}

export function listAiProviders(params?: { keyword?: string }) {
  return request.get<any, AiPageResult<AiProviderItem>>('/ai/provider/list', { params })
}
export function createAiProvider(data: AiProviderForm) {
  return request.post<any, null>('/ai/provider', data)
}
export function updateAiProvider(id: number, data: AiProviderForm) {
  return request.put<any, null>('/ai/provider/' + id, data)
}
export function deleteAiProvider(id: number) {
  return request.delete<any, null>('/ai/provider/' + id)
}

export function fetchAiProviderModels(id: number) {
  return request.post<any, { models: string[] }>('/ai/provider/' + id + '/fetch-models')
}

export function listAiAgents(params?: { keyword?: string }) {
  return request.get<any, AiPageResult<AiAgentItem>>('/ai/agent/list', { params })
}
export function createAiAgent(data: AiAgentForm) {
  return request.post<any, null>('/ai/agent', data)
}
export function updateAiAgent(id: number, data: AiAgentForm) {
  return request.put<any, null>('/ai/agent/' + id, data)
}
export function deleteAiAgent(id: number) {
  return request.delete<any, null>('/ai/agent/' + id)
}

export function listAiSkills(params?: { keyword?: string }) {
  return request.get<any, AiPageResult<AiSkillItem>>('/ai/skill/list', { params })
}
export function saveAiSkill(data: { id?: number; name: string; description: string; prompt: string; remark: string }) {
  if (data.id) return request.put<any, null>('/ai/skill/' + data.id, data)
  return request.post<any, null>('/ai/skill', data)
}
export function deleteAiSkill(id: number) {
  return request.delete<any, null>('/ai/skill/' + id)
}

export function listAiTools(params?: { keyword?: string }) {
  return request.get<any, AiPageResult<AiToolItem>>('/ai/tool/list', { params })
}
export function saveAiTool(data: { id?: number; name: string; description: string; schema: string; enabled: boolean; remark: string }) {
  if (data.id) return request.put<any, null>('/ai/tool/' + data.id, data)
  return request.post<any, null>('/ai/tool', data)
}
export function deleteAiTool(id: number) {
  return request.delete<any, null>('/ai/tool/' + id)
}

export function listAiMcpServers(params?: { keyword?: string }) {
  return request.get<any, AiPageResult<AiMcpItem>>('/ai/mcp/list', { params })
}
export function saveAiMcpServer(data: { id?: number; name: string; transport: string; url: string; command: string; args: string; headers: string; enabled: boolean; remark: string }) {
  if (data.id) return request.put<any, null>('/ai/mcp/' + data.id, data)
  return request.post<any, null>('/ai/mcp', data)
}
export function deleteAiMcpServer(id: number) {
  return request.delete<any, null>('/ai/mcp/' + id)
}

export function listAiSessions(params?: { keyword?: string; page?: number; pageSize?: number }) {
  return request.get<any, AiPageResult<AiSessionItem>>('/ai/session/list', { params })
}
export function createAiSession(data: { title?: string; agentId: number }) {
  return request.post<any, AiSessionItem>('/ai/session', data)
}
export function deleteAiSession(id: number) {
  return request.delete<any, null>('/ai/session/' + id)
}

export function renameAiSession(id: number, title: string) {
  return request.put<any, null>('/ai/session/' + id, { title })
}

export function clearAiSession(id: number) {
  return request.delete<any, null>('/ai/session/' + id + '/messages')
}

export function regenerateAiSession(id: number) {
  return request.post<any, AiMessageItem>('/ai/session/' + id + '/regenerate')
}

export function resendAiMessage(sessionId: number, messageId: number, content: string) {
  return request.post<any, AiMessageItem>('/ai/session/' + sessionId + '/messages/' + messageId + '/resend', { content })
}
export function listAiMessages(sessionId: number) {
  return request.get<any, { list: AiMessageItem[] }>('/ai/session/' + sessionId + '/messages')
}
export function chatAi(sessionId: number, content: string) {
  return request.post<any, AiMessageItem>('/ai/session/' + sessionId + '/chat', { content })
}

// 流式聊天(SSE), onDelta 逐段回调
export async function streamChatAi(
  sessionId: number,
  content: string,
  onDelta: (delta: string) => void,
  signal?: AbortSignal,
): Promise<void> {
  const token = localStorage.getItem('fde-token') || ''
  const res = await fetch('/api/ai/session/' + sessionId + '/chat/stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'x-token': token },
    body: JSON.stringify({ content }),
    signal,
  })
  if (!res.ok || !res.body) {
    throw new Error('流式请求失败')
  }
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() || ''
    for (const line of lines) {
      if (!line.startsWith('data: ')) continue
      const payload = line.slice(6).trim()
      if (payload === '[DONE]') return
      try {
        const obj = JSON.parse(payload)
        if (obj.error) throw new Error(obj.error)
        if (obj.delta) onDelta(obj.delta)
      } catch (e) {
        if (e instanceof SyntaxError) continue
        throw e
      }
    }
  }
}

export interface AiUsageItem {
  id: number
  sessionId: number
  agentId: number
  providerId: number
  model: string
  promptTokens: number
  completionTokens: number
  totalTokens: number
  latencyMs: number
  createdAt: string
}

export interface AiUsageGroup {
  id: number
  name: string
  calls: number
  tokens: number
}

export interface AiUsageSummary {
  totalCalls: number
  totalTokens: number
  totalLatencyMs: number
  byProvider: AiUsageGroup[]
  byAgent: AiUsageGroup[]
}

export function listAiUsages(params?: { page?: number; pageSize?: number; agentId?: number; providerId?: number }) {
  return request.get<any, AiPageResult<AiUsageItem>>('/ai/usage/list', { params })
}

export function getAiUsageSummary() {
  return request.get<any, AiUsageSummary>('/ai/usage/summary')
}
