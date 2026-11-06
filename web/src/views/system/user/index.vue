<template>
  <div class="page user-page">
    <table-page :columns="columns" :fetch="fetchUsers" :search-fields="searchFields" @add="openDialog()">
      <template #roles="{ row }">
        <el-tag v-for="r in row.roles" :key="r.id" size="small" class="mr-4">{{ r.name }}</el-tag>
      </template>
      <template #status="{ row }">
        <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
      </template>
      <template #createdAt="{ row }">{{ formatTime(row.createdAt) }}</template>
      <template #ops="{ row }">
        <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
        <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑用户' : '新增用户'" width="480px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="用户名" prop="username">
          <el-input v-model="form.username" :disabled="!!editingId" placeholder="登录用户名" />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            show-password
            :placeholder="editingId ? '留空则不修改' : '登录密码'"
          />
        </el-form-item>
        <el-form-item label="昵称" prop="nickname">
          <el-input v-model="form.nickname" placeholder="显示名称" />
        </el-form-item>
        <el-form-item label="角色" prop="roleIds">
          <el-select v-model="form.roleIds" multiple placeholder="选择角色" style="width: 100%">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.enabled" active-text="启用" inactive-text="禁用" />
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
import { createUser, deleteUser, listUsers, updateUser } from '@/api/user'
import type { UserItem } from '@/api/user'
import { listRoles } from '@/api/role'
import type { RoleListItem } from '@/api/role'
import { formatTime } from '@/utils/format'
import { TablePage } from '@/components/table-page'
import type { SearchField, TableQuery } from '@/components/table-page'

const searchFields: SearchField[] = [
  { field: 'keyword', label: '关键词', placeholder: '用户名 / 昵称' },
]

const columns = [
  { type: 'seq', title: '#', width: 60 },
  { field: 'id', title: 'ID', width: 70 },
  { field: 'username', title: '用户名', minWidth: 120 },
  { field: 'nickname', title: '昵称', minWidth: 120 },
  { title: '角色', minWidth: 160, slots: { default: 'roles' } },
  { title: '状态', width: 90, slots: { default: 'status' } },
  { title: '创建时间', width: 170, slots: { default: 'createdAt' } },
  { title: '操作', width: 160, fixed: 'right', slots: { default: 'ops' } },
]

async function fetchUsers(query: TableQuery) {
  const data = await listUsers({
    page: query.page,
    pageSize: query.pageSize,
    keyword: (query.keyword as string) || '',
  })
  return { list: data.list, total: data.total }
}

const roles = ref<RoleListItem[]>([])
const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  username: '',
  password: '',
  nickname: '',
  enabled: true,
  roleIds: [] as number[],
})

const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [
    {
      validator: (_rule: any, value: string, callback: any) => {
        if (!editingId.value && !value) {
          callback(new Error('请输入密码'))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
  nickname: [{ required: true, message: '请输入昵称', trigger: 'blur' }],
}

async function loadRoles() {
  const data = await listRoles()
  roles.value = data.list
}

function openDialog(row?: UserItem) {
  editingId.value = row ? row.id : null
  form.username = row ? row.username : ''
  form.password = ''
  form.nickname = row ? row.nickname : ''
  form.enabled = row ? row.enabled : true
  form.roleIds = row ? row.roles.map((r) => r.id) : []
  dialogVisible.value = true
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    if (editingId.value) {
      await updateUser(editingId.value, {
        nickname: form.nickname,
        password: form.password,
        enabled: form.enabled,
        roleIds: form.roleIds,
      })
      ElMessage.success('修改成功')
    } else {
      await createUser({
        username: form.username,
        password: form.password,
        nickname: form.nickname,
        enabled: form.enabled,
        roleIds: form.roleIds,
      })
      ElMessage.success('新增成功')
    }
    dialogVisible.value = false
  } finally {
    saving.value = false
  }
}

function handleDelete(row: UserItem) {
  ElMessageBox.confirm('确定删除用户 ' + row.username + ' 吗？', '提示', { type: 'warning' })
    .then(async () => {
      await deleteUser(row.id)
      ElMessage.success('删除成功')
    })
    .catch(() => {})
}

onMounted(() => {
  loadRoles()
})
</script>

<style scoped>
.user-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.mr-4 {
  margin-right: 4px;
}
</style>
