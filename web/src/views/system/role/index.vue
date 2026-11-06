<template>
  <div class="page role-page">
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchRoles"
      :search-fields="[]"
      :toolbar-actions="[{ code: 'add', label: '新增角色', type: 'primary' }]"
      @add="openDialog()"
    >
      <template #menuCount="{ row }">{{ row.menus.length }}</template>
      <template #status="{ row }">
        <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
      </template>
      <template #ops="{ row }">
        <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
        <el-button link type="danger" :disabled="row.code === 'admin'" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑角色' : '新增角色'" width="520px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="角色名称" prop="name">
          <el-input v-model="form.name" placeholder="如: 运营人员" />
        </el-form-item>
        <el-form-item label="角色编码" prop="code">
          <el-input v-model="form.code" :disabled="!!editingId" placeholder="如: operator" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" placeholder="角色职责说明" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.enabled" active-text="启用" inactive-text="禁用" />
        </el-form-item>
        <el-form-item label="菜单权限">
          <div class="tree-box">
            <el-tree
              ref="treeRef"
              :data="menuTree"
              show-checkbox
              node-key="id"
              default-expand-all
              :props="treeProps"
            />
          </div>
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
import { nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { ElTree } from 'element-plus'
import { createRole, deleteRole, listRoles, updateRole } from '@/api/role'
import type { RoleListItem } from '@/api/role'
import { listMenuTree } from '@/api/menu'
import type { MenuInfo } from '@/api/auth'
import { TablePage } from '@/components/table-page'
import type { TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()
const menuTree = ref<MenuInfo[]>([])
const treeProps = { label: 'title', children: 'children' }

const columns = [
  { type: 'seq', title: '#', width: 60 },
  { field: 'id', title: 'ID', width: 70 },
  { field: 'name', title: '角色名称', minWidth: 120 },
  { field: 'code', title: '角色编码', minWidth: 110 },
  { field: 'description', title: '描述', minWidth: 160 },
  { title: '菜单数', width: 90, slots: { default: 'menuCount' } },
  { title: '状态', width: 90, slots: { default: 'status' } },
  { title: '操作', width: 160, fixed: 'right', slots: { default: 'ops' } },
]

async function fetchRoles(_query: TableQuery) {
  const data = await listRoles()
  return { list: data.list, total: data.list.length }
}

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()
const treeRef = ref<InstanceType<typeof ElTree>>()

const form = reactive({
  name: '',
  code: '',
  description: '',
  enabled: true,
  menuIds: [] as number[],
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入角色名称', trigger: 'blur' }],
  code: [{ required: true, message: '请输入角色编码', trigger: 'blur' }],
}

async function loadMenuTree() {
  const data = await listMenuTree()
  menuTree.value = data.list
}

async function openDialog(row?: RoleListItem) {
  editingId.value = row ? row.id : null
  form.name = row ? row.name : ''
  form.code = row ? row.code : ''
  form.description = row ? row.description : ''
  form.enabled = row ? row.enabled : true
  dialogVisible.value = true
  await nextTick()
  if (row) {
    treeRef.value?.setCheckedKeys(row.menus.map((m) => m.id))
  } else {
    treeRef.value?.setCheckedKeys([])
  }
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  const checked = (treeRef.value?.getCheckedKeys() || []) as number[]
  saving.value = true
  try {
    if (editingId.value) {
      await updateRole(editingId.value, {
        name: form.name,
        description: form.description,
        enabled: form.enabled,
        menuIds: checked,
      })
      ElMessage.success('修改成功')
    } else {
      await createRole({
        name: form.name,
        code: form.code,
        description: form.description,
        enabled: form.enabled,
        menuIds: checked,
      })
      ElMessage.success('新增成功')
    }
    dialogVisible.value = false
    pageRef.value?.reload()
  } finally {
    saving.value = false
  }
}

function handleDelete(row: RoleListItem) {
  ElMessageBox.confirm('确定删除角色 ' + row.name + ' 吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteRole(row.id)
      ElMessage.success('删除成功')
      pageRef.value?.reload()
    })
    .catch(() => {})
}

onMounted(() => {
  loadMenuTree()
})
</script>

<style scoped>
.role-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
</style>
