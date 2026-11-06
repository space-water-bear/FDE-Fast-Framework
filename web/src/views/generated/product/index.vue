<template>
  <div class="page">
    <div class="page-toolbar">
      <el-input v-model="query.name" placeholder="产品名称" clearable class="toolbar-input" @keyup.enter="handleSearch" @clear="handleSearch" />
      <el-input-number v-model="query.stock" :controls="false" placeholder="库存" class="toolbar-input" />
      <el-button type="primary" :icon="Search" @click="handleSearch">搜索</el-button>
      <el-button :icon="Refresh" @click="handleReset">重置</el-button>
      <div class="toolbar-spacer" />
      <el-button type="primary" :icon="Plus" @click="openDialog()">新增产品</el-button>
    </div>

    <div v-loading="loading">
      <vxe-table :data="list" height="auto" align="center" :row-config="{ isHover: true }">
        <vxe-column type="seq" title="#" width="60" />
        <vxe-column field="name" title="产品名称" min-width="120" />
        <vxe-column field="price" title="价格" min-width="120" />
        <vxe-column field="stock" title="库存" min-width="120" />
        <vxe-column title="是否上架" width="100">
          <template #default="{ row }">
            <el-tag :type="row.onSale ? 'success' : 'info'" size="small">{{ row.onSale ? '启用' : '禁用' }}</el-tag>
          </template>
        </vxe-column>
        <vxe-column title="上架时间" width="170">
          <template #default="{ row }">{{ formatTime(row.releasedAt) }}</template>
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

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑产品' : '新增产品'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
        <el-form-item label="产品名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入产品名称" />
        </el-form-item>
        <el-form-item label="价格" prop="price">
          <el-input-number v-model="form.price" :controls="false" placeholder="请输入价格" style="width: 100%" />
        </el-form-item>
        <el-form-item label="库存" prop="stock">
          <el-input-number v-model="form.stock" :controls="false" placeholder="请输入库存" style="width: 100%" />
        </el-form-item>
        <el-form-item label="是否上架">
          <el-switch v-model="form.onSale" active-text="启用" inactive-text="禁用" />
        </el-form-item>
        <el-form-item label="上架时间" prop="releasedAt">
          <el-date-picker v-model="form.releasedAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" placeholder="请选择上架时间" style="width: 100%" />
        </el-form-item>
        <el-form-item label="备注" prop="remark">
          <el-input v-model="form.remark" type="textarea" :rows="3" placeholder="请输入备注" />
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
import { createProduct, deleteProduct, listProducts, updateProduct } from '@/api/product'
import type { ProductItem } from '@/api/product'
import { formatTime } from '@/utils/format'

const loading = ref(false)
const list = ref<ProductItem[]>([])
const total = ref(0)
const query = reactive<Record<string, any>>({
  page: 1,
  pageSize: 10,
  name: '',
  stock: null,
})

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive<Record<string, any>>({
  name: '',
  price: null,
  stock: null,
  onSale: false,
  releasedAt: '',
  remark: '',
})

const rules: FormRules = {
  name: [{ required: true, message: '请填写产品名称', trigger: 'blur' }],
  price: [{ required: true, message: '请填写价格', trigger: 'blur' }],
}

function buildParams() {
  const params: Record<string, any> = { page: query.page, pageSize: query.pageSize }
  if (query.name) {
    params.name = query.name
  }
  if (query.stock !== null && query.stock !== undefined) {
    params.stock = query.stock
  }
  return params
}

async function load() {
  loading.value = true
  try {
    const data = await listProducts(buildParams())
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
  query.stock = null
  query.page = 1
  load()
}

function openDialog(row?: ProductItem) {
  editingId.value = row ? row.id : null
  form.name = row ? (row.name ?? '') : ''
  form.price = row ? (row.price ?? null) : null
  form.stock = row ? (row.stock ?? null) : null
  form.onSale = row ? (row.onSale ?? false) : false
  form.releasedAt = row ? (row.releasedAt ?? '') : ''
  form.remark = row ? (row.remark ?? '') : ''
  dialogVisible.value = true
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    if (editingId.value) {
      await updateProduct(editingId.value, { ...form })
      ElMessage.success('修改成功')
    } else {
      await createProduct({ ...form })
      ElMessage.success('新增成功')
    }
    dialogVisible.value = false
    load()
  } finally {
    saving.value = false
  }
}

function handleDelete(row: ProductItem) {
  ElMessageBox.confirm('确定删除该产品记录吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteProduct(row.id)
      ElMessage.success('删除成功')
      load()
    })
    .catch(() => {})
}

onMounted(load)
</script>
