<template>
  <div class="page device-page">
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchDevices"
      :search-fields="[]"
      :toolbar-actions="[]"
    >
      <template #toolbar>
        <el-button type="danger" :icon="SwitchButton" @click="handleKickOthers">踢出其他设备</el-button>
      </template>
      <template #userAgent="{ row }">{{ row.userAgent || '-' }}</template>
      <template #createdAt="{ row }">{{ formatTime(row.createdAt) }}</template>
      <template #expiresAt="{ row }">{{ formatTime(row.expiresAt) }}</template>
      <template #ops="{ row }">
        <el-button link type="danger" @click="handleKick(row)">踢下线</el-button>
      </template>
    </table-page>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { SwitchButton } from '@element-plus/icons-vue'
import { kickDevice, kickOtherDevices, listDevices } from '@/api/auth'
import type { DeviceItem } from '@/api/auth'
import { formatTime } from '@/utils/format'
import { TablePage } from '@/components/table-page'
import type { TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()

const columns = [
  { field: 'id', title: 'ID', width: 70 },
  { field: 'ip', title: '登录 IP', width: 140 },
  { title: '设备(UA)', minWidth: 260, slots: { default: 'userAgent' } },
  { title: '登录时间', width: 170, slots: { default: 'createdAt' } },
  { title: '令牌过期', width: 170, slots: { default: 'expiresAt' } },
  { title: '操作', width: 110, fixed: 'right', slots: { default: 'ops' } },
]

async function fetchDevices(_query: TableQuery) {
  const data = await listDevices()
  return { list: data.list, total: data.list.length }
}

function handleKick(row: DeviceItem) {
  ElMessageBox.confirm('确定将该设备踢下线吗？', '提示', { type: 'warning' })
    .then(async () => {
      await kickDevice(row.id)
      ElMessage.success('已踢下线')
      pageRef.value?.reload()
    })
    .catch(() => {})
}

function handleKickOthers() {
  ElMessageBox.confirm('确定踢出除当前设备外的全部设备吗？', '提示', { type: 'warning' })
    .then(async () => {
      await kickOtherDevices()
      ElMessage.success('已踢出其他设备')
      pageRef.value?.reload()
    })
    .catch(() => {})
}
</script>

<style scoped>
.device-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
</style>
