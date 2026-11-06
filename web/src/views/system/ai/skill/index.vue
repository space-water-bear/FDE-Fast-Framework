<template>
  <div class="page ai-page">
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchSkills"
      :search-fields="[{ field: 'keyword', label: '关键词', placeholder: '名称 / 描述' }]"
      :toolbar-actions="[{ code: 'add', label: '新增技能', type: 'primary' }]"
      @add="openDialog()"
    >
      <template #ops="{ row }">
        <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
        <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑技能' : '新增技能'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="名称" prop="name"><el-input v-model="form.name" placeholder="如: 翻译" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" placeholder="技能用途" /></el-form-item>
        <el-form-item label="提示词"><el-input v-model="form.prompt" type="textarea" :rows="6" placeholder="技能提示词/流程说明" /></el-form-item>
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
import { deleteAiSkill, listAiSkills, saveAiSkill } from '@/api/ai'
import type { AiSkillItem } from '@/api/ai'
import { TablePage } from '@/components/table-page'
import type { TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()

const columns = [
  { field: 'id', title: 'ID', width: 60 },
  { field: 'name', title: '名称', minWidth: 120 },
  { field: 'description', title: '描述', minWidth: 200 },
  { field: 'remark', title: '备注', minWidth: 120 },
  { title: '操作', width: 130, fixed: 'right', slots: { default: 'ops' } },
]

async function fetchSkills(query: TableQuery) {
  const data = await listAiSkills({ keyword: (query.keyword as string) || '' })
  return { list: data.list, total: data.total }
}

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive({ name: '', description: '', prompt: '', remark: '' })
const rules: FormRules = { name: [{ required: true, message: '请输入名称', trigger: 'blur' }] }

function openDialog(row?: AiSkillItem) {
  editingId.value = row ? row.id : null
  form.name = row ? row.name : ''
  form.description = row ? row.description : ''
  form.prompt = row ? row.prompt : ''
  form.remark = row ? row.remark : ''
  dialogVisible.value = true
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    await saveAiSkill({ id: editingId.value || undefined, ...form })
    ElMessage.success('保存成功')
    dialogVisible.value = false
    pageRef.value?.reload()
  } finally {
    saving.value = false
  }
}

function handleDelete(row: AiSkillItem) {
  ElMessageBox.confirm('确定删除技能「' + row.name + '」吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteAiSkill(row.id)
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
