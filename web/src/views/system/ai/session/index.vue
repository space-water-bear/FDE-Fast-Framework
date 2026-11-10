<template>
  <div class="page chat-page">
    <div class="chat-left">
      <div class="chat-left-toolbar">
        <el-select v-model="selectedAgentId" placeholder="选择智能体" style="width: 100%">
          <el-option v-for="a in agents" :key="a.id" :label="a.name" :value="a.id" />
        </el-select>
        <el-button type="primary" :disabled="!selectedAgentId" @click="handleNewSession">新建会话</el-button>
        <el-input v-model="keyword" placeholder="搜索会话标题" clearable @keyup.enter="handleSearch" @clear="handleSearch" />
      </div>
      <el-scrollbar class="chat-session-list">
        <div
          v-for="s in sessions"
          :key="s.id"
          class="chat-session-item"
          :class="{ active: s.id === currentSessionId }"
          @click="selectSession(s)"
        >
          <span class="chat-session-title">{{ s.title }}</span>
          <el-tag v-if="s.userId === 0" size="small" type="info">机器人历史</el-tag>
          <el-icon v-if="s.canDelete !== false" class="chat-session-del" @click.stop="handleDeleteSession(s)"><Close /></el-icon>
        </div>
        <div v-if="sessions.length === 0" class="chat-empty">暂无会话，选择智能体后新建</div>
      </el-scrollbar>
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="prev, pager, next"
        small
        class="chat-pagination"
        @current-change="loadSessions"
      />
    </div>

    <div class="chat-right">
      <div class="chat-header">
        <span>{{ currentSession ? currentSession.title : '请选择或新建会话' }}</span>
        <span class="chat-header-right">
          <span v-if="currentSession" class="chat-agent-name">{{ agentName(currentSession.agentId) }}</span>
          <el-tag v-if="isBotHistory" type="info" size="small">只读历史</el-tag>
          <template v-if="currentSession && !isBotHistory">
            <el-button link :icon="Edit" @click="handleRename">重命名</el-button>
            <el-button link type="danger" @click="handleClear">清空</el-button>
          </template>
        </span>
      </div>
      <div ref="msgBox" class="chat-messages">
        <div v-if="messages.length === 0" class="chat-empty">{{ isBotHistory ? '暂无机器人消息' : '发送一条消息开始对话' }}</div>
        <div v-for="m in messages" :key="m.id" class="chat-msg" :class="m.role">
          <div class="chat-bubble"><markdown-view :content="m.content" /></div>
          <div class="chat-msg-actions">
            <el-button link size="small" @click="copyText(m.content)">复制</el-button>
            <el-button v-if="!isBotHistory && m.role === 'user'" link size="small" @click="handleEditResend(m)">编辑重发</el-button>
            <el-button v-if="!isBotHistory && m.role === 'assistant' && isLastAssistant(m)" link size="small" @click="handleRegenerate">重新生成</el-button>
          </div>
        </div>
        <div v-if="sending && !isBotHistory" class="chat-msg assistant">
          <div class="chat-bubble">正在思考...</div>
        </div>
      </div>
      <div v-if="isBotHistory" class="chat-history-notice">请在原平台继续对话。</div>
      <div v-else class="chat-input">
        <el-input
          v-model="inputText"
          type="textarea"
          :rows="3"
          :disabled="!currentSessionId || sending"
          placeholder="输入消息，Enter 发送，Shift+Enter 换行"
          @keydown.enter.exact.prevent="handleSend"
        />
        <el-button v-if="sending" type="warning" @click="handleStop">停止</el-button>
        <el-button type="primary" :disabled="!currentSessionId || sending || !inputText.trim()" @click="handleSend">
          发送
        </el-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Close, Edit } from '@element-plus/icons-vue'
import {
  clearAiSession,
  createAiSession,
  deleteAiSession,
  listAiAgents,
  listAiMessages,
  listAiSessions,
  regenerateAiSession,
  renameAiSession,
  resendAiMessage,
  streamChatAi,
} from '@/api/ai'
import type { AiAgentItem, AiMessageItem, AiSessionItem } from '@/api/ai'
import MarkdownView from '@/components/MarkdownView.vue'

const agents = ref<AiAgentItem[]>([])
type SessionListItem = AiSessionItem & { canDelete?: boolean }

const sessions = ref<SessionListItem[]>([])
const messages = ref<AiMessageItem[]>([])
const selectedAgentId = ref<number | undefined>(undefined)
const currentSessionId = ref<number | null>(null)
const currentSession = ref<SessionListItem | null>(null)
const isBotHistory = computed(() => currentSession.value?.userId === 0)
const inputText = ref('')
const sending = ref(false)
const keyword = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const msgBox = ref<any>()
let abortCtrl: AbortController | null = null

function agentName(id: number) {
  const a = agents.value.find((x) => x.id === id)
  return a ? a.name : '未知智能体'
}

async function loadAgents() {
  const data = await listAiAgents()
  agents.value = data.list
}

async function loadSessions() {
  const data = await listAiSessions({ keyword: keyword.value.trim(), page: page.value, pageSize })
  sessions.value = data.list
  total.value = data.total
}

function handleSearch() {
  page.value = 1
  loadSessions()
}

async function selectSession(s: SessionListItem) {
  currentSession.value = s
  currentSessionId.value = s.id
  messages.value = []
  const data = await listAiMessages(s.id)
  if (currentSessionId.value !== s.id) return
  messages.value = data.list
  scrollBottom()
}

async function handleNewSession() {
  if (!selectedAgentId.value) return
  const s = await createAiSession({ agentId: selectedAgentId.value })
  ElMessage.success('会话已创建')
  page.value = 1
  keyword.value = ''
  await loadSessions()
  await selectSession(s)
}

async function handleSend() {
  const content = inputText.value.trim()
  if (!content || !currentSessionId.value || sending.value || isBotHistory.value) return
  const sid = currentSessionId.value
  inputText.value = ''
  messages.value.push({ id: Date.now(), sessionId: sid, role: 'user', content, createdAt: '' })
  const assistant = { id: Date.now() + 1, sessionId: sid, role: 'assistant', content: '', createdAt: '' }
  messages.value.push(assistant)
  scrollBottom()
  sending.value = true
  abortCtrl = new AbortController()
  try {
    await streamChatAi(sid, content, (delta) => {
      if (currentSessionId.value !== sid) return
      assistant.content += delta
      scrollBottom()
    }, abortCtrl.signal)
    if (!assistant.content) {
      assistant.content = '(无回复)'
    }
  } catch (e: any) {
    if (e && e.name === 'AbortError') {
      if (!assistant.content) assistant.content = '(已停止)'
      else assistant.content += '\n(已停止)'
    } else {
      assistant.content = assistant.content || (e && e.message ? e.message : '流式请求失败')
    }
  } finally {
    sending.value = false
    abortCtrl = null
  }
}

function handleStop() {
  if (abortCtrl) abortCtrl.abort()
}

function copyText(text: string) {
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text).then(
      () => ElMessage.success('已复制'),
      () => ElMessage.warning('复制失败'),
    )
  }
}

function lastAssistantIdx(): number {
  for (let i = messages.value.length - 1; i >= 0; i--) {
    if (messages.value[i].role === 'assistant') return i
  }
  return -1
}

function isLastAssistant(m: AiMessageItem): boolean {
  const idx = lastAssistantIdx()
  return idx >= 0 && messages.value[idx] === m
}

async function handleRegenerate() {
  const sid = currentSessionId.value
  if (!sid || sending.value || isBotHistory.value) return
  sending.value = true
  try {
    const msg = await regenerateAiSession(sid)
    if (currentSessionId.value !== sid) return
    const idx = lastAssistantIdx()
    if (idx >= 0) messages.value.splice(idx, 1)
    messages.value.push(msg)
    scrollBottom()
  } finally {
    sending.value = false
  }
}

async function handleEditResend(m: AiMessageItem) {
  const sid = currentSessionId.value
  if (!sid || m.sessionId !== sid || sending.value || isBotHistory.value) return
  let content: string
  try {
    const { value } = await ElMessageBox.prompt('编辑消息内容', '编辑重发', {
      inputValue: m.content,
      inputType: 'textarea',
    })
    content = value.trim()
  } catch {
    return
  }
  if (!content || currentSessionId.value !== sid || sending.value || isBotHistory.value) return
  sending.value = true
  try {
    const reply = await resendAiMessage(sid, m.id, content)
    if (currentSessionId.value !== sid) return
    const idx = messages.value.findIndex((x) => x.id === m.id)
    if (idx >= 0) {
      messages.value[idx].content = content
      messages.value.splice(idx + 1)
    }
    messages.value.push(reply)
    scrollBottom()
  } catch {
    // Request errors are displayed by the interceptor.
  } finally {
    sending.value = false
  }
}

async function handleRename() {
  const session = currentSession.value
  if (!session || isBotHistory.value) return
  try {
    const { value } = await ElMessageBox.prompt('新的会话标题', '重命名', {
      inputValue: session.title,
    })
    if (!value || !value.trim() || currentSessionId.value !== session.id) return
    await renameAiSession(session.id, value.trim())
    session.title = value.trim()
    loadSessions()
  } catch {
    /* 用户取消 */
  }
}

async function handleClear() {
  const sid = currentSessionId.value
  if (!sid || isBotHistory.value) return
  try {
    await ElMessageBox.confirm('确定清空该会话的全部消息吗？', '提示', { type: 'warning' })
    if (currentSessionId.value !== sid) return
    await clearAiSession(sid)
    if (currentSessionId.value === sid) messages.value = []
    ElMessage.success('已清空')
  } catch {
    /* 用户取消 */
  }
}

function scrollBottom() {
  nextTick(() => {
    if (msgBox.value) {
      msgBox.value.scrollTop = msgBox.value.scrollHeight
    }
  })
}

function handleDeleteSession(s: SessionListItem) {
  if (s.canDelete === false) return
  ElMessageBox.confirm('确定删除会话「' + s.title + '」及其全部消息吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteAiSession(s.id)
      ElMessage.success('已删除')
      if (currentSessionId.value === s.id) {
        currentSessionId.value = null
        currentSession.value = null
        messages.value = []
      }
      await loadSessions()
    })
    .catch(() => {})
}

onMounted(async () => {
  await loadAgents()
  await loadSessions()
})
</script>

<style scoped>
.chat-page {
  height: 100%;
  display: flex;
  gap: 12px;
}
.chat-left {
  width: 300px;
  flex: 0 0 300px;
  background: #fff;
  border-radius: 4px;
  padding: 12px;
  display: flex;
  flex-direction: column;
}
.chat-left-toolbar {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 10px;
}
.chat-session-list {
  flex: 1;
  min-height: 0;
}
.chat-session-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border-radius: 4px;
  cursor: pointer;
  color: #303133;
  margin-bottom: 4px;
}
.chat-session-item:hover {
  background: #f5f7fa;
}
.chat-session-item.active {
  background: #ecf5ff;
  color: #409eff;
}
.chat-session-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chat-session-del {
  font-size: 12px;
  opacity: 0;
}
.chat-session-item:hover .chat-session-del {
  opacity: 1;
}
.chat-right {
  flex: 1;
  min-width: 0;
  background: #fff;
  border-radius: 4px;
  display: flex;
  flex-direction: column;
}
.chat-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid #e4e7ed;
  font-weight: 600;
}
.chat-agent-name {
  font-size: 12px;
  color: #909399;
  font-weight: 400;
}
.chat-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 14px;
  background: #f5f7fa;
}
.chat-msg {
  display: flex;
  margin-bottom: 12px;
}
.chat-msg.user {
  justify-content: flex-end;
}
.chat-msg.assistant {
  justify-content: flex-start;
}
.chat-bubble {
  max-width: 76%;
  padding: 10px 14px;
  border-radius: 8px;
  font-size: 14px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
.chat-msg.user .chat-bubble {
  background: #409eff;
  color: #fff;
}
.chat-msg.assistant .chat-bubble {
  background: #fff;
  color: #303133;
  border: 1px solid #e4e7ed;
}
.chat-empty {
  text-align: center;
  color: #909399;
  font-size: 13px;
  margin-top: 40px;
}
.chat-history-notice {
  padding: 12px 16px;
  border-top: 1px solid var(--el-border-color-light);
  color: var(--el-text-color-secondary);
}
.chat-input {
  display: flex;
  gap: 10px;
  padding: 10px 14px;
  border-top: 1px solid #e4e7ed;
  align-items: flex-end;
}
.chat-bubble :deep(.markdown-body) {
  line-height: 1.6;
}
.chat-bubble :deep(.markdown-body p) {
  margin: 4px 0;
}
.chat-bubble :deep(.markdown-body ul),
.chat-bubble :deep(.markdown-body ol) {
  padding-left: 18px;
  margin: 4px 0;
}
.chat-bubble :deep(.markdown-body h1),
.chat-bubble :deep(.markdown-body h2),
.chat-bubble :deep(.markdown-body h3),
.chat-bubble :deep(.markdown-body h4) {
  margin: 8px 0 4px;
  font-weight: 600;
}
.chat-bubble :deep(.markdown-body code) {
  background: rgba(0, 0, 0, 0.08);
  padding: 2px 5px;
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.9em;
}
.chat-bubble :deep(.markdown-body pre) {
  background: #0d1117;
  color: #c9d1d9;
  padding: 10px 12px;
  border-radius: 6px;
  overflow-x: auto;
  margin: 6px 0;
}
.chat-bubble :deep(.markdown-body pre code) {
  background: transparent;
  color: inherit;
  padding: 0;
}
.chat-bubble :deep(.markdown-body blockquote) {
  border-left: 3px solid #dcdfe6;
  margin: 6px 0;
  padding-left: 10px;
  color: #909399;
}
.chat-bubble :deep(.markdown-body table) {
  border-collapse: collapse;
  margin: 6px 0;
}
.chat-bubble :deep(.markdown-body th),
.chat-bubble :deep(.markdown-body td) {
  border: 1px solid #dcdfe6;
  padding: 4px 8px;
}
.chat-bubble :deep(.markdown-body a) {
  color: #409eff;
  text-decoration: underline;
}
.chat-header-right {
  display: flex;
  align-items: center;
  gap: 4px;
}
.chat-msg-actions {
  display: flex;
  gap: 2px;
  margin-top: 2px;
}
.chat-msg.user .chat-msg-actions {
  justify-content: flex-end;
}
</style>
