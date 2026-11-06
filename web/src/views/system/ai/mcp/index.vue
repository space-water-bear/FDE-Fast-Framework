<template>
  <div class="page ai-page">
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchMcp"
      :search-fields="[{ field: 'keyword', label: '关键词', placeholder: '名称 / 地址' }]"
      :toolbar-actions="[{ code: 'add', label: '新增MCP服务', type: 'primary' }]"
      @add="openDialog()"
    >
      <template #addr="{ row }">{{ row.url || (row.command + ' ' + row.args) }}</template>
      <template #enabled="{ row }">
        <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
      </template>
      <template #ops="{ row }">
        <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
        <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑MCP服务' : '新增MCP服务'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="名称" prop="name"><el-input v-model="form.name" placeholder="如: 文件系统" /></el-form-item>
        <el-form-item label="传输方式">
          <el-select v-model="form.transport" style="width: 100%">
            <el-option label="SSE" value="sse" />
            <el-option label="Streamable HTTP" value="streamable" />
            <el-option label="Stdio" value="stdio" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.transport !== 'stdio'" label="服务地址"><el-input v-model="form.url" placeholder="http://host:port/mcp" /></el-form-item>
        <el-form-item v-else label="命令"><el-input v-model="form.command" placeholder="如: npx" /></el-form-item>
        <el-form-item v-if="form.transport === 'stdio'" label="参数"><el-input v-model="form.args" placeholder="如: -y @modelcontextprotocol/server-filesystem /tmp" /></el-form-item>
        <el-form-item label="Headers"><el-input v-model="form.headers" placeholder='JSON, 如 {"Authorization":"Bearer xxx"}' /></el-form-item>
        <el-form-item label="状态"><el-switch v-model="form.enabled" active-text="启用" inactive-text="禁用" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { deleteAiMcpServer, listAiMcpServers, saveAiMcpServer } from '@/api/ai'
import type { AiMcpItem } from '@/api/ai'
import { TablePage } from '@/components/table-page'
import type { TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()

const columns = [
  { field: 'id', title: 'ID', width: 60 },
  { field: 'name', title: '名称', minWidth: 120 },
  { field: 'transport', title: '传输', width: 110 },
  { title: '地址/命令', minWidth: 240, slots: { default: 'addr' } },
  { title: '状态', width: 80, slots: { default: 'enabled' } },
  { field: 'remark', title: '备注', minWidth: 120 },
  { title: '操作', width: 130, fixed: 'right', slots: { default: 'ops' } },
]

async function fetchMcp(query: TableQuery) {
  const data = await listAiMcpServers({ keyword: (query.keyword as string) || '' })
  return { list: data.list, total: data.total }
}

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive({ name: '', transport: 'sse', url: '', command: '', args: '', headers: '', enabled: true, remark: '' })
const rules: FormRules = { name: [{ required: true, message: '请输入名称', trigger: 'blur' }] }

function openDialog(row?: AiMcpItem) {
  editingId.value = row ? row.id : null
  form.name = row ? row.name : ''
  form.transport = row ? row.transport : 'sse'
  form.url = row ? row.url : ''
  form.command = row ? row.command : ''
  form.args = row ? row.args : ''
  form.headers = row ? row.headers : ''
  form.enabled = row ? row.enabled : true
  form.remark = row ? row.remark : ''
  dialogVisible.value = true
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    await saveAiMcpServer({ id: editingId.value || undefined, ...form })
    ElMessage.success('保存成功')
    dialogVisible.value = false
    pageRef.value?.reload()
  } finally {
    saving.value = false
  }
}

function handleDelete(row: AiMcpItem) {
  ElMessageBox.confirm('确定删除MCP服务「' + row.name + '」吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteAiMcpServer(row.id)
      ElMessage.success('删除成功')
      pageRef.value?.reload()
    })
    .catch(() => {})
}
</script>

<style scoped>
.ai-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
</style>
