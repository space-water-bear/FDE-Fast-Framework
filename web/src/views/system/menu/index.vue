<template>
  <div class="page">
    <div class="page-toolbar">
      <div class="toolbar-spacer" />
      <el-button type="primary" :icon="Plus" @click="openDialog()">新增菜单</el-button>
    </div>

    <el-table
      v-loading="loading"
      :data="menuTree"
      row-key="id"
      :tree-props="{ children: 'children' }"
      default-expand-all
      border
    >
      <el-table-column prop="title" label="菜单名称" min-width="150" />
      <el-table-column label="图标" width="80">
        <template #default="{ row }">
          <el-icon v-if="row.icon"><component :is="iconOf(row.icon)" /></el-icon>
        </template>
      </el-table-column>
      <el-table-column prop="path" label="路由" min-width="110" />
      <el-table-column prop="component" label="组件" min-width="180" />
      <el-table-column label="类型" width="80">
        <template #default="{ row }">
          <el-tag :type="typeTag(row.type)" size="small">{{ typeText(row.type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="接口权限" min-width="170">
        <template #default="{ row }">
          <span v-if="row.apiPath">{{ row.apiMethod }} {{ row.apiPath }}</span>
        </template>
      </el-table-column>
      <el-table-column label="标签配置" width="150">
        <template #default="{ row }">
          <el-tag v-if="row.affix" size="small" class="mr-4">固定</el-tag>
          <el-tag v-if="row.hideTab" size="small" type="info">隐藏标签</el-tag>
          <el-tag v-if="row.keepAlive" size="small" type="warning">缓存</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="sort" label="排序" width="70" />
      <el-table-column label="操作" width="160" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
          <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑菜单' : '新增菜单'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="上级菜单">
          <el-tree-select
            v-model="form.parentId"
            :data="parentOptions"
            check-strictly
            :render-after-expand="false"
            node-key="id"
            :props="treeProps"
            placeholder="不选则为顶级菜单"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="类型" prop="type">
          <el-radio-group v-model="form.type">
            <el-radio :value="0">目录</el-radio>
            <el-radio :value="1">菜单</el-radio>
            <el-radio :value="2">按钮</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="标题" prop="title">
          <el-input v-model="form.title" placeholder="菜单显示名称" />
        </el-form-item>
        <el-form-item v-if="form.type !== 0" label="路由路径">
          <el-input v-model="form.path" placeholder="如: user" />
        </el-form-item>
        <el-form-item v-if="form.type !== 0" label="路由名称">
          <el-input v-model="form.name" placeholder="如: SystemUser" />
        </el-form-item>
        <el-form-item v-if="form.type === 1" label="组件路径">
          <el-input v-model="form.component" placeholder="如: system/user/index" />
        </el-form-item>
        <el-form-item label="图标">
          <el-input v-model="form.icon" placeholder="element-plus 图标名, 如: User" />
        </el-form-item>
        <el-form-item v-if="form.type !== 0" label="接口路径">
          <el-input v-model="form.apiPath" placeholder="如: /api/user/:id (作为 casbin 权限)" />
        </el-form-item>
        <el-form-item v-if="form.type !== 0" label="请求方法">
          <el-select v-model="form.apiMethod" placeholder="无接口权限可留空" clearable style="width: 100%">
            <el-option label="GET" value="GET" />
            <el-option label="POST" value="POST" />
            <el-option label="PUT" value="PUT" />
            <el-option label="DELETE" value="DELETE" />
          </el-select>
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" :max="9999" />
        </el-form-item>
        <el-form-item label="隐藏">
          <el-switch v-model="form.hidden" />
        </el-form-item>
        <el-form-item label="缓存">
          <el-switch v-model="form.keepAlive" />
        </el-form-item>
        <el-form-item v-if="form.type === 1" label="固定标签">
          <el-switch v-model="form.affix" />
          <span class="form-hint">打开后标签栏不可关闭(如首页)</span>
        </el-form-item>
        <el-form-item v-if="form.type === 1" label="隐藏标签">
          <el-switch v-model="form.hideTab" />
          <span class="form-hint">打开后访问该页不生成标签(如详情页)</span>
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
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import * as Icons from '@element-plus/icons-vue'
import { createMenu, deleteMenu, listMenuTree, updateMenu } from '@/api/menu'
import type { MenuForm } from '@/api/menu'
import type { MenuInfo } from '@/api/auth'

const loading = ref(false)
const menuTree = ref<MenuInfo[]>([])
const treeProps = { label: 'title', children: 'children' }

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive<MenuForm>({
  parentId: 0,
  path: '',
  name: '',
  component: '',
  title: '',
  icon: '',
  type: 1,
  sort: 0,
  hidden: false,
  keepAlive: false,
  affix: false,
  hideTab: false,
  apiPath: '',
  apiMethod: '',
})

const rules: FormRules = {
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }],
}

const parentOptions = computed(() => filterButtons(menuTree.value))

function filterButtons(nodes: MenuInfo[]): MenuInfo[] {
  const result: MenuInfo[] = []
  for (const n of nodes) {
    if (n.type === 2) continue
    result.push({ ...n, children: filterButtons(n.children || []) })
  }
  return result
}

function iconOf(name: string) {
  const icons = Icons as unknown as Record<string, unknown>
  return icons[name] || null
}

function typeText(t: number) {
  if (t === 0) return '目录'
  if (t === 1) return '菜单'
  return '按钮'
}

function typeTag(t: number) {
  if (t === 0) return 'warning'
  if (t === 1) return 'success'
  return 'info'
}

async function load() {
  loading.value = true
  try {
    const data = await listMenuTree()
    menuTree.value = data.list
  } finally {
    loading.value = false
  }
}

function openDialog(row?: MenuInfo) {
  editingId.value = row ? row.id : null
  form.parentId = row ? row.parentId : 0
  form.path = row ? row.path : ''
  form.name = row ? row.name : ''
  form.component = row ? row.component : ''
  form.title = row ? row.title : ''
  form.icon = row ? row.icon : ''
  form.type = row ? row.type : 1
  form.sort = row ? row.sort : 0
  form.hidden = row ? row.hidden : false
  form.keepAlive = row ? row.keepAlive : false
  form.affix = row ? !!row.affix : false
  form.hideTab = row ? !!row.hideTab : false
  form.apiPath = row ? row.apiPath : ''
  form.apiMethod = row ? row.apiMethod : ''
  dialogVisible.value = true
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    if (editingId.value) {
      await updateMenu(editingId.value, { ...form })
      ElMessage.success('修改成功')
    } else {
      await createMenu({ ...form })
      ElMessage.success('新增成功')
    }
    dialogVisible.value = false
    load()
  } finally {
    saving.value = false
  }
}

function handleDelete(row: MenuInfo) {
  ElMessageBox.confirm('确定删除菜单 ' + row.title + ' 吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteMenu(row.id)
      ElMessage.success('删除成功')
      load()
    })
    .catch(() => {})
}

onMounted(load)
</script>

<style scoped>
.mr-4 {
  margin-right: 4px;
}
.form-hint {
  margin-left: 8px;
  color: #909399;
  font-size: 12px;
}
</style>
