<template>
  <div class="page">
    <div class="page-toolbar">
      <el-input v-model="query.name" placeholder="客户名称" clearable class="toolbar-input" @keyup.enter="handleSearch" @clear="handleSearch" />
      <el-input-number v-model="query.age" :controls="false" placeholder="年龄" class="toolbar-input" />
      <el-button type="primary" :icon="Search" @click="handleSearch">搜索</el-button>
      <el-button :icon="Refresh" @click="handleReset">重置</el-button>
      <div class="toolbar-spacer" />
      <el-button type="primary" :icon="Plus" @click="openDialog()">新增客户</el-button>
    </div>

    <div v-loading="loading">
      <vxe-table :data="list" height="auto" align="center" :row-config="{ isHover: true }">
        <vxe-column type="seq" title="#" width="60" />
        <vxe-column field="name" title="客户名称" min-width="120" />
        <vxe-column field="age" title="年龄" min-width="120" />
        <vxe-column field="email" title="邮箱" min-width="120" />
        <vxe-column title="是否启用" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
          </template>
        </vxe-column>
        <vxe-column title="下单时间" width="170">
          <template #default="{ row }">{{ formatTime(row.orderedAt) }}</template>
        </vxe-column>
        <vxe-column title="创建时间" width="170">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </vxe-column>
        <vxe-column title="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
            <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
          </template>
        </vxe-column>
      </vxe-table>
    </div>

    <div class="page-pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.pageSize"
        background
        layout="total, sizes, prev, pager, next"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        @change="load"
      />
    </div>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑客户' : '新增客户'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
        <el-form-item label="客户名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入客户名称" />
        </el-form-item>
        <el-form-item label="年龄" prop="age">
          <el-input-number v-model="form.age" :controls="false" placeholder="请输入年龄" style="width: 100%" />
        </el-form-item>
        <el-form-item label="邮箱" prop="email">
          <el-input v-model="form.email" placeholder="请输入邮箱" />
        </el-form-item>
        <el-form-item label="是否启用">
          <el-switch v-model="form.enabled" active-text="启用" inactive-text="禁用" />
        </el-form-item>
        <el-form-item label="备注" prop="remark">
          <el-input v-model="form.remark" type="textarea" :rows="3" placeholder="请输入备注" />
        </el-form-item>
        <el-form-item label="下单时间" prop="orderedAt">
          <el-date-picker v-model="form.orderedAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" placeholder="请选择下单时间" style="width: 100%" />
        </el-form-item>
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
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { createCustomer, deleteCustomer, listCustomers, updateCustomer } from '@/api/customer'
import type { CustomerItem } from '@/api/customer'
import { formatTime } from '@/utils/format'

const loading = ref(false)
const list = ref<CustomerItem[]>([])
const total = ref(0)
const query = reactive<Record<string, any>>({
  page: 1,
  pageSize: 10,
  name: '',
  age: null,
})

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive<Record<string, any>>({
  name: '',
  age: null,
  email: '',
  enabled: false,
  remark: '',
  orderedAt: '',
})

const rules: FormRules = {
  name: [{ required: true, message: '请填写客户名称', trigger: 'blur' }],
}

function buildParams() {
  const params: Record<string, any> = { page: query.page, pageSize: query.pageSize }
  if (query.name) {
    params.name = query.name
  }
  if (query.age !== null && query.age !== undefined) {
    params.age = query.age
  }
  return params
}

async function load() {
  loading.value = true
  try {
    const data = await listCustomers(buildParams())
    list.value = data.list
    total.value = data.total
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  query.page = 1
  load()
}

function handleReset() {
  query.name = ''
  query.age = null
  query.page = 1
  load()
}

function openDialog(row?: CustomerItem) {
  editingId.value = row ? row.id : null
  form.name = row ? (row.name ?? '') : ''
  form.age = row ? (row.age ?? null) : null
  form.email = row ? (row.email ?? '') : ''
  form.enabled = row ? (row.enabled ?? false) : false
  form.remark = row ? (row.remark ?? '') : ''
  form.orderedAt = row ? (row.orderedAt ?? '') : ''
  dialogVisible.value = true
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    if (editingId.value) {
      await updateCustomer(editingId.value, { ...form })
      ElMessage.success('修改成功')
    } else {
      await createCustomer({ ...form })
      ElMessage.success('新增成功')
    }
    dialogVisible.value = false
    load()
  } finally {
    saving.value = false
  }
}

function handleDelete(row: CustomerItem) {
  ElMessageBox.confirm('确定删除该客户记录吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteCustomer(row.id)
      ElMessage.success('删除成功')
      load()
    })
    .catch(() => {})
}

onMounted(load)
</script>
