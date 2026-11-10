<template>
  <div class="page ai-bot-page">
    <div class="platform-summary">
      <span>双向 AI 对话</span>
      <el-tag type="success">企业微信 · 长连接</el-tag>
      <el-tag type="success">钉钉 · Stream</el-tag>
      <el-tag type="info">飞书 · 后续支持</el-tag>
    </div>
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchBots"
      :search-fields="[{ field: 'keyword', label: '关键词', placeholder: '机器人名称' }]"
      :toolbar-actions="[{ code: 'add', label: '新增机器人', type: 'primary' }]"
      @add="openDialog()"
    >
      <template #platform="{ row }">{{ platformName(row.platform) }}</template>
      <template #agent="{ row }">{{ agentName(row.agentId) }}</template>
      <template #enabled="{ row }">
        <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
      </template>
      <template #connection="{ row }">
        <el-tag :type="connectionStatus(row).type" size="small">{{ connectionStatus(row).label }}</el-tag>
      </template>
      <template #ops="{ row }">
        <el-button link type="primary" @click="showConnection(row)">接入配置</el-button>
        <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
        <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑机器人' : '新增机器人'" width="640px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="130px" :disabled="saving">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" maxlength="128" placeholder="如：内部知识助手" />
        </el-form-item>
        <el-form-item label="平台" prop="platform">
          <el-select v-model="form.platform" :disabled="!!editingId" style="width: 100%" @change="clearCredentials">
            <el-option label="企业微信 · 智能机器人长连接" value="wecom" />
            <el-option label="钉钉 · 企业内部机器人 Stream" value="dingtalk" />
            <el-option label="飞书 · 后续支持" value="feishu" disabled />
          </el-select>
        </el-form-item>
        <el-form-item label="智能体" prop="agentId">
          <el-select :model-value="form.agentId || undefined" filterable placeholder="选择已启用的智能体" style="width: 100%" :loading="loadingAgents" @update:model-value="form.agentId = $event ?? 0">
            <el-option v-for="agent in selectableAgents" :key="agent.id" :value="agent.id" :disabled="!agent.enabled" :label="agent.name + (agent.enabled ? '' : '（已禁用）')" />
          </el-select>
          <p class="field-hint">复用智能体模型与技能；不同群聊及发送人的对话分别保存。</p>
        </el-form-item>
        <template v-if="form.platform === 'wecom'">
          <el-form-item label="BotID" prop="botId">
            <el-input v-model="form.botId" maxlength="128" autocomplete="off" placeholder="填写智能机器人的 BotID" @input="formRef?.clearValidate('secret')" />
          </el-form-item>
          <el-form-item label="Secret" prop="secret">
            <el-input v-model="form.secret" type="password" show-password autocomplete="new-password" :placeholder="wecomSecretPlaceholder" />
            <p v-if="botIdChanged" class="field-hint">BotID 已变更，请填写该机器人的 Secret。</p>
          </el-form-item>
          <el-alert class="platform-notice" type="info" :closable="false" show-icon title="长连接无需公网回调地址。服务端需能访问企业微信的 WSS 服务，启用后自动连接、保持心跳并在断线后重连。" />
          <el-alert class="platform-notice" type="warning" :closable="false" show-icon title="文档与智能表格权限须单独授权，当前仅文字问答，尚未开放该能力。">
            <a :href="wecomAuthorizationUrl" target="_blank" rel="noopener noreferrer">查看企业微信官方文档授权说明</a>
          </el-alert>
        </template>
        <template v-else>
          <el-form-item label="Client ID" prop="clientId">
            <el-input v-model="form.clientId" maxlength="128" autocomplete="off" placeholder="填写钉钉应用的 Client ID（AppKey）" @input="formRef?.clearValidate('clientSecret')" />
          </el-form-item>
          <el-form-item label="Client Secret" prop="clientSecret">
            <el-input v-model="form.clientSecret" type="password" show-password autocomplete="new-password" :placeholder="dingTalkSecretPlaceholder" />
            <p v-if="clientIdChanged" class="field-hint">Client ID 已变更，请填写该应用的 Client Secret。</p>
          </el-form-item>
          <el-alert class="platform-notice" type="info" :closable="false" show-icon title="无需公网回调地址。消息经 Stream 长连接接收，当前回复仍通过钉钉临时会话地址发送。">
            <a :href="dingTalkStreamUrl" target="_blank" rel="noopener noreferrer">查看钉钉官方 Stream 接入文档</a>
          </el-alert>
        </template>
        <el-form-item label="状态">
          <el-switch v-model="form.enabled" active-text="启用" inactive-text="禁用" />
          <p class="field-hint">新建默认禁用。完成平台配置后，再启用接收提问。</p>
        </el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" maxlength="255" placeholder="用途说明" /></el-form-item>
      </el-form>
      <el-alert
        title="当前仅提供文字问答，业务工具与 MCP 待账号权限接入后开放。"
        type="info"
        :closable="false"
        show-icon
      />
      <template #footer>
        <el-button :disabled="saving" @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="connectionVisible" title="接入配置" width="640px" destroy-on-close>
      <template v-if="connectionBot">
        <p><strong>{{ connectionBot.name }}</strong> · {{ platformName(connectionBot.platform) }}</p>
        <el-descriptions :column="1" border class="connection-details">
          <el-descriptions-item :label="connectionBot.platform === 'wecom' ? 'BotID' : 'Client ID'">{{ (connectionBot.platform === 'wecom' ? connectionBot.botId : connectionBot.clientId) || '未配置' }}</el-descriptions-item>
          <el-descriptions-item label="连接状态"><el-tag :type="connectionStatus(connectionBot).type">{{ connectionStatus(connectionBot).label }}</el-tag></el-descriptions-item>
          <el-descriptions-item label="最近连接">{{ formatConnectionTime(connectionBot.connection?.lastConnectedAt) }}</el-descriptions-item>
          <el-descriptions-item label="最近心跳">{{ formatConnectionTime(connectionBot.connection?.lastHeartbeatAt) }}</el-descriptions-item>
        </el-descriptions>
        <template v-if="connectionBot.platform === 'wecom'">
          <el-alert class="connection-notice" title="长连接无需公网回调地址，服务端需能出站访问企业微信的 WSS 服务。" type="info" :closable="false" show-icon />
          <ol class="connection-steps">
            <li>在企业微信智能机器人中使用长连接方式，获取该机器人的 BotID 和 Secret。</li>
            <li>编辑本系统机器人，填写 BotID 和 Secret 并启用；保存后系统自动连接。</li>
            <li>确认显示“已连接”后，向机器人发送文字提问。网络断线后自动重连；同一 BotID 被其他实例接管后，本实例会停止重连。</li>
          </ol>
          <el-alert type="warning" :closable="false" show-icon title="文档与智能表格权限须按官方文档单独授权，当前尚未开放该能力。">
            <a :href="wecomAuthorizationUrl" target="_blank" rel="noopener noreferrer">查看企业微信官方文档授权说明</a>
          </el-alert>
        </template>
        <template v-else>
          <el-alert class="connection-notice" title="无需公网回调地址。消息经 Stream 长连接接收，当前回复仍通过钉钉临时会话地址发送。" type="info" :closable="false" show-icon />
          <ol class="connection-steps">
            <li>在钉钉开放平台创建企业内部应用，添加机器人能力。</li>
            <li>将机器人消息接收模式设为 Stream，保存发布，并配置可见范围。</li>
            <li>将应用的 Client ID（AppKey）与 Client Secret（AppSecret）填入本系统并启用。</li>
            <li>确认显示“已连接”后，在单聊中直接提问，或在群聊中 @机器人 提问；断线后系统自动重连。</li>
          </ol>
          <a :href="dingTalkStreamUrl" target="_blank" rel="noopener noreferrer">查看钉钉官方 Stream 接入文档</a>
        </template>
        <p class="field-hint">仅支持文字对话。密钥已隐藏；编辑时留空保留现有密钥，变更 BotID 或 Client ID 时须填写对应密钥。</p>
      </template>
      <template #footer><el-button type="primary" @click="connectionVisible = false">知道了</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onDeactivated, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { listAiAgents } from '@/api/ai'
import type { AiAgentItem } from '@/api/ai'
import { createAiBot, deleteAiBot, listAiBots, updateAiBot } from '@/api/ai-bot'
import type { AiBotForm, AiBotItem, AiBotPlatform } from '@/api/ai-bot'
import { TablePage } from '@/components/table-page'
import type { TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()
const agents = ref<AiAgentItem[]>([])
const loadingAgents = ref(false)
const dialogVisible = ref(false)
const connectionVisible = ref(false)
const connectionBot = ref<AiBotItem | null>(null)
const editingBot = ref<AiBotItem | null>(null)
const editingId = computed(() => editingBot.value?.id)
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive<AiBotForm>({
  name: '', platform: 'wecom', agentId: 0, enabled: false, remark: '',
  botId: '', secret: '', clientId: '', clientSecret: '',
})
const botIdChanged = computed(() => !!editingBot.value && form.botId.trim() !== editingBot.value.botId)
const wecomSecretPlaceholder = computed(() => editingBot.value?.hasSecret && !botIdChanged.value ? '已设置，留空保留原值' : '填写该机器人的 Secret')
const clientIdChanged = computed(() => !!editingBot.value && form.clientId.trim() !== editingBot.value.clientId)
const dingTalkSecretPlaceholder = computed(() => editingBot.value?.hasClientSecret && !clientIdChanged.value ? '已设置，留空保留原值' : '填写该应用的 Client Secret（AppSecret）')
const selectableAgents = computed(() => agents.value.filter((agent) => agent.enabled || agent.id === form.agentId))
const wecomAuthorizationUrl = 'https://developer.work.weixin.qq.com/document/path/101468'
const dingTalkStreamUrl = 'https://open-dingtalk.github.io/developerpedia/docs/explore/tutorials/stream/bot/python/create-bot/'
let refreshTimer: ReturnType<typeof setInterval> | undefined
let listLoading = false
let hasActiveBot = false

const columns = [
  { field: 'id', title: 'ID', width: 60 },
  { field: 'name', title: '名称', minWidth: 140 },
  { title: '平台', width: 120, slots: { default: 'platform' } },
  { title: '智能体', minWidth: 150, slots: { default: 'agent' } },
  { title: '状态', width: 80, slots: { default: 'enabled' } },
  { title: '连接', minWidth: 150, slots: { default: 'connection' } },
  { field: 'remark', title: '备注', minWidth: 150 },
  { title: '操作', width: 210, fixed: 'right', slots: { default: 'ops' } },
]

function credentialRule(field: 'secret' | 'clientSecret', platform: AiBotPlatform) {
  return {
    validator: (_rule: unknown, value: string, callback: (error?: Error) => void) => {
      if (form.platform !== platform) return callback()
      const hasSaved = field === 'secret' ? editingBot.value?.hasSecret && !botIdChanged.value : editingBot.value?.hasClientSecret && !clientIdChanged.value
      if (!value.trim() && !hasSaved) return callback(new Error('请填写 ' + (field === 'secret' ? 'Secret' : 'Client Secret')))
      callback()
    },
    trigger: 'blur',
  }
}

const rules: FormRules = {
  name: [{ required: true, whitespace: true, message: '请输入机器人名称', trigger: 'blur' }],
  agentId: [{ validator: (_rule, value, callback) => callback(value > 0 ? undefined : new Error('请选择智能体')), trigger: 'change' }],
  botId: [{ validator: (_rule, value, callback) => callback(form.platform !== 'wecom' || String(value).trim() ? undefined : new Error('请填写 BotID')), trigger: 'blur' }],
  clientId: [{ validator: (_rule, value, callback) => callback(form.platform !== 'dingtalk' || String(value).trim() ? undefined : new Error('请填写 Client ID（AppKey）')), trigger: 'blur' }],
  secret: [credentialRule('secret', 'wecom')],
  clientSecret: [credentialRule('clientSecret', 'dingtalk')],
}

const connectionStates: Record<string, { label: string; type: 'success' | 'info' | 'warning' | 'danger' }> = {
  disabled: { label: '已禁用', type: 'info' },
  unconfigured: { label: '待配置', type: 'warning' },
  connecting: { label: '连接中', type: 'warning' },
  authenticating: { label: '鉴权中', type: 'warning' },
  connected: { label: '已连接', type: 'success' },
  reconnecting: { label: '重连中', type: 'warning' },
  auth_failed: { label: '鉴权失败，请检查凭据', type: 'danger' },
  disconnected: { label: '已断开', type: 'danger' },
  conflict: { label: '连接被接管', type: 'danger' },
}

function connectionStatus(bot: AiBotItem) {
  const configured = bot.platform === 'wecom' ? bot.botId && bot.hasSecret : bot.clientId && bot.hasClientSecret
  if (!configured) return connectionStates.unconfigured!
  if (!bot.enabled) return connectionStates.disabled!
  const state = bot.connection?.state || 'unconfigured'
  const status = connectionStates[state] || { label: '状态待更新', type: 'info' as const }
  const attempt = bot.connection?.reconnectAttempt || 0
  return state === 'reconnecting' && attempt > 0 ? { ...status, label: `${status.label}（第 ${attempt} 次）` } : status
}

function formatConnectionTime(value?: string) {
  if (!value || value.startsWith('0001-')) return '暂无记录'
  const time = new Date(value)
  return Number.isNaN(time.getTime()) ? '暂无记录' : time.toLocaleString('zh-CN', { hour12: false })
}

function platformName(platform: AiBotPlatform) {
  return platform === 'wecom' ? '企业微信' : '钉钉'
}

function agentName(id: number) {
  return agents.value.find((agent) => agent.id === id)?.name || '智能体不存在'
}

function clearCredentials() {
  form.botId = ''
  form.secret = ''
  form.clientId = ''
  form.clientSecret = ''
  formRef.value?.clearValidate(['botId', 'secret', 'clientId', 'clientSecret'])
}

async function loadAgents() {
  loadingAgents.value = true
  try {
    agents.value = (await listAiAgents()).list
  } finally {
    loadingAgents.value = false
  }
}

async function fetchBots(query: TableQuery) {
  listLoading = true
  try {
    const result = await listAiBots({ keyword: (query.keyword as string) || '' })
    hasActiveBot = result.list.some((bot) => bot.enabled)
    if (connectionBot.value) {
      const fresh = result.list.find((bot) => bot.id === connectionBot.value?.id)
      if (fresh) connectionBot.value = fresh
    }
    return result
  } finally {
    listLoading = false
  }
}

function openDialog(row?: AiBotItem) {
  editingBot.value = row || null
  Object.assign(form, {
    name: row?.name || '', platform: row?.platform || 'wecom', agentId: row?.agentId || 0,
    enabled: row?.enabled || false, remark: row?.remark || '', botId: row?.botId || '', secret: '', clientId: row?.clientId || '', clientSecret: '',
  })
  dialogVisible.value = true
  void loadAgents()
}

function showConnection(row: AiBotItem) {
  connectionBot.value = row
  connectionVisible.value = true
}

async function handleSave() {
  if (!formRef.value || !(await formRef.value.validate().catch(() => false))) return
  saving.value = true
  try {
    const data = { ...form, name: form.name.trim(), botId: form.botId.trim(), clientId: form.clientId.trim() }
    const saved = editingId.value ? await updateAiBot(editingId.value, data) : await createAiBot(data)
    ElMessage.success('保存成功')
    dialogVisible.value = false
    clearCredentials()
    pageRef.value?.reload()
    showConnection(saved)
  } finally {
    saving.value = false
  }
}

async function handleDelete(row: AiBotItem) {
  try {
    await ElMessageBox.confirm('确定删除机器人「' + row.name + '」吗？删除后将停止接收新提问。', '删除机器人', { type: 'warning' })
  } catch {
    return
  }
  await deleteAiBot(row.id)
  ElMessage.success('删除成功')
  pageRef.value?.reload()
}

function startRefresh() {
  if (refreshTimer) return
  refreshTimer = setInterval(() => {
    if (hasActiveBot && !listLoading && !saving.value && !document.hidden) pageRef.value?.reload()
  }, 10000)
}

function stopRefresh() {
  if (refreshTimer) clearInterval(refreshTimer)
  refreshTimer = undefined
}

onMounted(() => { void loadAgents(); startRefresh() })
onActivated(startRefresh)
onDeactivated(stopRefresh)
onUnmounted(stopRefresh)
</script>

<style scoped>
.ai-bot-page { height: 100%; display: flex; flex-direction: column; gap: 12px; }
.platform-summary { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; color: var(--el-text-color-secondary); font-size: 14px; }
.ai-bot-page :deep(.table-page) { flex: 1; min-height: 0; }
.field-hint { width: 100%; margin: 6px 0 0; color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.6; }
.connection-steps { padding-left: 22px; line-height: 1.8; }
.connection-steps li + li { margin-top: 8px; }
.connection-details { margin-top: 18px; }
.connection-notice { margin-top: 18px; }
.platform-notice { margin-bottom: 16px; }
.platform-notice a, .connection-steps + .el-alert a { color: var(--el-color-primary); text-decoration: underline; }
</style>
