<template>
  <div class="page development-page">
    <div class="page-toolbar">
      <span class="panel-title">开发工作台</span>
      <el-select :model-value="projectId" :disabled="!!action || creatingRun" placeholder="选择客户项目" class="project-select" @change="changeProject">
        <el-option v-for="project in projects" :key="project.id" :value="project.id" :label="project.name + ' · ' + project.key" />
      </el-select>
      <el-button :icon="Plus" :disabled="!!action || creatingRun" @click="projectDialog = true">新建项目</el-button>
      <div class="toolbar-spacer" />
      <el-button :icon="Refresh" :loading="refreshing" @click="refreshCurrent()">{{ pollingPaused ? '重新连接' : '刷新' }}</el-button>
    </div>

    <el-alert v-if="pollingPaused" type="warning" :closable="false" :title="refreshError + '；自动刷新已暂停，保留最后收到的状态。'" />
    <el-empty v-if="!projects.length && !initializing" description="先创建客户项目，生成独立源码工作区" />

    <template v-if="selectedProject">
      <p class="project-meta">{{ selectedProject.name }} · 源码基线 {{ selectedProject.sourceCommit ? selectedProject.sourceCommit.slice(0, 12) : '尚未配置，请新建独立客户项目' }}</p>
      <section class="work-card compose-card">
        <div class="section-heading"><h3>描述业务需求</h3><span>关闭页面后任务继续执行，重新打开可恢复查看。</span></div>
        <el-input v-model="requirement" type="textarea" :rows="4" :disabled="creatingRun" placeholder="说明业务对象、字段、规则和验收条件，例如：建立维修登记，登记号必填且唯一，支持查询和编辑……" />
        <div class="compose-actions">
          <el-select v-model="agentId" :disabled="creatingRun" placeholder="选择执行智能体" class="agent-select">
            <el-option v-for="agent in enabledAgents" :key="agent.id" :value="agent.id" :label="agent.name" />
          </el-select>
          <span v-if="!enabledAgents.length" class="muted">请先在 AI 助手中配置并启用智能体。</span>
          <div class="toolbar-spacer" />
          <el-button type="primary" :icon="MagicStick" :loading="creatingRun" :disabled="!agentId || !requirement.trim() || !!action" @click="startRun">创建开发任务</el-button>
        </div>
      </section>

      <div class="work-grid">
        <section class="work-card runs-card">
          <div class="section-heading"><h3>任务记录</h3><span>{{ total }} 条</span></div>
          <el-table :data="runs" row-key="key" highlight-current-row :current-row-key="selectedKey" size="small" @row-click="selectRun">
            <el-table-column label="需求" min-width="150" show-overflow-tooltip><template #default="{ row }">{{ row.requirement }}</template></el-table-column>
            <el-table-column label="状态" width="100"><template #default="{ row }"><el-tag :type="statusType(row.state)" size="small">{{ runLabel(row) }}</el-tag></template></el-table-column>
            <el-table-column label="创建时间" width="145"><template #default="{ row }">{{ displayTime(row.createdAt) }}</template></el-table-column>
          </el-table>
          <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="prev, pager, next" small @current-change="changePage" />
        </section>

        <section class="work-card detail-card" v-loading="detailLoading">
          <el-empty v-if="!detail" description="选择一条任务查看执行记录" :image-size="70" />
          <template v-else>
            <div class="section-heading">
              <h3>任务详情 <el-tag :type="statusType(detail.run.state)" size="small">{{ runLabel(detail.run) }}</el-tag></h3>
              <el-button v-if="canCancelRun(detail.run) || detail.run.cancelRequested" type="danger" plain size="small" :disabled="detail.run.cancelRequested || !!action" :loading="canceling" @click="cancelRun">{{ detail.run.cancelRequested ? '等待取消完成' : '取消任务' }}</el-button>
            </div>
            <div class="run-meta">{{ detail.run.key }} · {{ displayTime(detail.run.createdAt) }} · 执行 {{ detail.run.attempts || 0 }} 轮</div>
            <p class="requirement-text">{{ detail.run.requirement }}</p>
            <el-steps :active="activePhase" :process-status="detail.run.state === 'failed' ? 'error' : detail.run.state === 'queued' ? 'wait' : 'process'" finish-status="success" simple>
              <el-step v-for="phase in phases" :key="phase" :title="phaseLabel(phase)" />
            </el-steps>
            <el-alert v-if="detail.run.state === 'succeeded'" title="源码检查已通过，可审查差异并构建发布包；尚未部署交付。" type="success" :closable="false" class="detail-notice" />
            <el-alert v-if="detail.run.error" :title="detail.run.error" type="error" :closable="false" class="detail-notice" />
            <el-alert v-if="detail.run.cancelRequested && isRunPending(detail.run)" title="取消请求已保存，正在等待后台停止。" type="warning" :closable="false" class="detail-notice" />

            <el-tabs v-model="detailTab" class="detail-tabs">
              <el-tab-pane label="执行记录" name="events">
                <el-empty v-if="!events.length" description="暂无持久化事件" :image-size="50" />
                <ol v-else class="event-list">
                  <li v-for="event in events" :key="event.id"><div class="event-heading"><strong>{{ phaseLabel(event.kind) }}</strong><time>{{ displayTime(event.createdAt) }}</time></div><pre>{{ prettyReport(event.message) }}</pre></li>
                </ol>
              </el-tab-pane>
              <el-tab-pane label="源码差异" name="diff"><pre v-if="detail.run.diff" class="report-pre diff-pre">{{ detail.run.diff }}</pre><el-empty v-else description="尚无源码差异" :image-size="50" /></el-tab-pane>
              <el-tab-pane label="检查报告" name="checks">
                <template v-if="checks">
                  <el-alert :title="checks.success ? '所有规定检查已通过' : '检查未通过：' + (checks.failedStep || checks.error || '查看报告')" :type="checks.success ? 'success' : 'error'" :closable="false" />
                  <el-table :data="checks.steps ?? []" size="small"><el-table-column prop="name" label="检查步骤" min-width="140" /><el-table-column label="结果" width="90"><template #default="{ row }"><el-tag :type="row.exitCode === 0 ? 'success' : 'danger'" size="small">{{ row.exitCode === 0 ? '通过' : '退出码 ' + row.exitCode }}</el-tag></template></el-table-column><el-table-column label="耗时" width="95"><template #default="{ row }">{{ row.durationMs ?? 0 }} ms</template></el-table-column><el-table-column label="日志" min-width="120" show-overflow-tooltip><template #default="{ row }">{{ row.logPath || '无引用' }}{{ row.truncated ? '（已截断）' : '' }}</template></el-table-column></el-table>
                </template>
                <pre v-if="checkReport" class="report-pre">{{ checkReport }}</pre><el-empty v-else description="尚未收到检查报告" :image-size="50" />
              </el-tab-pane>
              <el-tab-pane label="执行结果" name="result"><pre v-if="resultReport" class="report-pre">{{ resultReport }}</pre><el-empty v-else description="尚无执行结果" :image-size="50" /></el-tab-pane>
            </el-tabs>

            <section class="delivery-section">
              <div class="section-heading"><h3>发布与验收</h3><el-button type="primary" plain size="small" :disabled="!buildAllowed || !!action" :loading="action === 'build'" @click="buildRelease">{{ detail.release ? '重新核验发布包' : '构建发布包' }}</el-button></div>
              <p v-if="!buildAllowed" class="muted">任务通过检查且报告对应当前源码版本后，才能构建发布包。</p>
              <el-alert v-if="actionError" :title="actionError" type="error" :closable="false" />
              <template v-if="detail.release">
                <dl class="release-meta"><dt>发布版本</dt><dd>{{ detail.release.id }}</dd><dt>状态</dt><dd>{{ detail.release.status }}</dd><dt>源码摘要</dt><dd>{{ detail.release.sourceHash }}</dd><dt>制品摘要</dt><dd>{{ detail.release.artifactHash || '尚未生成' }}</dd></dl>
                <el-alert v-if="!verifiedRelease" title="发布包尚未验证或与本任务源码版本不一致，不能预览或部署。" type="warning" :closable="false" />
                <div class="release-actions">
                  <el-button size="small" :disabled="!verifiedRelease || !!action" :loading="action === 'download'" @click="downloadRelease">下载发布包</el-button>
                  <el-button size="small" :disabled="!verifiedRelease || !!action" :loading="action === 'preview'" @click="previewRelease">启动预览</el-button>
                  <a v-if="previewUrl" :href="previewUrl" target="_blank" rel="noopener noreferrer">打开预览</a>
                  <el-button size="small" text @click="manifestVisible = true">查看发布清单</el-button>
                </div>
                <CredentialsPanel v-if="previewCredentials" title="预览应用" :credentials="previewCredentials" />
                <p v-else-if="previewUrl" class="muted">本次未返回新登录信息，请使用此前保存的账号。</p>
                <div class="release-actions">
                  <el-select v-model="targetId" placeholder="选择测试部署目标" size="small" :disabled="!!action" class="target-select"><el-option v-for="target in targets" :key="target.id" :value="target.id" :label="target.name + ' · ' + target.kind" /></el-select>
                  <el-button type="warning" size="small" :disabled="!verifiedRelease || !targetId || !!action" :loading="action === 'deploy'" @click="deployRelease">部署此发布版本</el-button>
                </div>
                <p v-if="!targets.length" class="muted">当前项目没有可用部署目标。</p>
                <el-alert v-if="deployment" :type="deployment.status === 'deployed' ? 'success' : 'info'" :closable="false" :title="'部署状态：' + deployment.status + '；版本 ' + deployment.releaseId + '；目标 ' + deployment.targetId" />
                <a v-if="deploymentUrl" :href="deploymentUrl" target="_blank" rel="noopener noreferrer">打开已部署应用</a>
                <CredentialsPanel v-if="deploymentCredentials" title="已部署应用" :credentials="deploymentCredentials" />
                <p v-else-if="deploymentUrl" class="muted">本次未返回新登录信息，请使用此前保存的账号。</p>
              </template>
            </section>
          </template>
        </section>
      </div>
    </template>

    <el-dialog v-model="projectDialog" title="创建客户项目" width="480px" :close-on-click-modal="!creatingProject" :close-on-press-escape="!creatingProject" :show-close="!creatingProject">
      <el-form label-width="90px" @submit.prevent="addProject">
        <el-form-item label="项目标识" required><el-input v-model="projectForm.key" :disabled="creatingProject" placeholder="如 acme-repair" maxlength="48" /></el-form-item>
        <el-form-item label="项目名称" required><el-input v-model="projectForm.name" :disabled="creatingProject" placeholder="如 客户维修管理" maxlength="128" /></el-form-item>
        <p class="muted">从已配置的框架版本创建独立工作区，客户项目的源码与运行数据分开保存。</p>
      </el-form>
      <template #footer><el-button :disabled="creatingProject" @click="projectDialog = false">取消</el-button><el-button type="primary" :loading="creatingProject" @click="addProject">创建项目</el-button></template>
    </el-dialog>
    <el-dialog v-model="manifestVisible" title="固定版本发布清单" width="760px"><pre class="report-pre">{{ prettyReport(detail?.release?.manifest) || '暂无发布清单' }}</pre></el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { MagicStick, Plus, Refresh } from '@element-plus/icons-vue'
import { listAiAgents } from '@/api/ai'
import type { AiAgentItem } from '@/api/ai'
import { buildDevRelease, cancelDevRun, createDevProject, createDevRun, deployDevRelease, downloadDevRelease, getDevRun, listDevProjects, listDevRuns, listDevTargets, previewDevRelease } from '@/api/development'
import type { DevCredentials, DevDeployment, DevProject, DevRun, DevRunDetail, DevRunEvent, DevTarget } from '@/api/development'
import { canBuildRelease, canCancelRun, canUseRelease, isDeployedResult, isRunPending, mergeRunEvents, phaseLabel, prettyReport, readCheckReport, readSavedSelection, runLabel, safeHttpUrl, validateProject, validateRun } from './state'
import CredentialsPanel from './CredentialsPanel.vue'

const SELECTION_KEY = 'fde-development-selection'
const phases = ['plan', 'edit', 'verify', 'review']
const projects = ref<DevProject[]>([])
const agents = ref<AiAgentItem[]>([])
const projectId = ref<number>()
const selectedProject = computed(() => projects.value.find((project) => project.id === projectId.value))
const enabledAgents = computed(() => agents.value.filter((agent) => agent.enabled))
const agentId = ref<number>()
const requirement = ref('')
const runs = ref<DevRun[]>([])
const selectedKey = ref('')
const detail = ref<DevRunDetail | null>(null)
const events = ref<DevRunEvent[]>([])
const total = ref(0)
const page = ref(1)
const detailTab = ref('events')
const initializing = ref(true)
const refreshing = ref(false)
const detailLoading = ref(false)
const pollingPaused = ref(false)
const refreshError = ref('')
const creatingRun = ref(false)
const canceling = ref(false)
const action = ref('')
const actionError = ref('')
const projectDialog = ref(false)
const creatingProject = ref(false)
const projectForm = reactive({ key: '', name: '' })
const manifestVisible = ref(false)
const targets = ref<DevTarget[]>([])
const targetId = ref('')
const previewUrl = ref('')
const previewCredentials = ref<DevCredentials | null>(null)
const deploymentCredentials = ref<DevCredentials | null>(null)
const deployment = ref<DevDeployment | null>(null)
const deploymentUrl = computed(() => safeHttpUrl(deployment.value?.url))
const checks = computed(() => readCheckReport(detail.value?.checks))
const buildAllowed = computed(() => !!detail.value && canBuildRelease(detail.value.run, checks.value))
const verifiedRelease = computed(() => !!detail.value && canUseRelease(detail.value.run, checks.value, detail.value.release))
const checkReport = computed(() => prettyReport(detail.value?.checks))
const resultReport = computed(() => prettyReport(detail.value?.result))
const activePhase = computed(() => detail.value?.run.state === 'succeeded' ? 4 : Math.max(0, phases.indexOf(detail.value?.run.phase ?? 'plan')))
let selectionRevision = 0
let detailRequest = 0
let listRequest = 0
let timer: ReturnType<typeof setTimeout> | undefined
let active = true
let disposed = false
let ready = false
const requirementDrafts = new Map<number, string>()

function statusType(state: string): 'success' | 'danger' | 'warning' | 'info' | '' {
  return state === 'succeeded' ? 'success' : state === 'failed' ? 'danger' : state === 'interrupted' ? 'warning'
    : state === 'running' ? '' : 'info'
}
function displayTime(value?: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}
function persistSelection(): void {
  try { localStorage.setItem(SELECTION_KEY, JSON.stringify({ projectId: projectId.value, runKey: selectedKey.value })) } catch { /* 服务端记录仍然可恢复。 */ }
}
function resetDetail(): void {
  detailRequest++
  detail.value = null
  events.value = []
  previewUrl.value = ''
  clearCredentials()
  actionError.value = ''
  deployment.value = null
  detailLoading.value = false
}
function clearCredentials(): void { previewCredentials.value = null; deploymentCredentials.value = null }
function pausePolling(error: unknown): void {
  pollingPaused.value = true
  refreshError.value = error instanceof Error ? error.message : '状态读取失败'
  stopTimer()
}
function stopTimer(): void { if (timer) clearTimeout(timer); timer = undefined }
function schedulePoll(): void {
  stopTimer()
  if (active && !disposed && ready && projectId.value && !pollingPaused.value) {
    timer = setTimeout(() => { void refreshCurrent(true) }, 2500)
  }
}

async function loadRunList(selectFirst = false): Promise<void> {
  if (!projectId.value) return
  const selectedProjectId = projectId.value
  const requestId = ++listRequest
  const data = await listDevRuns(selectedProjectId, page.value)
  if (disposed || projectId.value !== selectedProjectId || requestId !== listRequest) return
  runs.value = data.list ?? []
  total.value = data.total
  if (selectFirst && !selectedKey.value && runs.value.length) await selectRun(runs.value[0])
}

async function loadDetail(showLoading = false): Promise<void> {
  if (!projectId.value || !selectedKey.value) return
  const selectedProjectId = projectId.value
  const key = selectedKey.value
  const requestId = ++detailRequest
  if (showLoading) detailLoading.value = true
  try {
    const data = await getDevRun(selectedProjectId, key)
    if (disposed || projectId.value !== selectedProjectId || selectedKey.value !== key || requestId !== detailRequest) return
    detail.value = data
    events.value = mergeRunEvents(events.value, data.events ?? [], data.run.id)
  } finally { if (requestId === detailRequest) detailLoading.value = false }
}

async function selectRun(run: DevRun): Promise<void> {
  if (action.value || selectedKey.value === run.key) return
  selectedKey.value = run.key
  resetDetail()
  persistSelection()
  pollingPaused.value = false
  try { await loadDetail(true) } catch (error) { pausePolling(error) } finally { schedulePoll() }
}

async function changePage(): Promise<void> {
  try { await loadRunList() } catch (error) { pausePolling(error) }
}

async function changeProject(id: number, restoreKey = ''): Promise<void> {
  if (action.value || creatingRun.value) return
  const revision = ++selectionRevision
  if (projectId.value) requirementDrafts.set(projectId.value, requirement.value)
  requirement.value = requirementDrafts.get(id) ?? ''
  projectId.value = id
  selectedKey.value = restoreKey
  page.value = 1
  runs.value = []
  total.value = 0
  targets.value = []
  targetId.value = ''
  resetDetail()
  pollingPaused.value = false
  persistSelection()
  try {
    const results = await Promise.allSettled([loadRunList(!restoreKey), restoreKey ? loadDetail(true) : Promise.resolve(), listDevTargets(id)])
    if (disposed || revision !== selectionRevision) return
    for (const result of results) if (result.status === 'rejected') pausePolling(result.reason)
    const targetResult = results[2]
    if (targetResult.status === 'fulfilled' && targetResult.value) targets.value = targetResult.value.list ?? []
  } finally { schedulePoll() }
}

async function refreshCurrent(automatic = false): Promise<void> {
  if (refreshing.value || action.value) return
  const revision = selectionRevision
  const selectedProjectId = projectId.value
  refreshing.value = true
  pollingPaused.value = false
  try {
    if (!automatic) {
      const catalogResults = await Promise.allSettled([listDevProjects(), listAiAgents(), selectedProjectId ? listDevTargets(selectedProjectId) : Promise.resolve({ list: [] as DevTarget[] })])
      if (disposed || revision !== selectionRevision) return
      if (catalogResults[0].status === 'fulfilled') projects.value = catalogResults[0].value.list ?? []
      if (catalogResults[1].status === 'fulfilled') agents.value = catalogResults[1].value.list ?? []
      if (catalogResults[2].status === 'fulfilled') targets.value = catalogResults[2].value.list ?? []
      for (const result of catalogResults) if (result.status === 'rejected') pausePolling(result.reason)
      if (!projects.value.some((project) => project.id === selectedProjectId)) {
        const fallback = projects.value[0]
        if (fallback) await changeProject(fallback.id)
        else { projectId.value = undefined; selectedKey.value = ''; runs.value = []; total.value = 0; resetDetail() }
        return
      }
    }
    if (!projectId.value || disposed || revision !== selectionRevision) return
    const results = await Promise.allSettled([loadRunList(), loadDetail()])
    for (const result of results) if (result.status === 'rejected') pausePolling(result.reason)
  } finally { refreshing.value = false; schedulePoll() }
}

async function addProject(): Promise<void> {
  const message = validateProject(projectForm.key.trim(), projectForm.name)
  if (message) { ElMessage.warning(message); return }
  creatingProject.value = true
  try {
    const project = await createDevProject({ key: projectForm.key.trim(), name: projectForm.name.trim() })
    projects.value = [...projects.value.filter((item) => item.id !== project.id), project]
    projectDialog.value = false
    projectForm.key = ''; projectForm.name = ''
    await changeProject(project.id)
    ElMessage.success('独立客户项目已创建')
  } catch { /* 请求层展示错误，不自动重放创建。 */ } finally { creatingProject.value = false }
}

async function startRun(): Promise<void> {
  const message = validateRun(requirement.value, agentId.value)
  if (message) { ElMessage.warning(message); return }
  if (!projectId.value) return
  creatingRun.value = true
  try {
    const run = await createDevRun(projectId.value, { requirement: requirement.value.trim(), agentId: agentId.value! })
    selectedKey.value = run.key
    resetDetail()
    persistSelection()
    requirement.value = ''
    requirementDrafts.set(projectId.value, '')
    page.value = 1
    await Promise.all([loadRunList(), loadDetail(true)])
    ElMessage.success('任务已创建，实际执行进度将在下方更新')
  } catch { /* 不把请求失败或排队状态显示为完成。 */ } finally { creatingRun.value = false; schedulePoll() }
}

async function cancelRun(): Promise<void> {
  if (!detail.value || !projectId.value || !canCancelRun(detail.value.run) || canceling.value) return
  canceling.value = true
  try {
    await cancelDevRun(projectId.value, detail.value.run.key)
    await loadDetail()
    ElMessage.info('取消请求已提交，请以后台最终状态为准')
  } catch { /* 保留实际后台状态。 */ } finally { canceling.value = false }
}

async function buildRelease(): Promise<void> {
  if (!detail.value || !projectId.value || !buildAllowed.value || action.value) return
  action.value = 'build'; actionError.value = ''; detailRequest++
  try {
    const release = await buildDevRelease(projectId.value, detail.value.run.key)
    detail.value = { ...detail.value, release }
    previewUrl.value = ''; deployment.value = null; clearCredentials()
    if (!canUseRelease(detail.value.run, checks.value, release)) throw new Error('发布包尚未验证通过，或制品与当前源码版本不一致')
    ElMessage.success('发布包构建完成，请核对清单与校验摘要')
  } catch (error) { actionError.value = (error as Error).message || '发布包构建失败' }
  finally { action.value = ''; schedulePoll() }
}

async function downloadRelease(): Promise<void> {
  if (!detail.value?.release || !projectId.value || !verifiedRelease.value || action.value) return
  action.value = 'download'; actionError.value = ''
  try { await downloadDevRelease(projectId.value, detail.value.release.id) }
  catch (error) { actionError.value = (error as Error).message || '下载失败'; ElMessage.error(actionError.value) }
  finally { action.value = ''; schedulePoll() }
}

async function previewRelease(): Promise<void> {
  if (!detail.value?.release || !projectId.value || !verifiedRelease.value || action.value) return
  const releaseId = detail.value.release.id
  action.value = 'preview'; actionError.value = ''; previewUrl.value = ''; previewCredentials.value = null
  try {
    const preview = await previewDevRelease(projectId.value, releaseId)
    if (!isDeployedResult(preview, releaseId)) throw new Error('预览尚未部署成功：' + preview.status)
    previewUrl.value = safeHttpUrl(preview.url)
    previewCredentials.value = preview.credentials ?? null
    if (!previewUrl.value) throw new Error('预览未返回可访问地址，请查看执行记录')
  } catch (error) { previewUrl.value = ''; previewCredentials.value = null; actionError.value = (error as Error).message || '预览失败' }
  finally { action.value = ''; schedulePoll() }
}

async function deployRelease(): Promise<void> {
  if (!detail.value?.release || !projectId.value || !verifiedRelease.value || action.value || !targetId.value) return
  const releaseId = detail.value.release.id
  const selectedTarget = targets.value.find((target) => target.id === targetId.value)
  if (!selectedTarget) return
  const selectedProjectId = projectId.value
  try {
    await ElMessageBox.confirm(`将发布版本 ${releaseId} 安装到「${selectedTarget.name}」（${selectedTarget.id}）。此操作会初始化目标数据库并执行健康验收，确认部署？`, '部署固定版本', { type: 'warning' })
  } catch { return }
  if (projectId.value !== selectedProjectId || detail.value?.release?.id !== releaseId || targetId.value !== selectedTarget.id || !verifiedRelease.value || action.value) return
  action.value = 'deploy'; actionError.value = ''; deployment.value = null; deploymentCredentials.value = null
  try {
    const { credentials, ...metadata } = await deployDevRelease(selectedProjectId, releaseId, selectedTarget.id)
    if (!isDeployedResult(metadata, releaseId, selectedTarget.id)) throw new Error('目标部署未完成：' + metadata.status)
    deployment.value = metadata
    deploymentCredentials.value = credentials ?? null
  } catch (error) { actionError.value = (error as Error).message || '部署失败' }
  finally { action.value = ''; schedulePoll() }
}

onMounted(async () => {
  let saved: ReturnType<typeof readSavedSelection> = null
  try { saved = readSavedSelection(localStorage.getItem(SELECTION_KEY)) } catch { /* 可不使用本地选择记录。 */ }
  const results = await Promise.allSettled([listDevProjects(), listAiAgents()])
  if (disposed) return
  if (results[0].status === 'fulfilled') projects.value = results[0].value.list ?? []
  if (results[1].status === 'fulfilled') agents.value = results[1].value.list ?? []
  agentId.value = enabledAgents.value[0]?.id
  const selected = projects.value.find((project) => project.id === saved?.projectId) ?? projects.value[0]
  if (selected) await changeProject(selected.id, selected.id === saved?.projectId ? saved.runKey : '')
  initializing.value = false
  ready = true
  schedulePoll()
})
onActivated(() => { active = true; if (ready) void refreshCurrent() })
onDeactivated(() => { active = false; stopTimer(); clearCredentials() })
onBeforeUnmount(() => { disposed = true; active = false; selectionRevision++; detailRequest++; listRequest++; stopTimer(); clearCredentials() })
</script>

<style scoped>
.development-page { display: flex; flex-direction: column; gap: 12px; }
.project-select { width: 300px; }
.project-meta, .muted, .run-meta { color: var(--el-text-color-secondary); font-size: 12px; margin: 0; }
.work-card { padding: 16px; background: var(--el-bg-color); border: 1px solid var(--el-border-color-lighter); border-radius: 6px; min-width: 0; }
.section-heading, .compose-actions, .release-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.section-heading { justify-content: space-between; margin-bottom: 12px; }
.section-heading h3 { display: flex; align-items: center; gap: 8px; font-size: 14px; margin: 0; }
.section-heading > span { font-size: 12px; color: var(--el-text-color-secondary); }
.compose-actions { margin-top: 12px; }
.agent-select { width: 240px; }
.work-grid { display: grid; grid-template-columns: minmax(320px, 0.8fr) minmax(0, 1.5fr); gap: 12px; align-items: start; }
.runs-card .el-pagination { margin-top: 12px; justify-content: center; }
.requirement-text { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.6; font-size: 13px; max-height: 180px; overflow: auto; }
.detail-card .el-steps { margin: 14px 0; }
.detail-notice { margin-bottom: 10px; }
.event-list { list-style: none; padding: 0; margin: 0; max-height: 450px; overflow: auto; }
.event-list li { border-left: 2px solid var(--el-border-color); padding: 0 0 12px 12px; }
.event-heading { display: flex; justify-content: space-between; gap: 8px; font-size: 12px; }
.event-heading time { color: var(--el-text-color-secondary); }
.event-list pre, .report-pre { white-space: pre-wrap; overflow-wrap: anywhere; overflow: auto; font: 12px/1.6 ui-monospace, SFMono-Regular, Menlo, monospace; }
.event-list pre { max-height: 160px; margin: 6px 0 0; }
.report-pre { max-height: 480px; background: var(--el-fill-color-light); padding: 12px; border-radius: 4px; }
.diff-pre { white-space: pre; }
.delivery-section { margin-top: 16px; border-top: 1px solid var(--el-border-color-lighter); padding-top: 16px; }
.release-meta { display: grid; grid-template-columns: 80px minmax(0, 1fr); gap: 6px 10px; font-size: 12px; }
.release-meta dt { color: var(--el-text-color-secondary); }
.release-meta dd { margin: 0; overflow-wrap: anywhere; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.release-actions { margin: 12px 0; }
.target-select { width: 260px; }
@media (max-width: 1150px) { .work-grid { grid-template-columns: 1fr; } }
@media (max-width: 680px) { .project-select { width: 100%; } .compose-actions .agent-select, .target-select { width: 100%; } .section-heading { align-items: flex-start; } }
</style>
