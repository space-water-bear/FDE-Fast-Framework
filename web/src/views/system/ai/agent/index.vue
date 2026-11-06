<template>
  <div class="page ai-page">
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchAgents"
      :search-fields="[{ field: 'keyword', label: '关键词', placeholder: '智能体名称' }]"
      :toolbar-actions="[{ code: 'add', label: '新增智能体', type: 'primary' }]"
      @add="openDialog()"
    >
      <template #provider="{ row }">{{ providerName(row.providerId) }}</template>
      <template #enabled="{ row }">
        <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
      </template>
      <template #ops="{ row }">
        <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
        <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑智能体' : '新增智能体'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="名称" prop="name"><el-input v-model="form.name" placeholder="如: 客服助手" /></el-form-item>
        <el-form-item label="供应商" prop="providerId">
          <el-select v-model="form.providerId" placeholder="选择供应商" style="width: 100%">
            <el-option v-for="p in providers" :key="p.id" :label="p.name + ' (' + p.model + ')'" :value="p.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="系统提示词"><el-input v-model="form.systemPrompt" type="textarea" :rows="5" placeholder="设定智能体角色与行为" /></el-form-item>
        <el-form-item label="技能">
          <el-select v-model="form.skillIds" multiple placeholder="选择技能(提示词自动注入系统提示)" style="width: 100%">
            <el-option v-for="s in skills" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="工具">
          <el-select v-model="form.toolIds" multiple placeholder="选择工具(模型可调用)" style="width: 100%">
            <el-option v-for="t in tools" :key="t.id" :label="t.name" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="MCP服务">
          <el-select v-model="form.mcpIds" multiple placeholder="选择MCP服务(记录绑定)" style="width: 100%">
            <el-option v-for="m in mcps" :key="m.id" :label="m.name" :value="m.id" />
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
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import {
  createAiAgent,
  deleteAiAgent,
  listAiAgents,
  listAiMcpServers,
  listAiProviders,
  listAiSkills,
  listAiTools,
  updateAiAgent,
} from '@/api/ai'
import type { AiAgentItem, AiMcpItem, AiProviderItem, AiSkillItem, AiToolItem } from '@/api/ai'
import { TablePage } from '@/components/table-page'
import type { TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()
const providers = ref<AiProviderItem[]>([])
const skills = ref<AiSkillItem[]>([])
const tools = ref<AiToolItem[]>([])
const mcps = ref<AiMcpItem[]>([])

const columns = [
  { field: 'id', title: 'ID', width: 60 },
  { field: 'name', title: '名称', minWidth: 120 },
  { title: '供应商', minWidth: 180, slots: { default: 'provider' } },
  { field: 'temperature', title: '温度', width: 70 },
  { title: '状态', width: 80, slots: { default: 'enabled' } },
  { field: 'remark', title: '备注', minWidth: 120 },
  { title: '操作', width: 130, fixed: 'right', slots: { default: 'ops' } },
]

function providerName(id: number) {
  const p = providers.value.find((x) => x.id === id)
  return p ? p.name + ' (' + p.model + ')' : '未配置'
}

async function fetchAgents(query: TableQuery) {
  const data = await listAiAgents({ keyword: (query.keyword as string) || '' })
  return { list: data.list, total: data.total }
}

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  name: '',
  providerId: 0,
  systemPrompt: '',
  temperature: 0.7,
  enabled: true,
  remark: '',
  skillIds: [] as number[],
  toolIds: [] as number[],
  mcpIds: [] as number[],
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  providerId: [{ required: true, message: '请选择供应商', trigger: 'change' }],
}

function openDialog(row?: AiAgentItem) {
  editingId.value = row ? row.id : null
  form.name = row ? row.name : ''
  form.providerId = row ? row.providerId : 0
  form.systemPrompt = row ? row.systemPrompt : ''
  form.temperature = row ? row.temperature : 0.7
  form.enabled = row ? row.enabled : true
  form.remark = row ? row.remark : ''
  form.skillIds = row && row.skills ? row.skills.map((s) => s.id) : []
  form.toolIds = row && row.tools ? row.tools.map((t) => t.id) : []
  form.mcpIds = row && row.mcpServers ? row.mcpServers.map((m) => m.id) : []
  dialogVisible.value = true
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    if (editingId.value) {
      await updateAiAgent(editingId.value, { ...form })
      ElMessage.success('修改成功')
    } else {
      await createAiAgent({ ...form })
      ElMessage.success('新增成功')
    }
    dialogVisible.value = false
    pageRef.value?.reload()
  } finally {
    saving.value = false
  }
}

function handleDelete(row: AiAgentItem) {
  ElMessageBox.confirm('确定删除智能体「' + row.name + '」吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteAiAgent(row.id)
      ElMessage.success('删除成功')
      pageRef.value?.reload()
    })
    .catch(() => {})
}

onMounted(async () => {
  const [ps, ss, ts, ms] = await Promise.all([
    listAiProviders(),
    listAiSkills(),
    listAiTools(),
    listAiMcpServers(),
  ])
  providers.value = ps.list
  skills.value = ss.list
  tools.value = ts.list
  mcps.value = ms.list
})
</script>

<style scoped>
.ai-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
</style>
