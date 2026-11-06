import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'

const source = await readFile(new URL('../src/views/system/development/state.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 },
})
const { runLabel, canCancelRun, canBuildRelease, canUseRelease, isRunPending, mergeRunEvents,
  readCheckReport, prettyReport, readSavedSelection, safeHttpUrl, validateProject, validateRun } =
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
