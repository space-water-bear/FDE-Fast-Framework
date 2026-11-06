<template>
  <div class="page">
    <el-row :gutter="12">
      <el-col :span="9">
        <div class="panel">
          <div class="page-toolbar">
            <span class="panel-title">字典类型</span>
            <div class="toolbar-spacer" />
            <el-button type="primary" size="small" :icon="Plus" @click="openTypeDialog()">新增类型</el-button>
          </div>
          <el-table :data="types" border size="small" highlight-current-row @current-change="handleSelectType">
            <el-table-column prop="name" label="名称" min-width="90" />
            <el-table-column prop="code" label="编码" min-width="100" />
            <el-table-column prop="remark" label="备注" min-width="110" show-overflow-tooltip />
            <el-table-column label="操作" width="110" align="center">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click.stop="openTypeDialog(row)">编辑</el-button>
                <el-button link type="danger" size="small" @click.stop="handleDeleteType(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-col>
      <el-col :span="15">
        <div class="panel">
          <div class="page-toolbar">
            <span class="panel-title">字典数据（{{ currentType ? currentType.name : '请选择左侧类型' }}）</span>
            <div class="toolbar-spacer" />
            <el-button type="primary" size="small" :icon="Plus" :disabled="!currentType" @click="openDataDialog()">
              新增数据
            </el-button>
          </div>
          <el-table v-loading="dataLoading" :data="dataList" border size="small">
            <el-table-column prop="label" label="标签" min-width="90" />
            <el-table-column prop="value" label="键值" min-width="90" />
            <el-table-column prop="sort" label="排序" width="70" align="center" />
            <el-table-column label="状态" width="80" align="center">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="remark" label="备注" min-width="110" show-overflow-tooltip />
            <el-table-column label="操作" width="110" align="center">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="openDataDialog(row)">编辑</el-button>
                <el-button link type="danger" size="small" @click="handleDeleteData(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-col>
    </el-row>

    <el-dialog
      v-model="typeDialogVisible"
      :title="editingTypeId ? '编辑字典类型' : '新增字典类型'"
      width="420px"
      destroy-on-close
    >
      <el-form ref="typeFormRef" :model="typeForm" :rules="typeRules" label-width="70px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="typeForm.name" placeholder="如: 性别" />
        </el-form-item>
        <el-form-item label="编码" prop="code">
          <el-input v-model="typeForm.code" placeholder="如: gender" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="typeForm.remark" placeholder="用途说明" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="typeDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSaveType">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="dataDialogVisible"
      :title="editingDataId ? '编辑字典数据' : '新增字典数据'"
      width="440px"
      destroy-on-close
    >
      <el-form ref="dataFormRef" :model="dataForm" :rules="dataRules" label-width="70px">
        <el-form-item label="标签" prop="label">
          <el-input v-model="dataForm.label" placeholder="如: 男" />
        </el-form-item>
        <el-form-item label="键值" prop="value">
          <el-input v-model="dataForm.value" placeholder="如: 1" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="dataForm.sort" :min="0" style="width: 100%" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="dataForm.enabled" active-text="启用" inactive-text="禁用" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="dataForm.remark" placeholder="补充说明" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dataDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSaveData">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import {
  createDictData,
  createDictType,
  deleteDictData,
  deleteDictType,
  listDictData,
  listDictTypes,
  updateDictData,
  updateDictType,
} from '@/api/dict'
import type { DictDataItem, DictTypeItem } from '@/api/dict'

const types = ref<DictTypeItem[]>([])
const currentType = ref<DictTypeItem | null>(null)
const dataList = ref<DictDataItem[]>([])
const dataLoading = ref(false)
const saving = ref(false)

const typeDialogVisible = ref(false)
const dataDialogVisible = ref(false)
const editingTypeId = ref<number | null>(null)
const editingDataId = ref<number | null>(null)
const typeFormRef = ref<FormInstance>()
const dataFormRef = ref<FormInstance>()

const typeForm = reactive({ name: '', code: '', remark: '' })
const dataForm = reactive({ label: '', value: '', sort: 0, enabled: true, remark: '' })

const typeRules: FormRules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  code: [{ required: true, message: '请输入编码', trigger: 'blur' }],
}
const dataRules: FormRules = {
  label: [{ required: true, message: '请输入标签', trigger: 'blur' }],
  value: [{ required: true, message: '请输入键值', trigger: 'blur' }],
}

async function loadTypes(keepId?: number) {
  const data = await listDictTypes()
  types.value = data.list
  if (keepId) {
    const found = types.value.find((t) => t.id === keepId) || null
    currentType.value = found
    if (found) {
      loadData(found.id)
    } else {
      dataList.value = []
    }
  }
}

async function loadData(typeId: number) {
  dataLoading.value = true
  try {
    const data = await listDictData(typeId)
    dataList.value = data.list
  } finally {
    dataLoading.value = false
  }
}

function handleSelectType(row: DictTypeItem | null) {
  currentType.value = row
  dataList.value = []
  if (row) {
    loadData(row.id)
  }
}

function openTypeDialog(row?: DictTypeItem) {
  editingTypeId.value = row ? row.id : null
  typeForm.name = row ? row.name : ''
  typeForm.code = row ? row.code : ''
  typeForm.remark = row ? row.remark : ''
  typeDialogVisible.value = true
}

async function handleSaveType() {
  if (!typeFormRef.value) return
  const valid = await typeFormRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    if (editingTypeId.value) {
      await updateDictType(editingTypeId.value, { ...typeForm })
      ElMessage.success('修改成功')
    } else {
      await createDictType({ ...typeForm })
      ElMessage.success('新增成功')
    }
    typeDialogVisible.value = false
    loadTypes(currentType.value ? currentType.value.id : undefined)
  } finally {
    saving.value = false
  }
}

function handleDeleteType(row: DictTypeItem) {
  ElMessageBox.confirm('删除类型「' + row.name + '」将同时删除其全部字典数据，确定吗？', '提示', {
    type: 'warning',
  })
    .then(async () => {
      await deleteDictType(row.id)
      ElMessage.success('删除成功')
      const keep = currentType.value && currentType.value.id === row.id ? undefined : currentType.value?.id
      loadTypes(keep)
    })
    .catch(() => {})
}

function openDataDialog(row?: DictDataItem) {
  if (!currentType.value) return
  editingDataId.value = row ? row.id : null
  dataForm.label = row ? row.label : ''
  dataForm.value = row ? row.value : ''
  dataForm.sort = row ? row.sort : 0
  dataForm.enabled = row ? row.enabled : true
  dataForm.remark = row ? row.remark : ''
  dataDialogVisible.value = true
}

async function handleSaveData() {
  if (!dataFormRef.value || !currentType.value) return
  const valid = await dataFormRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    const payload = {
      typeId: currentType.value.id,
      label: dataForm.label,
      value: dataForm.value,
      sort: dataForm.sort,
      enabled: dataForm.enabled,
      remark: dataForm.remark,
    }
    if (editingDataId.value) {
      await updateDictData(editingDataId.value, payload)
      ElMessage.success('修改成功')
    } else {
      await createDictData(payload)
      ElMessage.success('新增成功')
    }
    dataDialogVisible.value = false
    loadData(currentType.value.id)
  } finally {
    saving.value = false
  }
}

function handleDeleteData(row: DictDataItem) {
  ElMessageBox.confirm('确定删除字典数据「' + row.label + '」吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteDictData(row.id)
      ElMessage.success('删除成功')
      if (currentType.value) {
        loadData(currentType.value.id)
      }
    })
    .catch(() => {})
}

onMounted(() => {
  loadTypes()
})
</script>

<style scoped>
.panel {
  min-height: 480px;
}
.panel-title {
  font-weight: 600;
  color: #303133;
}
</style>
