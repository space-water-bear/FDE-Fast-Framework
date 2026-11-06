<template>
  <div class="page">
    <div class="page-toolbar">
      <span class="panel-title">表单 Schema 编辑器</span>
      <div class="toolbar-spacer" />
      <el-input v-model="objectKey" placeholder="objectKey，如 biz_customer" class="toolbar-input object-key-input" clearable />
      <el-button type="primary" :icon="Refresh" :loading="loading" @click="loadRemoteSchema">拉取 schema</el-button>
      <el-button :icon="RefreshLeft" @click="useMockSchema">重置示例</el-button>
      <el-radio-group v-model="saveMode" size="small" class="save-mode">
        <el-radio-button value="create">新建 POST</el-radio-button>
        <el-radio-button value="update">更新 PUT</el-radio-button>
      </el-radio-group>
      <el-button type="success" :loading="saving" @click="saveSchema">保存</el-button>
    </div>

    <el-row :gutter="12" class="formgen-body">
      <el-col :span="leftSpan">
        <div class="panel schema-panel">
          <el-tabs v-model="activeTab" class="schema-tabs" stretch>
            <el-tab-pane label="可视化编辑" name="visual">
              <div class="tab-body">
                <SchemaEditor :model-value="currentSchema" @update:model-value="handleEditorUpdate" />
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
            <span class="panel-title">实时渲染</span>
            <div class="toolbar-spacer" />
            <el-tag v-if="activeSource === 'remote'" type="success" size="small">远端 schema</el-tag>
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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, RefreshLeft } from '@element-plus/icons-vue'
import { createFormgenObject, getFormgenSchema, updateFormgenObject } from '@/api/formgen'
import type { FormgenObjectPayload } from '@/api/formgen'
import FormRender from '@/components/form-render/FormRender.vue'
import type { FormSchema } from '@/components/form-render/types'
import SchemaEditor from './SchemaEditor.vue'

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
    ownerId: {
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
    orderedAt: {
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
const currentSchema = ref<FormSchema>(JSON.parse(JSON.stringify(MOCK_SCHEMA)) as FormSchema)
const model = ref<Record<string, any>>({})
const loading = ref(false)
const saving = ref(false)
const activeSource = ref<'mock' | 'remote'>('mock')
const activeTab = ref('visual')
const saveMode = ref('create')

const leftSpan = computed(() => (activeTab.value === 'visual' ? 14 : 10))
const rightSpan = computed(() => 24 - leftSpan.value)

const parsedSchema = computed<FormSchema | null>(() => {
  try {
    const parsed = JSON.parse(schemaText.value) as FormSchema
    if (parsed && typeof parsed === 'object' && parsed.fields && typeof parsed.fields === 'object') {
      return parsed
    }
    return null
  } catch {
    return null
  }
})

const parseError = computed(() => {
  try {
    JSON.parse(schemaText.value)
    return ''
  } catch (error) {
    return error instanceof Error ? error.message : String(error)
  }
})

watch(schemaText, (text) => {
  try {
    const parsed = JSON.parse(text) as FormSchema
    if (parsed && typeof parsed === 'object' && parsed.fields && typeof parsed.fields === 'object') {
      currentSchema.value = parsed
    }
  } catch {
    // 解析失败时保留上一个可用 schema，JSON 面板负责提示错误
  }
})

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
  currentSchema.value = JSON.parse(JSON.stringify(schema)) as FormSchema
  model.value = buildModel(schema)
  activeSource.value = source
  saveMode.value = source === 'remote' ? 'update' : 'create'
}

function handleModelUpdate(value: Record<string, any>): void {
  model.value = value
}

function handleEditorUpdate(schema: FormSchema): void {
  currentSchema.value = schema
  schemaText.value = JSON.stringify(schema, null, 2)
  syncModelDefaults(schema)
}

function useMockSchema(): void {
  applySchema(JSON.parse(JSON.stringify(MOCK_SCHEMA)) as FormSchema, 'mock')
  ElMessage.info('已使用内置示例 schema')
}

async function loadRemoteSchema(): Promise<void> {
  const key = objectKey.value.trim() || 'biz_customer'
  loading.value = true
  try {
    const data = await getFormgenSchema(key)
    if (data && data.fields) {
      applySchema(data, 'remote')
      ElMessage.success('已加载后端 schema')
      return
    }
    useMockSchema()
  } catch {
    useMockSchema()
  } finally {
    loading.value = false
  }
}

function buildPayload(schema: FormSchema): FormgenObjectPayload {
  const fields = Object.entries(schema.fields ?? {}).map(([name, field]) => ({
    name,
    title: field.title,
    type: (field.type ?? 'string') as FormgenObjectPayload['fields'][number]['type'],
    required: field.required === true,
    'x-ui': field['x-ui'] ? { ...field['x-ui'] } : undefined,
  }))
  return {
    objectKey: schema.objectKey,
    name: schema.name,
    fields,
    mode: saveMode.value as 'create' | 'update',
  }
}

async function saveSchema(): Promise<void> {
  const schema = currentSchema.value
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
  saving.value = true
  try {
    const payload = buildPayload(schema)
    if (saveMode.value === 'create') {
      await createFormgenObject(payload)
    } else {
      await updateFormgenObject(schema.objectKey, payload)
    }
    ElMessage.success('保存成功')
  } catch (error) {
    const status = (error as any)?.response?.status
    if (status === 404) {
      ElMessage.error('保存接口不存在（404）：后端 P4a 对象接口尚未就绪，请稍后重试')
    }
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  model.value = buildModel(MOCK_SCHEMA)
  loadRemoteSchema()
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
