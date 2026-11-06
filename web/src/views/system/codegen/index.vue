<template>
  <div class="page">
    <el-alert
      type="info"
      :closable="false"
      class="mb-12"
      title="填写表信息与字段后，一键生成后端 CRUD + 前端页面（vxe-table 列表 + 搜索 + 表单弹窗）。生成后需重启后端服务并刷新页面，新菜单将出现在侧边栏。"
    />

    <el-form :inline="true" class="mb-12">
      <el-form-item label="表名" required>
        <el-input v-model="form.tableName" placeholder="如: biz_customer" style="width: 220px" />
      </el-form-item>
      <el-form-item label="业务名称" required>
        <el-input v-model="form.business" placeholder="如: 客户" style="width: 160px" />
      </el-form-item>
      <el-form-item label="父菜单">
        <el-tree-select
          v-model="form.parentMenuId"
          :data="parentOptions"
          check-strictly
          node-key="id"
          :props="treeProps"
          style="width: 240px"
        />
      </el-form-item>
      <el-form-item label="结构体名(自动)">
        <el-input v-model="structName" disabled style="width: 180px" />
      </el-form-item>
    </el-form>

    <div class="page-toolbar">
      <span class="hint">字段定义</span>
      <div class="toolbar-spacer" />
      <el-button type="primary" :icon="Plus" @click="addField">添加字段</el-button>
    </div>

    <el-table :data="form.fields" border>
      <el-table-column label="字段名" min-width="150">
        <template #default="{ row }">
          <el-input v-model="row.name" placeholder="如: order_no" />
        </template>
      </el-table-column>
      <el-table-column label="中文名" min-width="130">
        <template #default="{ row }">
          <el-input v-model="row.title" placeholder="如: 订单号" />
        </template>
      </el-table-column>
      <el-table-column label="类型" width="150">
        <template #default="{ row }">
          <el-select v-model="row.type" style="width: 100%">
            <el-option v-for="t in fieldTypes" :key="t.value" :label="t.label" :value="t.value" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column label="查询" width="70" align="center">
        <template #default="{ row }">
          <el-switch v-model="row.search" />
        </template>
      </el-table-column>
      <el-table-column label="列表" width="70" align="center">
        <template #default="{ row }">
          <el-switch v-model="row.inList" />
        </template>
      </el-table-column>
      <el-table-column label="表单" width="70" align="center">
        <template #default="{ row }">
          <el-switch v-model="row.inForm" />
        </template>
      </el-table-column>
      <el-table-column label="必填" width="70" align="center">
        <template #default="{ row }">
          <el-switch v-model="row.required" />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="80" align="center">
        <template #default="{ $index }">
          <el-button link type="danger" @click="removeField($index)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="page-toolbar mt-12">
      <div class="toolbar-spacer" />
      <el-button :icon="View" @click="handlePreview">预览</el-button>
      <el-button type="primary" :icon="MagicStick" :loading="generating" @click="handleGenerate">生成代码</el-button>
    </div>

    <el-dialog v-model="previewVisible" title="生成预览" width="860px" top="4vh">
      <el-tabs v-model="activeFile">
        <el-tab-pane v-for="f in previewFiles" :key="f.path" :label="f.path.split('/').pop()" :name="f.path">
          <pre class="code-pre">{{ f.content }}</pre>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { MagicStick, Plus, View } from '@element-plus/icons-vue'
import { generateCode, getCodegenOptions, previewCode } from '@/api/codegen'
import type { FieldTypeOption, GenFile, GenFieldDef } from '@/api/codegen'
import type { MenuInfo } from '@/api/auth'

const fieldTypes = ref<FieldTypeOption[]>([])
const parentMenus = ref<MenuInfo[]>([])
const treeProps = { label: 'title', children: 'children' }

const rootNode: MenuInfo = {
  id: 0,
  parentId: 0,
  path: '',
  name: '',
  component: '',
  title: '顶级菜单(不挂父级)',
  icon: '',
  type: 0,
  sort: 0,
  hidden: false,
  keepAlive: false,
  apiPath: '',
  apiMethod: '',
  children: [],
}

const parentOptions = computed<MenuInfo[]>(() => [{ ...rootNode, children: parentMenus.value }])

const form = reactive({
  tableName: '',
  business: '',
  parentMenuId: 0,
  fields: [] as GenFieldDef[],
})

const structName = computed(() => toUpperCamel(form.tableName))

const previewVisible = ref(false)
const previewFiles = ref<GenFile[]>([])
const activeFile = ref('')
const generating = ref(false)

function toUpperCamel(s: string) {
  const t = s.replace(/^(sys_|biz_|t_)/, '')
  return t
    .split('_')
    .filter(Boolean)
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join('')
}

function addField() {
  form.fields.push({ name: '', title: '', type: 'string', search: false, inList: true, inForm: true, required: false })
}

function removeField(index: number) {
  form.fields.splice(index, 1)
}

function validate(): string {
  if (!form.tableName) return '请填写表名'
  if (!/^[a-z][a-z0-9_]*$/.test(form.tableName)) return '表名仅支持小写字母/数字/下划线'
  if (!form.business) return '请填写业务名称'
  if (form.fields.length === 0) return '请至少添加一个字段'
  for (const f of form.fields) {
    if (!/^[a-z][a-z0-9_]*$/.test(f.name)) return '字段名 ' + (f.name || '(空)') + ' 不合法'
    if (!f.title) return '请填写字段 ' + f.name + ' 的中文名'
  }
  return ''
}

function buildRequest() {
  return {
    tableName: form.tableName,
    business: form.business,
    structName: structName.value,
    parentMenuId: form.parentMenuId,
    fields: form.fields.map((f) => ({ ...f })),
  }
}

async function handlePreview() {
  const msg = validate()
  if (msg) {
    ElMessage.warning(msg)
    return
  }
  const data = await previewCode(buildRequest())
  previewFiles.value = data.files
  activeFile.value = data.files.length ? data.files[0].path : ''
  previewVisible.value = true
}

async function handleGenerate() {
  const msg = validate()
  if (msg) {
    ElMessage.warning(msg)
    return
  }
  try {
    await ElMessageBox.confirm('将写入后端/前端源码文件并创建菜单，生成后需要重启后端服务。确认生成？', '确认', {
      type: 'warning',
    })
  } catch {
    return
  }
  generating.value = true
  try {
    const data = await generateCode(buildRequest())
    const fileList = data.files.map((f) => f.path).join(' | ')
    await ElMessageBox.alert('生成成功，已写入文件: ' + fileList + ' 下一步: 重启后端服务(go run .)，然后刷新页面，新菜单将出现在侧边栏。', '生成完成')
  } finally {
    generating.value = false
  }
}

onMounted(async () => {
  const data = await getCodegenOptions()
  fieldTypes.value = data.fieldTypes
  parentMenus.value = data.parentMenus
})
</script>

<style scoped>
.mb-12 {
  margin-bottom: 12px;
}
.mt-12 {
  margin-top: 12px;
}
.hint {
  font-weight: 600;
  color: #303133;
}
.code-pre {
  max-height: 55vh;
  overflow: auto;
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 12px;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
