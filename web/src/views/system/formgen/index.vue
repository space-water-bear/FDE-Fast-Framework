<template>
  <div class="page">
    <div class="page-toolbar">
      <span class="panel-title">表单 Schema 编辑器</span>
      <div class="toolbar-spacer" />
      <el-input v-model="objectKey" :disabled="busy" placeholder="objectKey，如 biz_customer" class="toolbar-input object-key-input" clearable />
      <el-button type="primary" :icon="Refresh" :loading="loading" :disabled="busy" @click="loadRemoteSchema">加载草稿</el-button>
      <el-button :icon="RefreshLeft" :disabled="busy" @click="useMockSchema">重置示例</el-button>
      <el-button type="success" :loading="saving" :disabled="!parsedSchema || loading || activating" @click="saveSchema">保存草稿</el-button>
      <el-button :disabled="activeVersion === 0 || !loadedKey" @click="viewActiveSchema">查看已激活版本</el-button>
      <el-button type="warning" :disabled="!canActivate" :loading="activating" @click="activateSchema">激活已保存草稿</el-button>
    </div>
    <el-alert :closable="false" type="info" :title="versionSummary + '。保存草稿不会改变已激活版本。'" />

    <el-row :gutter="12" class="formgen-body" :inert="busy">
      <el-col :span="leftSpan">
        <div class="panel schema-panel">
          <el-tabs v-model="activeTab" class="schema-tabs" stretch>
            <el-tab-pane label="可视化编辑" name="visual">
              <div class="tab-body">
                <SchemaEditor v-if="parsedSchema" :model-value="parsedSchema" @update:model-value="handleEditorUpdate" />
                <el-alert v-else type="error" :closable="false" :title="parseError" />
              </div>
            </el-tab-pane>
            <el-tab-pane label="Schema JSON" name="json">
              <div class="tab-body">
                <div class="page-toolbar json-toolbar">
                  <span class="panel-title">Schema JSON</span>
                  <div class="toolbar-spacer" />
                  <span v-if="parseError" class="schema-error">解析失败：{{ parseError }}</span>
                </div>
                <el-input v-model="schemaText" type="textarea" :rows="22" class="schema-editor" spellcheck="false" />
              </div>
            </el-tab-pane>
          </el-tabs>
        </div>
      </el-col>
      <el-col :span="rightSpan">
        <div class="panel render-panel">
          <div class="page-toolbar">
            <span class="panel-title">草稿预览</span>
            <div class="toolbar-spacer" />
            <el-tag v-if="activeSource === 'remote'" type="success" size="small">草稿 v{{ draftVersion }}{{ dirty ? ' · 未保存修改' : '' }}</el-tag>
            <el-tag v-else type="info" size="small">内置示例</el-tag>
          </div>
          <div class="render-body">
            <FormRender
              v-if="parsedSchema"
              :schema="parsedSchema"
              :model-value="model"
              @update:model-value="handleModelUpdate"
            />
            <el-empty v-else description="暂无可用 schema" />
          </div>
        </div>
      </el-col>
    </el-row>
    <el-dialog v-model="activeVisible" title="当前已激活 Schema（只读）" width="760px">
      <pre class="active-json">{{ activeSchemaText }}</pre>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, RefreshLeft } from '@element-plus/icons-vue'
import { activateFormgenSchema, createFormgenObject, getFormgenSchema, updateFormgenObject } from '@/api/formgen'
import FormRender from '@/components/form-render/FormRender.vue'
import type { FormSchema } from '@/components/form-render/types'
import SchemaEditor from './SchemaEditor.vue'
import { cloneSchema, parseFormSchema, schemaToPayload } from './schema-payload'

const MOCK_SCHEMA: FormSchema = {
  $schema: 'https://fde-admin/schema/form-v1',
  objectKey: 'biz_customer',
  name: '客户',
  version: 2,
  fields: {
    name: {
      title: '客户名称',
      type: 'string',
      required: true,
      minLength: 2,
      maxLength: 64,
      'x-ui': { control: 'input', placeholder: '请输入客户名称', colSpan: 12 },
    },
    level: {
      title: '等级',
      type: 'string',
      enum: ['A', 'B', 'C'],
      'x-ui': {
        control: 'select',
        options: [
          { label: 'A级', value: 'A' },
          { label: 'B级', value: 'B' },
          { label: 'C级', value: 'C' },
        ],
        colSpan: 12,
      },
    },
    balance: {
      title: '余额',
      type: 'number',
      minimum: 0,
      'x-ui': { control: 'number', step: 0.01, colSpan: 12 },
    },
    owner_id: {
      title: '负责人',
      type: 'integer',
      'x-ui': {
        control: 'table-select',
        options: [
          { label: '张三', value: 1, dept: '销售部' },
          { label: '李四', value: 2, dept: '市场部' },
          { label: '王五', value: 3, dept: '研发部' },
        ],
        colSpan: 12,
      },
    },
    active: {
      title: '启用',
      type: 'boolean',
      default: true,
      'x-ui': { control: 'switch', colSpan: 12 },
    },
    ordered_at: {
      title: '下单时间',
      type: 'string',
      format: 'date-time',
      'x-ui': { control: 'datetime', colSpan: 12 },
    },
    gender: {
      title: '性别',
      type: 'string',
      enum: ['male', 'female'],
      'x-ui': {
        control: 'radio',
        options: [
          { label: '男', value: 'male' },
          { label: '女', value: 'female' },
        ],
        colSpan: 12,
      },
    },
    remark: {
      title: '备注',
      type: 'text',
      maxLength: 200,
      'x-ui': { control: 'textarea', colSpan: 24 },
    },
  },
  'x-linkages': [
    { when: { field: 'level', eq: 'A' }, then: { field: 'balance', required: true, visible: true } },
    { when: { field: 'active', eq: false }, then: { field: 'remark', visible: false } },
  ],
}

const objectKey = ref('biz_customer')
const schemaText = ref(JSON.stringify(MOCK_SCHEMA, null, 2))
const model = ref<Record<string, any>>({})
const loading = ref(false)
const saving = ref(false)
const activeSource = ref<'mock' | 'remote'>('mock')
const activeTab = ref('visual')
const loadedKey = ref('')
const draftVersion = ref(0)
const activeVersion = ref(0)
const activating = ref(false)
const savedFingerprint = ref(JSON.stringify(MOCK_SCHEMA))
const activeVisible = ref(false)
const activeSchemaText = ref('')
const busy = computed(() => loading.value || saving.value || activating.value)

const leftSpan = computed(() => (activeTab.value === 'visual' ? 14 : 10))
const rightSpan = computed(() => 24 - leftSpan.value)

const parsedSchema = computed<FormSchema | null>(() => {
  try {
    return parseFormSchema(schemaText.value)
  } catch {
    return null
  }
})

const parseError = computed(() => {
  try {
    parseFormSchema(schemaText.value)
    return ''
  } catch (error) {
    return error instanceof Error ? error.message : String(error)
  }
})

const dirty = computed(() => !parsedSchema.value || JSON.stringify(parsedSchema.value) !== savedFingerprint.value)
const canActivate = computed(() => !!loadedKey.value && draftVersion.value > activeVersion.value
  && !dirty.value && !saving.value && !loading.value && !activating.value)
const versionSummary = computed(() => loadedKey.value
  ? `草稿 v${draftVersion.value}；${activeVersion.value > 0 ? '已激活 v' + activeVersion.value : '尚未激活'}${dirty.value ? '；有未保存修改' : ''}`
  : '当前为未保存示例')

watch(parsedSchema, (schema) => { if (schema) syncModelDefaults(schema) })

function defaultValueFor(field: FormSchema['fields'][string]): any {
  if (field.default !== undefined) return field.default
  switch (field.type) {
    case 'bool':
    case 'boolean':
      return false
    case 'number':
    case 'integer':
    case 'int':
    case 'float':
      return null
    default:
      return ''
  }
}

function buildModel(schema: FormSchema): Record<string, any> {
  const next: Record<string, any> = {}
  for (const [name, field] of Object.entries(schema.fields ?? {})) {
    next[name] = defaultValueFor(field)
  }
  return next
}

function syncModelDefaults(schema: FormSchema): void {
  const next: Record<string, any> = { ...model.value }
  for (const [name, field] of Object.entries(schema.fields ?? {})) {
    if (!(name in next)) {
      next[name] = defaultValueFor(field)
    }
  }
  model.value = next
}

function applySchema(schema: FormSchema, source: 'mock' | 'remote'): void {
  schemaText.value = JSON.stringify(schema, null, 2)
  model.value = buildModel(schema)
  activeSource.value = source
  objectKey.value = schema.objectKey
  loadedKey.value = source === 'remote' ? schema.objectKey : ''
  draftVersion.value = source === 'remote' ? schema.version ?? 0 : 0
  activeVersion.value = source === 'remote' ? schema.activeVersion ?? 0 : 0
  savedFingerprint.value = JSON.stringify(schema)
}

function handleModelUpdate(value: Record<string, any>): void {
  model.value = value
}

function handleEditorUpdate(schema: FormSchema): void {
  schemaText.value = JSON.stringify(schema, null, 2)
  syncModelDefaults(schema)
}

async function confirmDiscard(): Promise<boolean> {
  if (!dirty.value) return true
  try { await ElMessageBox.confirm('将放弃当前未保存的修改，继续？', '切换草稿', { type: 'warning' }); return true } catch { return false }
}

async function useMockSchema(): Promise<void> {
  if (!await confirmDiscard()) return
  applySchema(cloneSchema(MOCK_SCHEMA), 'mock')
  ElMessage.info('已使用内置示例 schema')
}

async function loadRemoteSchema(): Promise<void> {
  const key = objectKey.value.trim()
  if (!key) { ElMessage.warning('请填写要加载的 objectKey'); return }
  if (!await confirmDiscard()) return
  loading.value = true
  try {
    const data = await getFormgenSchema(key, 'draft')
    if (data && data.fields) {
      applySchema(data, 'remote')
      ElMessage.success('已加载草稿，当前已激活版本保持不变')
      return
    }
    ElMessage.error('后端返回的草稿格式不可用')
  } catch {
    // 请求层已展示错误，保留用户当前内容，不用示例覆盖。
  } finally {
    loading.value = false
  }
}

async function saveSchema(): Promise<void> {
  const schema = parsedSchema.value
  if (!schema || !schema.fields) {
    ElMessage.warning('schema 不可用')
    return
  }
  if (!schema.objectKey.trim()) {
    ElMessage.warning('请先填写 objectKey')
    return
  }
  if (!schema.name.trim()) {
    ElMessage.warning('请先填写名称')
    return
  }
  if (Object.keys(schema.fields).length === 0) {
    ElMessage.warning('请至少添加一个字段')
    return
  }
  if (loadedKey.value && loadedKey.value !== schema.objectKey) {
    ElMessage.warning('已加载对象的 objectKey 不可更改；请重置示例后新建其他对象')
    return
  }
  let payload
  try {
    payload = schemaToPayload(schema, loadedKey.value ? 'update' : 'create', draftVersion.value)
  } catch (error) {
    ElMessage.warning((error as Error).message)
    return
  }
  saving.value = true
  try {
    const saved = loadedKey.value
      ? await updateFormgenObject(schema.objectKey, payload)
      : await createFormgenObject(payload)
    applySchema({ ...saved.schema, version: saved.object.version, activeVersion: saved.object.activeVersion }, 'remote')
    ElMessage.success('草稿已保存，尚未激活')
  } catch (error) {
    const status = (error as any)?.response?.status
    if (status === 404) {
      ElMessage.error('对象不存在，请重新加载草稿后重试')
    }
  } finally {
    saving.value = false
  }
}

async function viewActiveSchema(): Promise<void> {
  try {
    const schema = await getFormgenSchema(loadedKey.value)
    activeSchemaText.value = JSON.stringify(schema, null, 2)
    activeVisible.value = true
  } catch { /* 请求层展示错误，保持草稿。 */ }
}

async function activateSchema(): Promise<void> {
  if (!canActivate.value) return
  const key = loadedKey.value
  const version = draftVersion.value
  const expectedActiveVersion = activeVersion.value
  try {
    await ElMessageBox.confirm(`将激活 ${key} 的草稿 v${version}，当前已激活版本为 ${expectedActiveVersion || '无'}。继续？`, '激活草稿', { type: 'warning' })
  } catch { return }
  if (!canActivate.value || loadedKey.value !== key || draftVersion.value !== version) return
  activating.value = true
  try {
    const activated = await activateFormgenSchema(key, { version, expectedActiveVersion })
    activeVersion.value = activated.object.activeVersion
    // 当前编辑内容是已保存的同一草稿，不用较旧的激活版本替换草稿。
    const current = parsedSchema.value
    if (current) applySchema({ ...current, activeVersion: activated.object.activeVersion }, 'remote')
    ElMessage.success(`已激活 v${version}`)
  } catch {
    // 激活失败时保留草稿及原 active 版本，重新加载可获取远端最新状态。
  } finally { activating.value = false }
}

onMounted(() => {
  model.value = buildModel(MOCK_SCHEMA)
})
</script>

<style scoped>
.formgen-body {
  margin-top: 12px;
}
.schema-panel,
.render-panel {
  display: flex;
  flex-direction: column;
  min-height: 560px;
}
.schema-tabs {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.schema-tabs :deep(.el-tabs__content) {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.schema-tabs :deep(.el-tab-pane) {
  height: 100%;
  overflow: hidden;
}
.tab-body {
  height: 100%;
  overflow: auto;
  padding: 8px 4px;
}
.json-toolbar {
  margin-bottom: 6px;
}
.schema-editor {
  flex: 1;
}
.schema-editor :deep(.el-textarea__inner) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
}
.active-json { max-height: 60vh; overflow: auto; white-space: pre-wrap; }
.render-body {
  flex: 1;
  overflow: auto;
  padding: 8px 4px;
}
.schema-error {
  color: var(--el-color-danger);
  font-size: 12px;
}
.object-key-input {
  width: 220px;
}
.save-mode {
  margin-left: 4px;
}
</style>
