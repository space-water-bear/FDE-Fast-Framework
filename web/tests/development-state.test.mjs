import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'

const source = await readFile(new URL('../src/views/system/development/state.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 },
})
const { canPromoteRun, canUpgradeTarget, canRollbackTarget, targetStatusLabel, runLabel, canCancelRun, canBuildRelease, canUseRelease, isRunPending, mergeRunEvents,
  readCheckReport, prettyReport, readSavedSelection, safeHttpUrl, isDeployedResult, validateProject, validateRun } =
  await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'))

test('queued/running/cancel-requested task states never imply successful completion', () => {
  assert.equal(runLabel({ state: 'queued', cancelRequested: false }), '排队中')
  assert.equal(runLabel({ state: 'running', cancelRequested: false }), '执行中')
  assert.equal(runLabel({ state: 'running', cancelRequested: true }), '正在取消')
  assert.equal(runLabel({ state: 'succeeded', cancelRequested: false }), '检查通过')
  assert.equal(runLabel({ state: 'interrupted', cancelRequested: false }), '执行中断')
  assert.equal(isRunPending({ state: 'queued' }), true)
  assert.equal(isRunPending({ state: 'interrupted' }), false)
  assert.equal(canCancelRun({ state: 'running', cancelRequested: false }), true)
  assert.equal(canCancelRun({ state: 'running', cancelRequested: true }), false)
  assert.equal(canCancelRun({ state: 'succeeded', cancelRequested: false }), false)
})

test('release construction requires a passing report for exactly the frozen source hash', () => {
  const run = { state: 'succeeded', sourceHash: 'source-a', cancelRequested: false }
  const report = { success: true, snapshotHash: 'source-a' }
  assert.equal(canBuildRelease(run, report), true)
  for (const state of ['queued', 'running', 'failed', 'canceled', 'interrupted']) {
    assert.equal(canBuildRelease({ ...run, state }, report), false)
  }
  assert.equal(canBuildRelease(run, null), false)
  assert.equal(canBuildRelease(run, { ...report, success: false }), false)
  assert.equal(canBuildRelease(run, { ...report, snapshotHash: 'old-source' }), false)
  assert.equal(canBuildRelease({ ...run, sourceHash: '' }, report), false)
  assert.equal(canBuildRelease({ ...run, cancelRequested: true }, report), false)
})

test('a stale, incomplete or unverified artifact cannot be previewed/deployed even if its task passed', () => {
  const run = { state: 'succeeded', sourceHash: 'source-a', cancelRequested: false }
  const report = { success: true, snapshotHash: 'source-a' }
  const release = { id: 'release-1', status: 'ready', sourceHash: 'source-a', artifactHash: 'artifact-a' }
  assert.equal(canUseRelease(run, report, release), true)
  for (const invalid of [null, { ...release, sourceHash: 'source-b' }, { ...release, artifactHash: '' },
    { ...release, id: '' }, { ...release, status: 'building' }, { ...release, status: 'verified' }, { ...release, status: 'failed' }, { ...release, status: 'unknown' }]) {
    assert.equal(canUseRelease(run, report, invalid), false)
  }
  assert.equal(canUseRelease({ ...run, state: 'queued' }, report, release), false)
})

test('polling durable events merges overlapping responses once, in server order, for the selected task only', () => {
  const event = (id, message, runId = 12) => ({ id, runId, kind: 'edit', message, createdAt: '2026-11-06T01:00:00Z' })
  const events = mergeRunEvents([event(2, 'old'), event(1, 'first')],
    [event(3, 'third'), event(2, 'updated'), event(9, 'other task', 99), event(-1, 'invalid')], 12)
  assert.deepEqual(events.map((item) => [item.id, item.message]), [[1, 'first'], [2, 'updated'], [3, 'third']])
  assert.deepEqual(mergeRunEvents(events, events, 12), events)
})

test('reports are read from persisted objects/JSON and malformed data never becomes passing checks', () => {
  const report = { success: true, snapshotHash: 'source-a', steps: [{ name: 'go-test', exitCode: 0 }] }
  assert.deepEqual(readCheckReport(JSON.stringify(report)), report)
  assert.deepEqual(readCheckReport(report), report)
  assert.equal(readCheckReport('{bad'), null)
  assert.equal(readCheckReport({ success: 'true', snapshotHash: 'source-a' }), null)
  assert.equal(readCheckReport({ success: true }), null)
  assert.equal(prettyReport('plain diagnostic'), 'plain diagnostic')
  assert.equal(prettyReport(null), '')
  assert.match(prettyReport('{"exitCode":1}'), /"exitCode": 1/)
})

test('reopening the workbench restores only a valid project/task reference, never a locally cached success state', () => {
  assert.deepEqual(readSavedSelection('{"projectId":7,"runKey":"run-1","state":"succeeded"}'), { projectId: 7, runKey: 'run-1' })
  assert.equal(readSavedSelection(null), null)
  assert.equal(readSavedSelection('{broken'), null)
  assert.equal(readSavedSelection('{"projectId":-1,"runKey":"x"}'), null)
  assert.equal(readSavedSelection('{"projectId":"7","runKey":"x"}'), null)
  assert.deepEqual(readSavedSelection('{"projectId":7,"runKey":false}'), { projectId: 7, runKey: '' })
})

test('preview and deployment links allow only explicit HTTP/HTTPS URLs', () => {
  assert.equal(safeHttpUrl('http://127.0.0.1:18080/'), 'http://127.0.0.1:18080/')
  assert.equal(safeHttpUrl('https://test.example/app'), 'https://test.example/app')
  assert.equal(safeHttpUrl('javascript:alert(1)'), '')
  assert.equal(safeHttpUrl('data:text/html,hello'), '')
  assert.equal(safeHttpUrl('/relative-url'), '')
})

test('preview/deployment success requires the exact requested release and target, not HTTP success alone', () => {
  const result = { status: 'deployed', releaseId: 'release-a', targetId: 'target-a' }
  assert.equal(isDeployedResult(result, 'release-a'), true)
  assert.equal(isDeployedResult(result, 'release-a', 'target-a'), true)
  assert.equal(isDeployedResult({ ...result, status: 'failed' }, 'release-a'), false)
  assert.equal(isDeployedResult({ ...result, status: 'ready' }, 'release-a'), false)
  assert.equal(isDeployedResult(result, 'release-b'), false)
  assert.equal(isDeployedResult(result, 'release-a', 'target-b'), false)
})

test('release downloads retain the server ZIP filename and safely handle encoded or invalid headers', async () => {
  const downloadSource = await readFile(new URL('../src/utils/download.ts', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(downloadSource, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText
  const { releaseDownloadName } = await import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64'))
  assert.equal(releaseDownloadName('attachment; filename="fde-release-1.zip"', 'r1'), 'fde-release-1.zip')
  assert.equal(releaseDownloadName("attachment; filename*=UTF-8''%E5%8F%91%E5%B8%83.zip", 'r1'), '发布.zip')
  assert.equal(releaseDownloadName('attachment; filename="../private/release.zip"', 'r1'), 'release.zip')
  assert.equal(releaseDownloadName(null, 'r1'), 'fde-release-r1.zip')
  assert.equal(releaseDownloadName('attachment; filename=".."', 'r1'), 'fde-release-r1.zip')
})

test('project/task inputs obey server naming and UTF-8 requirement limits', () => {
  assert.equal(validateProject('acme-repair', '客户维修管理'), '')
  assert.match(validateProject('ACME', '客户'), /小写/)
  assert.match(validateProject('../other', '客户'), /小写/)
  assert.match(validateProject('acme', '   '), /名称/)
  assert.equal(validateRun('新建维修登记', 1), '')
  assert.match(validateRun('需求', undefined), /智能体/)
  assert.match(validateRun('  ', 1), /需求/)
  assert.equal(validateRun('中'.repeat(10666), 1), '')
  assert.match(validateRun('中'.repeat(10667), 1), /过长/)
})


test('promotion requires the reviewed run and the exact current project baseline', () => {
  const project = { currentCommit: 'base-a', baselineRevision: 2 }
  const run = { state: 'succeeded', sourceHash: 'source-a', cancelRequested: false,
    baseCommit: 'base-a', baseProjectRevision: 2, promotedCommit: '' }
  const checks = { success: true, snapshotHash: 'source-a' }
  assert.equal(canPromoteRun(run, checks, project), true)
  assert.equal(canPromoteRun({ ...run, promotedCommit: 'promoted' }, checks, project), false)
  assert.equal(canPromoteRun({ ...run, state: 'running' }, checks, project), false)
  assert.equal(canPromoteRun(run, { ...checks, snapshotHash: 'old' }, project), false)
  assert.equal(canPromoteRun(run, checks, { ...project, currentCommit: 'new-base' }), false)
  assert.equal(canPromoteRun(run, checks, { ...project, baselineRevision: 3 }), false)
  assert.equal(canPromoteRun(run, checks, undefined), false)
  assert.equal(canPromoteRun({ ...run, baseProjectRevision: 0 }, checks, { ...project, baselineRevision: 0 }), true)
})

test('upgrade and rollback use observed target versions and refuse in-flight state', () => {
  const release = { id: 'v2', status: 'ready', artifactHash: 'hash-v2' }
  const deployed = { targetId: 'local-linux', status: 'deployed', currentReleaseId: 'v1', previousReleaseId: '', backupAvailable: false, operationActive: false }
  assert.equal(canUpgradeTarget(release, deployed), true)
  assert.equal(canUpgradeTarget({ ...release, id: 'v1' }, deployed), false)
  assert.equal(canUpgradeTarget({ ...release, artifactHash: '' }, deployed), false)
  assert.equal(canUpgradeTarget(release, { ...deployed, status: 'upgrading', operationActive: true }), false)
  assert.equal(canUpgradeTarget(release, { ...deployed, currentReleaseId: '' }), false)
  const recoverable = { ...deployed, currentReleaseId: 'v2', previousReleaseId: 'v1', backupAvailable: true }
  assert.equal(canUpgradeTarget(release, { ...recoverable, status: 'upgrade_failed', backupAvailable: false }), true)
  assert.equal(canUpgradeTarget({ ...release, id: 'v3' }, { ...recoverable, status: 'upgrade_failed' }), false)
  for (const status of ['deployed', 'upgrade_failed', 'rolling_back', 'rollback_failed']) assert.equal(canRollbackTarget({ ...recoverable, status }), true)
  for (const status of ['empty', 'upgrading']) assert.equal(canRollbackTarget({ ...recoverable, status }), false)
  assert.equal(canRollbackTarget({ ...recoverable, backupAvailable: false }), false)
  assert.equal(canRollbackTarget({ ...recoverable, previousReleaseId: '' }), false)
  assert.equal(canRollbackTarget(null), false)
  assert.equal(canRollbackTarget({ ...recoverable, status: 'rolling_back', operationActive: true }), false)
  assert.equal(canUpgradeTarget(release, { ...recoverable, status: 'upgrading', operationActive: false }), true)
  assert.equal(canUpgradeTarget(release, { ...recoverable, status: 'upgrading', operationActive: true }), false)
  assert.equal(targetStatusLabel('upgrade_failed'), '升级失败，等待恢复')
  assert.equal(targetStatusLabel('unknown'), '状态待确认')
  assert.equal(targetStatusLabel('upgrading', false), '升级中断，等待继续')
  assert.equal(targetStatusLabel('rolling_back', false), '回退中断，等待恢复')
})


test('P2 API calls bind explicit approval, actor project scope and the observed current release', async () => {
  const source = await readFile(new URL('../src/api/development.ts', import.meta.url), 'utf8')
  const isolated = source.replace("import request from '@/utils/request'", `const request = {
    get: async (url, options) => ({ url, options }),
    post: async (url, body, options) => ({ url, body, options }),
  }`).replace("import { releaseDownloadName } from '@/utils/download'", '')
  const compiled = ts.transpileModule(isolated, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText
  const api = await import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64'))
  const promotion = await api.promoteDevRun(7, 'checked-run')
  assert.equal(promotion.url, '/dev/runs/checked-run/promote')
  assert.deepEqual(promotion.body, { approve: true })
  assert.equal(promotion.options.params.projectId, 7)
  const upgrade = await api.upgradeDevRelease(7, 'v2', 'local-linux', 'v1')
  assert.equal(upgrade.url, '/dev/releases/v2/upgrade')
  assert.deepEqual(upgrade.body, { targetId: 'local-linux', expectedCurrentReleaseId: 'v1', approve: true })
  assert.equal(upgrade.options.params.projectId, 7)
  assert.equal(upgrade.options.timeout, 360_000)
  const rollback = await api.rollbackDevRelease(7, 'v2', 'local-linux', 'v2')
  assert.equal(rollback.url, '/dev/releases/v2/rollback')
  assert.equal(rollback.options.timeout, 360_000)
  assert.deepEqual(rollback.body, { targetId: 'local-linux', expectedCurrentReleaseId: 'v2', approve: true })
  const target = await api.getDevTarget(7, 'local-linux')
  assert.equal(target.url, '/dev/targets/local-linux')
  assert.equal(target.options.params.projectId, 7)
})
