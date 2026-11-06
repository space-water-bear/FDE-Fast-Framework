<template>
  <div class="page">
    <el-alert
      type="info"
      :closable="false"
      class="mb-12"
      title="先预览后端 CRUD 与前端页面，再应用该预览。修改任何生成参数后须重新预览；应用后需重新编译后端并构建前端。"
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
      <el-button :icon="View" :loading="previewing" :disabled="generating" @click="handlePreview">预览</el-button>
      <el-button type="primary" :icon="MagicStick" :loading="generating" :disabled="!canApply" @click="handleGenerate">应用此预览</el-button>
    </div>

    <el-dialog v-model="previewVisible" title="生成预览" width="860px" top="4vh">
      <template v-if="preview">
        <p>批次：{{ preview.batchId }}</p>
        <el-alert v-for="warning in preview.warnings ?? []" :key="warning" :title="warning" type="warning" :closable="false" />
        <details><summary>文件差异与菜单计划</summary><pre>{{ JSON.stringify({ diff: preview.diff, menuPlan: preview.menuPlan }, null, 2) }}</pre></details>
      </template>
      <el-alert v-else title="参数已修改，此预览已失效，请重新预览。" type="warning" :closable="false" />
      <el-tabs v-model="activeFile">
        <el-tab-pane v-for="f in previewFiles" :key="f.path" :label="f.path.split('/').pop()" :name="f.path">
          <pre class="code-pre">{{ f.content }}</pre>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <el-button @click="previewVisible = false">关闭</el-button>
        <el-button type="primary" :disabled="!canApply" :loading="generating" @click="handleGenerate">应用此预览</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { MagicStick, Plus, View } from '@element-plus/icons-vue'
import { generateCode, getCodegenOptions, previewCode } from '@/api/codegen'
import type { FieldTypeOption, GenFieldDef, GenPreview } from '@/api/codegen'
import type { MenuInfo } from '@/api/auth'
import { canApplyPreview, requestFingerprint } from './preview-state'

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
const preview = ref<GenPreview | null>(null)
const previewFiles = computed(() => preview.value?.files ?? [])
const previewFingerprint = ref('')
const activeFile = ref('')
const generating = ref(false)
const previewing = ref(false)
const currentFingerprint = computed(() => requestFingerprint(buildRequest()))
const canApply = computed(() => !generating.value && !previewing.value
  && canApplyPreview(preview.value, previewFingerprint.value, currentFingerprint.value))

watch(currentFingerprint, () => { preview.value = null; previewFingerprint.value = '' }, { flush: 'sync' })

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
  const request = buildRequest()
  const fingerprint = requestFingerprint(request)
  preview.value = null
  previewFingerprint.value = ''
  previewing.value = true
  try {
    const data = await previewCode(request)
    if (fingerprint !== currentFingerprint.value) {
      ElMessage.warning('生成参数已改变，请重新预览')
      return
    }
    if (!data.ok || !data.batchId) {
      ElMessage.error(data.error?.msg ?? '预览失败，未生成可应用的批次')
      return
    }
    preview.value = data
    previewFingerprint.value = fingerprint
    activeFile.value = data.files.length ? data.files[0].path : ''
    previewVisible.value = true
  } catch {
    // 请求层已展示错误，失败请求不留下可应用批次。
  } finally { previewing.value = false }
}

async function handleGenerate() {
  if (!canApply.value || !preview.value) {
    ElMessage.warning('请先生成有效预览')
    return
  }
  const batchId = preview.value.batchId
  try {
    await ElMessageBox.confirm('将应用已展示批次 ' + batchId + '，写入源码并创建菜单和权限。确认应用此预览？', '应用预览', {
      type: 'warning',
    })
  } catch {
    return
  }
  if (!canApply.value || preview.value?.batchId !== batchId) {
    ElMessage.warning('预览已失效，请重新预览')
    return
  }
  generating.value = true
  try {
    const data = await generateCode({ batchId, approve: true })
    if (!data.ok) {
      ElMessage.error(data.error?.msg ?? '应用失败，请重新预览并查看冲突')
      return
    }
    previewVisible.value = false
    const fileList = (data.applied ?? []).map((f) => f.path).join(' | ')
    await ElMessageBox.alert('已写入文件: ' + fileList + '。请重新编译后端并构建前端后验证业务页面。', '应用完成')
  } catch {
    // 请求层展示执行错误；无论结果如何，本次预览都必须重新获取。
  } finally {
    preview.value = null
    previewFingerprint.value = ''
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
