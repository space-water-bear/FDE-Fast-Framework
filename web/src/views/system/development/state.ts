import type { DevCheckReport, DevProject, DevRelease, DevRun, DevRunEvent, DevTargetStatus } from '@/api/development'

const RUN_LABELS: Record<string, string> = {
  queued: '排队中', running: '执行中', succeeded: '检查通过', failed: '执行失败',
  canceled: '已取消', interrupted: '执行中断',
}
const PHASE_LABELS: Record<string, string> = { plan: '分析需求', edit: '修改代码', verify: '测试构建', review: '待审查' }

export function runLabel(run: Pick<DevRun, 'state' | 'cancelRequested'>): string {
  return run.cancelRequested && (run.state === 'queued' || run.state === 'running')
    ? '正在取消' : RUN_LABELS[run.state] ?? run.state
}

export function phaseLabel(phase: string): string { return PHASE_LABELS[phase] ?? phase }

export function isRunPending(run: Pick<DevRun, 'state'>): boolean {
  return run.state === 'queued' || run.state === 'running'
}

export function canCancelRun(run: Pick<DevRun, 'state' | 'cancelRequested'>): boolean {
  return isRunPending(run) && !run.cancelRequested
}

export function readCheckReport(value: unknown): DevCheckReport | null {
  if (typeof value === 'string') {
    try { value = JSON.parse(value) } catch { return null }
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const report = value as Record<string, unknown>
  return typeof report.success === 'boolean' && typeof report.snapshotHash === 'string' ? report as DevCheckReport : null
}

export function canBuildRelease(run: Pick<DevRun, 'state' | 'sourceHash' | 'cancelRequested'>, checks: DevCheckReport | null): boolean {
  return run.state === 'succeeded' && !!run.sourceHash && !run.cancelRequested
    && checks?.success === true && checks.snapshotHash === run.sourceHash
}

export function canUseRelease(run: Pick<DevRun, 'state' | 'sourceHash' | 'cancelRequested'>, checks: DevCheckReport | null, release?: DevRelease | null): boolean {
  return canBuildRelease(run, checks) && !!release?.id && !!release.artifactHash && release.sourceHash === run.sourceHash
    && release.status === 'ready'
}

export function mergeRunEvents(previous: DevRunEvent[], incoming: DevRunEvent[], runId: number): DevRunEvent[] {
  const events = new Map<number, DevRunEvent>()
  for (const event of [...previous, ...incoming]) {
    if (event.runId === runId && Number.isInteger(event.id) && event.id > 0) events.set(event.id, event)
  }
  return [...events.values()].sort((a, b) => a.id - b.id)
}

export function prettyReport(value: unknown): string {
  if (value === null || value === undefined || value === '') return ''
  if (typeof value === 'string') {
    try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
  }
  return JSON.stringify(value, null, 2)
}

export function readSavedSelection(value: string | null): { projectId: number; runKey: string } | null {
  try {
    const selected = JSON.parse(value || '')
    if (!Number.isInteger(selected.projectId) || selected.projectId <= 0) return null
    return { projectId: selected.projectId, runKey: typeof selected.runKey === 'string' && selected.runKey.length <= 128 ? selected.runKey : '' }
  } catch { return null }
}

export function safeHttpUrl(value?: string): string {
  if (!value) return ''
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : ''
  } catch { return '' }
}

export function isDeployedResult(result: { status: string; releaseId: string; targetId?: string }, releaseId: string, targetId?: string): boolean {
  return result.status === 'deployed' && result.releaseId === releaseId
    && (targetId === undefined || result.targetId === targetId)
}

export function validateProject(key: string, name: string): string {
  if (!/^[a-z][a-z0-9-]{0,47}$/.test(key)) return '项目标识需以小写字母开头，最多 48 位，仅包含小写字母、数字和连字符'
  if (!name.trim() || Array.from(name.trim()).length > 128) return '请填写不超过 128 字的项目名称'
  return ''
}

export function validateRun(requirement: string, agentId?: number): string {
  if (!agentId || agentId <= 0) return '请选择已启用的智能体'
  if (!requirement.trim()) return '请填写业务需求与验收条件'
  if (new TextEncoder().encode(requirement.trim()).length > 32000) return '需求内容过长，请精简后提交'
  return ''
}


export function canPromoteRun(run: DevRun, checks: DevCheckReport | null, project?: DevProject): boolean {
  return canBuildRelease(run, checks) && !run.promotedCommit && !!project?.currentCommit
    && project.currentCommit === run.baseCommit
    && Number.isInteger(project.baselineRevision) && project.baselineRevision >= 0 && project.baselineRevision === run.baseProjectRevision
}

export function canUpgradeTarget(release?: Pick<DevRelease, 'id' | 'status' | 'artifactHash'> | null, target?: DevTargetStatus | null): boolean {
  if (!release?.id || release.status !== 'ready' || !release.artifactHash || !target?.currentReleaseId || target.operationActive !== false) return false
  if (target.status === 'deployed') return target.currentReleaseId !== release.id
  return ['upgrading', 'upgrade_failed'].includes(target.status) && target.currentReleaseId === release.id && !!target.previousReleaseId
}

export function canRollbackTarget(target?: DevTargetStatus | null): boolean {
  return !!target && target.operationActive === false && ['deployed', 'upgrade_failed', 'rolling_back', 'rollback_failed'].includes(target.status)
    && !!target.currentReleaseId && !!target.previousReleaseId && target.backupAvailable
}

export function targetStatusLabel(status: string, operationActive?: boolean): string {
  if (operationActive === false && status === 'upgrading') return '升级中断，等待继续'
  if (operationActive === false && status === 'rolling_back') return '回退中断，等待恢复'
  const labels: Record<string, string> = {
    empty: '尚未部署', deployed: '运行中', upgrading: '升级中',
    upgrade_failed: '升级失败，等待恢复', rolling_back: '回退中', rollback_failed: '回退失败，等待恢复',
  }
  return labels[status] ?? '状态待确认'
}
