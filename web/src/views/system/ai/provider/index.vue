<template>
  <div class="page ai-page">
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchProviders"
      :search-fields="[{ field: 'keyword', label: '关键词', placeholder: '名称 / 模型' }]"
      :toolbar-actions="[{ code: 'add', label: '新增供应商', type: 'primary' }]"
      @add="openDialog()"
    >
      <template #key="{ row }">
        <el-tag v-if="row.apiKey" size="small" type="success">已配置</el-tag>
        <el-tag v-else size="small" type="info">未配置</el-tag>
      </template>
      <template #enabled="{ row }">
        <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
      </template>
      <template #apiMode="{ row }">
        <el-tag :type="row.apiMode === 'responses' ? 'success' : 'warning'" size="small">
          {{ row.apiMode === 'responses' ? 'responses' : 'chat' }}
        </el-tag>
      </template>
      <template #ops="{ row }">
        <el-button link type="success" @click="handleFetchModels(row)">获取模型</el-button>
        <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
        <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑供应商' : '新增供应商'" width="520px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="名称" prop="name"><el-input v-model="form.name" placeholder="如: DeepSeek" /></el-form-item>
        <el-form-item label="接口地址"><el-input v-model="form.baseUrl" placeholder="OpenAI兼容地址, 如 https://api.deepseek.com/v1" /></el-form-item>
        <el-form-item label="API Key"><el-input v-model="form.apiKey" type="password" show-password :placeholder="editingId ? '留空则不修改' : 'sk-...'" /></el-form-item>
        <el-form-item label="API 模式">
          <el-select v-model="form.apiMode" style="width: 100%">
            <el-option label="Responses API (/v1/responses, 默认)" value="responses" />
            <el-option label="Chat Completions (/v1/chat/completions)" value="chat" />
          </el-select>
        </el-form-item>
        <el-form-item label="默认模型" prop="model">
          <el-select v-model="form.model" filterable allow-create default-first-option placeholder="选择或输入模型名" style="width: 100%">
            <el-option v-for="m in form.models" :key="m" :label="m" :value="m" />
          </el-select>
        </el-form-item>
        <el-form-item label="温度"><el-slider v-model="form.temperature" :min="0" :max="2" :step="0.1" show-input style="width: 100%" /></el-form-item>
        <el-form-item label="状态"><el-switch v-model="form.enabled" active-text="启用" inactive-text="禁用" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" placeholder="用途说明" /></el-form-item>
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
import { createAiProvider, deleteAiProvider, fetchAiProviderModels, listAiProviders, updateAiProvider } from '@/api/ai'
import type { AiProviderItem } from '@/api/ai'
import { TablePage } from '@/components/table-page'
import type { TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()

const columns = [
  { field: 'id', title: 'ID', width: 60 },
  { field: 'name', title: '名称', minWidth: 120 },
  { field: 'baseUrl', title: '接口地址', minWidth: 220 },
  { field: 'model', title: '模型', minWidth: 140 },
  { title: '模式', width: 100, slots: { default: 'apiMode' } },
  { field: 'temperature', title: '温度', width: 70 },
  { title: 'Key', width: 90, slots: { default: 'key' } },
  { title: '状态', width: 80, slots: { default: 'enabled' } },
  { field: 'remark', title: '备注', minWidth: 120 },
  { title: '操作', width: 220, fixed: 'right', slots: { default: 'ops' } },
]

async function fetchProviders(query: TableQuery) {
  const data = await listAiProviders({ keyword: (query.keyword as string) || '' })
  return { list: data.list, total: data.total }
}

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  name: '',
  baseUrl: '',
  apiKey: '',
  apiMode: 'responses',
  model: '',
  models: [] as string[],
  temperature: 0.7,
  enabled: true,
  remark: '',
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],

}

function openDialog(row?: AiProviderItem) {
  editingId.value = row ? row.id : null
  form.name = row ? row.name : ''
  form.baseUrl = row ? row.baseUrl : ''
  form.apiKey = ''
  form.apiMode = row ? row.apiMode || 'responses' : 'responses'
  form.model = row ? row.model : ''
  form.models = row && row.models ? row.models : []
  form.temperature = row ? row.temperature : 0.7
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
    if (editingId.value) {
      await updateAiProvider(editingId.value, { ...form })
      ElMessage.success('修改成功')
    } else {
      await createAiProvider({ ...form })
      ElMessage.success('新增成功')
    }
    dialogVisible.value = false
    pageRef.value?.reload()
  } finally {
    saving.value = false
  }
}

async function handleFetchModels(row: AiProviderItem) {
  try {
    const data = await fetchAiProviderModels(row.id)
    ElMessage.success('获取到 ' + data.models.length + ' 个模型')
    pageRef.value?.reload()
  } catch (e: any) {
    ElMessage.error(e && e.message ? e.message : '获取模型列表失败')
  }
}

function handleDelete(row: AiProviderItem) {
  ElMessageBox.confirm('确定删除供应商「' + row.name + '」吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteAiProvider(row.id)
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
