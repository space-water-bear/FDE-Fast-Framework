<template>
  <div class="page oplog-page">
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchLogs"
      :search-fields="searchFields"
      :toolbar-actions="[]"
    >
      <template #toolbar>
        <el-button type="danger" :icon="Delete" @click="handleClear">清空日志</el-button>
      </template>
      <template #methodPath="{ row }">
        <el-tag size="small" class="mr-4">{{ row.method }}</el-tag>
        <span>{{ row.path }}</span>
      </template>
      <template #result="{ row }">
        <el-tag :type="row.code === 0 ? 'success' : 'danger'" size="small">
          {{ row.code === 0 ? '成功' : '失败' }}
        </el-tag>
      </template>
      <template #createdAt="{ row }">{{ formatTime(row.createdAt) }}</template>
      <template #ops="{ row }">
        <el-button link type="primary" @click="openDetail(row)">详情</el-button>
        <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="detailVisible" title="日志详情" width="680px">
      <el-descriptions :column="2" border size="small">
        <el-descriptions-item label="用户">{{ detail.username || '-' }}</el-descriptions-item>
        <el-descriptions-item label="IP">{{ detail.ip }}</el-descriptions-item>
        <el-descriptions-item label="方法">{{ detail.method }}</el-descriptions-item>
        <el-descriptions-item label="状态码">{{ detail.status }}</el-descriptions-item>
        <el-descriptions-item label="业务码">{{ detail.code }}</el-descriptions-item>
        <el-descriptions-item label="耗时">{{ detail.latencyMs }} ms</el-descriptions-item>
        <el-descriptions-item label="路径" :span="2">{{ detail.path }}</el-descriptions-item>
        <el-descriptions-item label="消息" :span="2">{{ detail.msg }}</el-descriptions-item>
      </el-descriptions>
      <div class="log-block">
        <div class="log-block-title">请求体</div>
        <pre class="log-pre">{{ detail.body || '(空)' }}</pre>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete } from '@element-plus/icons-vue'
import { clearOplogs, deleteOplog, listOplogs } from '@/api/oplog'
import type { OplogItem } from '@/api/oplog'
import { formatTime } from '@/utils/format'
import { TablePage } from '@/components/table-page'
import type { SearchField, TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()

const searchFields: SearchField[] = [
  { field: 'keyword', label: '关键词', placeholder: '用户名 / 路径' },
]

const columns = [
  { field: 'id', title: 'ID', width: 70 },
  { field: 'username', title: '用户', width: 100 },
  { title: '请求', minWidth: 220, slots: { default: 'methodPath' } },
  { field: 'ip', title: 'IP', width: 120 },
  { title: '结果', width: 90, slots: { default: 'result' } },
  { field: 'latencyMs', title: '耗时(ms)', width: 90 },
  { title: '时间', width: 170, slots: { default: 'createdAt' } },
  { title: '操作', width: 140, fixed: 'right', slots: { default: 'ops' } },
]

async function fetchLogs(query: TableQuery) {
  const data = await listOplogs({
    page: query.page,
    pageSize: query.pageSize,
    keyword: (query.keyword as string) || '',
  })
  return { list: data.list, total: data.total }
}

const detailVisible = ref(false)
const detail = reactive<OplogItem>({
  id: 0,
  userId: 0,
  username: '',
  method: '',
  path: '',
  query: '',
  body: '',
  ip: '',
  userAgent: '',
  status: 0,
  code: 0,
  msg: '',
  latencyMs: 0,
  createdAt: '',
})

function openDetail(row: OplogItem) {
  Object.assign(detail, row)
  detailVisible.value = true
}

function handleDelete(row: OplogItem) {
  ElMessageBox.confirm('确定删除该条日志吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteOplog(row.id)
      ElMessage.success('删除成功')
      pageRef.value?.reload()
    })
    .catch(() => {})
}

function handleClear() {
  ElMessageBox.confirm('确定清空全部操作日志吗？此操作不可恢复。', '提示', { type: 'warning' })
    .then(async () => {
      await clearOplogs()
      ElMessage.success('已清空')
      pageRef.value?.reload()
    })
    .catch(() => {})
}
</script>

<style scoped>
.oplog-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.mr-4 {
  margin-right: 4px;
}
.log-block {
  margin-top: 12px;
}
.log-block-title {
  font-weight: 600;
  margin-bottom: 6px;
}
.log-pre {
  background: #f5f7fa;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  padding: 10px;
  max-height: 260px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 12px;
  color: #303133;
}
</style>
