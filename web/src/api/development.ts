import request from '@/utils/request'
import { releaseDownloadName } from '@/utils/download'

export interface DevProject {
  id: number
  key: string
  name: string
  ownerId: number
  sourceCommit: string
  currentCommit: string
  baselineHash: string
  baselineRevision: number
  lastPromotedRunId: number
  createdAt: string
}

export interface DevRun {
  id: number
  key: string
  projectId: number
  actorId: number
  agentId: number
  requirement: string
  baseCommit: string
  baseProjectRevision?: number
  promotedCommit?: string
  promotedAt?: string | null
  workspaceBaseCommit?: string
  state: string
  phase: string
  attempts: number
  cancelRequested: boolean
  diff?: string
  sourceHash?: string
  error?: string
  createdAt: string
  updatedAt: string
  startedAt?: string | null
  finishedAt?: string | null
}

export interface DevRunEvent {
  id: number
  runId: number
  kind: string
  message: string
  createdAt: string
}

export interface DevRelease {
  id: string
  status: string
  sourceHash: string
  artifactHash: string
  manifest?: unknown
  downloadUrl?: string
}

export interface DevRunDetail {
  run: DevRun
  events: DevRunEvent[]
  checks?: unknown
  result?: unknown
  release?: DevRelease | null
}

export interface DevCheckReport {
  success: boolean
  snapshotHash: string
  profile?: string
  failedStep?: string
  error?: string
  steps?: { name: string; exitCode: number; logPath?: string; truncated?: boolean; durationMs?: number }[]
  [key: string]: unknown
}

export interface DevTarget {
  id: string
  name: string
  kind: string
}

export interface DevTargetStatus {
  targetId: string
  status: string
  currentReleaseId: string
  previousReleaseId: string
  url: string
  backupAvailable: boolean
  operationActive: boolean
}

export interface DevDeployment {
  status: string
  releaseId: string
  targetId: string
  url?: string
  credentials?: DevCredentials
}

export interface DevCredentials {
  username: string
  password: string
}

export interface DevPreview {
  url: string
  status: string
  releaseId: string
  credentials?: DevCredentials
}

function scope(projectId: number) { return { params: { projectId } } }

export function listDevProjects() {
  return request.get<any, { list: DevProject[] }>('/dev/projects')
}

export function createDevProject(data: { key: string; name: string }) {
  return request.post<any, DevProject>('/dev/projects', data, { timeout: 120_000 })
}

export function listDevRuns(projectId: number, page = 1, pageSize = 20) {
  return request.get<any, { list: DevRun[]; total: number }>('/dev/runs', { params: { projectId, page, pageSize } })
}

export function createDevRun(projectId: number, data: { requirement: string; agentId: number }) {
  return request.post<any, DevRun>('/dev/runs', data, scope(projectId))
}

export function getDevRun(projectId: number, key: string) {
  return request.get<any, DevRunDetail>('/dev/runs/' + encodeURIComponent(key), scope(projectId))
}

export function promoteDevRun(projectId: number, key: string) {
  return request.post<any, DevProject>('/dev/runs/' + encodeURIComponent(key) + '/promote',
    { approve: true }, { ...scope(projectId), timeout: 120_000 })
}

export function cancelDevRun(projectId: number, key: string) {
  return request.post<any, null>('/dev/runs/' + encodeURIComponent(key) + '/cancel', {}, scope(projectId))
}

export function buildDevRelease(projectId: number, key: string) {
  return request.post<any, DevRelease>('/dev/runs/' + encodeURIComponent(key) + '/release', {}, {
    ...scope(projectId), timeout: 120_000,
  })
}

export function previewDevRelease(projectId: number, releaseId: string) {
  return request.post<any, DevPreview>(
    '/dev/releases/' + encodeURIComponent(releaseId) + '/preview', {}, { ...scope(projectId), timeout: 120_000 },
  )
}

export function listDevTargets(projectId: number) {
  return request.get<any, { list: DevTarget[] }>('/dev/targets', scope(projectId))
}

export function getDevTarget(projectId: number, targetId: string) {
  return request.get<any, DevTargetStatus>('/dev/targets/' + encodeURIComponent(targetId), scope(projectId))
}

export function upgradeDevRelease(projectId: number, releaseId: string, targetId: string, expectedCurrentReleaseId: string) {
  return request.post<any, DevDeployment>('/dev/releases/' + encodeURIComponent(releaseId) + '/upgrade',
    { targetId, expectedCurrentReleaseId, approve: true }, { ...scope(projectId), timeout: 360_000 })
}

export function rollbackDevRelease(projectId: number, releaseId: string, targetId: string, expectedCurrentReleaseId: string) {
  return request.post<any, DevDeployment>('/dev/releases/' + encodeURIComponent(releaseId) + '/rollback',
    { targetId, expectedCurrentReleaseId, approve: true }, { ...scope(projectId), timeout: 360_000 })
}

export function deployDevRelease(projectId: number, releaseId: string, targetId: string) {
  return request.post<any, DevDeployment>('/dev/releases/' + encodeURIComponent(releaseId) + '/deploy',
    { targetId, approve: true }, { ...scope(projectId), timeout: 120_000 })
}

export async function downloadDevRelease(projectId: number, releaseId: string): Promise<void> {
  const token = localStorage.getItem('fde-token') || ''
  const response = await fetch('/api/dev/releases/' + encodeURIComponent(releaseId)
    + '/download?projectId=' + encodeURIComponent(projectId), { headers: { 'x-token': token } })
  if (!response.ok || response.headers.get('content-type')?.includes('application/json')) {
    const body = await response.json().catch(() => null)
    throw new Error(body?.msg || '发布包下载失败，请刷新登录状态后重试')
  }
  const url = URL.createObjectURL(await response.blob())
  const link = document.createElement('a')
  link.href = url
  link.download = releaseDownloadName(response.headers.get('content-disposition'), releaseId)
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
