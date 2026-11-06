<template>
  <div class="schema-editor">
    <el-alert type="info" :closable="false" title="完整规则、关联、联动及扩展配置保留在 Schema JSON 中；可视化编辑只修改所选属性。" />
    <div class="editor-meta">
      <div class="meta-grid">
        <div class="meta-item">
          <span class="field-label">objectKey</span>
          <el-input
            :model-value="schema.objectKey"
            size="small"
            placeholder="如 biz_customer"
            @update:model-value="setMeta('objectKey', $event)"
          />
        </div>
        <div class="meta-item">
          <span class="field-label">名称</span>
          <el-input
            :model-value="schema.name"
            size="small"
            placeholder="如 客户"
            @update:model-value="setMeta('name', $event)"
          />
        </div>
      </div>
    </div>

    <div class="editor-toolbar">
      <span class="panel-title">字段列表</span>
      <div class="toolbar-spacer" />
      <el-button type="primary" size="small" :icon="Plus" @click="addField">新增字段</el-button>
    </div>

    <div class="field-list">
      <el-empty
        v-if="fieldEntries.length === 0"
        description="暂无字段，点击右上角新增"
        :image-size="60"
      />
      <div v-for="[name, field] in fieldEntries" :key="name" class="field-card">
        <div class="field-card-header">
          <el-input
            :key="name + ':' + renameTick"
            :model-value="name"
            size="small"
            class="field-name-input"
            placeholder="字段名"
            @change="renameField(name, $event)"
          />
          <el-input
            :model-value="field.title ?? ''"
            size="small"
            class="field-title-input"
            placeholder="标题"
            @update:model-value="setField(name, { title: $event })"
          />
          <el-button
            size="small"
            :icon="ArrowUp"
            :disabled="isFirst(name)"
            title="上移"
            @click="moveField(name, -1)"
          />
          <el-button
            size="small"
            :icon="ArrowDown"
            :disabled="isLast(name)"
            title="下移"
            @click="moveField(name, 1)"
          />
          <el-button size="small" type="danger" :icon="Delete" title="删除" @click="removeField(name)" />
        </div>

        <div class="field-grid">
          <div class="field-grid-item">
            <span class="field-label">类型</span>
            <el-select
              :model-value="currentType(field)"
              size="small"
              class="control-full"
              @update:model-value="onTypeChange(name, $event)"
            >
              <el-option v-for="item in TYPE_OPTIONS" :key="item" :label="item" :value="item" />
              <el-option v-if="!TYPE_OPTIONS.includes(currentType(field) as any)" :label="currentType(field) + '（请用 JSON 编辑）'" :value="currentType(field)" disabled />
            </el-select>
          </div>
          <div class="field-grid-item">
            <span class="field-label">必填</span>
            <el-switch
              :model-value="field.required === true"
              size="small"
              @update:model-value="setField(name, { required: $event })"
            />
          </div>
          <div class="field-grid-item">
            <span class="field-label">控件</span>
            <el-select
              :model-value="currentControl(field)"
              size="small"
              class="control-full"
              @update:model-value="setXui(name, 'control', $event)"
            >
              <el-option v-for="item in CONTROL_OPTIONS" :key="item" :label="item" :value="item" />
            </el-select>
          </div>
          <div class="field-grid-item">
            <span class="field-label">placeholder</span>
            <el-input
              :model-value="field['x-ui']?.placeholder ?? ''"
              size="small"
              @update:model-value="setXui(name, 'placeholder', $event)"
            />
          </div>
          <div class="field-grid-item">
            <span class="field-label">colSpan</span>
            <el-input-number
              :model-value="field['x-ui']?.colSpan ?? 12"
              :min="1"
              :max="24"
              size="small"
              class="control-full"
              @update:model-value="setXui(name, 'colSpan', $event)"
            />
          </div>
          <div class="field-grid-item">
            <span class="field-label">dict</span>
            <el-input
              :model-value="String(field['x-ui']?.dict ?? '')"
              size="small"
              placeholder="字典编码或 typeId"
              @update:model-value="setXui(name, 'dict', $event)"
            />
          </div>
          <div class="field-grid-item" v-for="flag in FIELD_FLAGS" :key="flag.key">
            <span class="field-label">{{ flag.label }}</span>
            <el-switch :model-value="field['x-fde']?.[flag.key] ?? false" size="small" @update:model-value="setFlag(name, flag.key, Boolean($event))" />
          </div>
        </div>

        <div class="options-editor">
          <div class="options-header">
            <span class="field-label">options（label / value）</span>
            <el-button size="small" :icon="Plus" @click="addOption(name)">选项</el-button>
          </div>
          <div v-for="(opt, index) in optionsOf(field)" :key="index" class="option-row">
            <el-input
              :model-value="opt.label"
              size="small"
              placeholder="label"
              @update:model-value="setOption(name, index, 'label', $event)"
            />
            <el-input
              :model-value="String(opt.value)"
              size="small"
              placeholder="value"
              @update:model-value="setOption(name, index, 'value', $event)"
            />
            <el-button size="small" :icon="Delete" circle title="删除选项" @click="removeOption(name, index)" />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowDown, ArrowUp, Delete, Plus } from '@element-plus/icons-vue'
import { CONTROL_REGISTRY, defaultControlForType, resolveControl } from '@/components/form-render/controls'
import type { BusinessFieldType, ControlType, FormFieldSchema, FormOption, FormSchema, XUi } from '@/components/form-render/types'
import { businessTypeForField, fieldHasReferences, moveSchemaField, orderedFields, parseOptionValue, renameSchemaField } from './schema-payload'

const props = defineProps<{ modelValue: FormSchema }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: FormSchema): void }>()

const TYPE_OPTIONS = ['string', 'text', 'int', 'float', 'bool', 'datetime', 'enum', 'relation'] as const
const CONTROL_OPTIONS = Object.keys(CONTROL_REGISTRY) as ControlType[]
const FIELD_FLAGS = [{ key: 'searchable', label: '可查询' }, { key: 'inList', label: '列表显示' }, { key: 'inForm', label: '表单显示' }] as const

const schema = computed(() => props.modelValue)
const fieldEntries = computed<[string, FormFieldSchema][]>(() =>
  orderedFields(props.modelValue),
)
const renameTick = ref(0)

function emitSchema(next: FormSchema): void {
  emit('update:modelValue', next)
}

function setMeta(key: 'objectKey' | 'name', value: string): void {
  emitSchema({ ...props.modelValue, [key]: value })
}

function setField(name: string, patch: Partial<FormFieldSchema>): void {
  const fields = { ...props.modelValue.fields }
  const current = fields[name] ?? {}
  fields[name] = { ...current, ...patch } as FormFieldSchema
  emitSchema({ ...props.modelValue, fields })
}

function setXui(name: string, key: keyof XUi, value: unknown): void {
  const fields = { ...props.modelValue.fields }
  const field = fields[name] ?? {}
  const xui: Record<string, unknown> = { ...(field['x-ui'] ?? {}) }
  if (value === '' || value === undefined || value === null) {
    delete xui[key]
  } else {
    xui[key] = value
  }
  fields[name] = { ...field, 'x-ui': xui as XUi } as FormFieldSchema
  emitSchema({ ...props.modelValue, fields })
}

function onTypeChange(name: string, type: string): void {
  const fields = { ...props.modelValue.fields }
  const field = fields[name] ?? {}
  const xui = { ...(field['x-ui'] ?? {}), control: defaultControlForType(type) }
  const jsonType = ({ text: 'string', int: 'integer', float: 'number', bool: 'boolean', datetime: 'string', enum: 'string', relation: 'integer' } as Record<string, string>)[type] ?? type
  fields[name] = { ...field, type: jsonType, 'x-ui': xui, 'x-fde': { ...field['x-fde'], type: type as BusinessFieldType } }
  if (type === 'datetime') fields[name].format = 'date-time'
  else if (field.format === 'date-time') delete fields[name].format
  emitSchema({ ...props.modelValue, fields })
}

function currentType(field: FormFieldSchema): string {
  try { return businessTypeForField(field) } catch { return field.type ?? 'unknown' }
}

function setFlag(name: string, key: 'searchable' | 'inList' | 'inForm', value: boolean): void {
  const field = props.modelValue.fields[name]
  setField(name, { 'x-fde': { ...field['x-fde'], [key]: value } })
}

function currentControl(field: FormFieldSchema): ControlType {
  return resolveControl(field).control
}

function optionsOf(field: FormFieldSchema): FormOption[] {
  return field['x-ui']?.options ?? (field.enum ?? []).map((value) => ({ label: String(value), value }))
}

function setOptions(name: string, options: FormOption[]): void {
  const field = props.modelValue.fields[name]
  const patch: Partial<FormFieldSchema> = { 'x-ui': { ...field['x-ui'], options } }
  if (field.enum || currentType(field) === 'enum') patch.enum = options.map((option) => option.value)
  setField(name, patch)
}

function setOption(name: string, index: number, key: 'label' | 'value', value: string): void {
  const field = props.modelValue.fields[name]
  const options = [...optionsOf(field)]
  const current = options[index] ?? { label: '', value: '' }
  try {
    options[index] = { ...current, [key]: key === 'value' ? parseOptionValue(value, current.value) : value }
    setOptions(name, options)
  } catch (error) {
    ElMessage.warning((error as Error).message)
  }
}

function addOption(name: string): void {
  const field = props.modelValue.fields[name]
  const options = [...optionsOf(field), { label: '', value: '' }]
  setOptions(name, options)
}

function removeOption(name: string, index: number): void {
  const field = props.modelValue.fields[name]
  const options = [...optionsOf(field)]
  options.splice(index, 1)
  setOptions(name, options)
}

function addField(): void {
  const fields = { ...props.modelValue.fields }
  let index = Object.keys(fields).length + 1
  while (Object.prototype.hasOwnProperty.call(fields, 'field_' + index)) {
    index += 1
  }
  const key = 'field_' + index
  fields[key] = {
    title: '字段' + index,
    type: 'string',
    required: false,
    'x-fde': { type: 'string', sort: Object.keys(fields).length, searchable: false, inList: true, inForm: true },
    'x-ui': { control: 'input', colSpan: 12 },
  }
  emitSchema({ ...props.modelValue, fields })
}

function removeField(name: string): void {
  if (fieldHasReferences(props.modelValue, name)) {
    ElMessage.warning('该字段存在关联或联动，请先在 Schema JSON 中明确调整引用后再删除')
    return
  }
  const fields = { ...props.modelValue.fields }
  delete fields[name]
  emitSchema({ ...props.modelValue, fields })
}

function isFirst(name: string): boolean {
  return fieldEntries.value[0]?.[0] === name
}

function isLast(name: string): boolean {
  const entries = fieldEntries.value
  return entries[entries.length - 1]?.[0] === name
}

function moveField(name: string, dir: -1 | 1): void {
  emitSchema(moveSchemaField(props.modelValue, name, dir))
}

function renameField(oldName: string, rawName: string | number): void {
  const newName = String(rawName).trim()
  if (!newName) {
    renameTick.value += 1
    ElMessage.warning('字段名不能为空')
    return
  }
  if (newName === oldName) return
  try {
    emitSchema(renameSchemaField(props.modelValue, oldName, newName))
  } catch (error) {
    renameTick.value += 1
    ElMessage.warning((error as Error).message)
  }
}
</script>

<style scoped>
.schema-editor {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.meta-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.meta-item {
  display: flex;
  align-items: center;
  gap: 8px;
}
.editor-toolbar {
  display: flex;
  align-items: center;
}
.toolbar-spacer {
  flex: 1;
}
.field-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.field-card {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.field-card-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.field-name-input {
  width: 150px;
  flex-shrink: 0;
}
.field-title-input {
  flex: 1;
}
.field-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px 12px;
}
.field-grid-item {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.field-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  white-space: nowrap;
  flex-shrink: 0;
}
.control-full {
  flex: 1;
  min-width: 0;
}
.options-editor {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.options-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.options-header .el-button {
  margin-left: 0;
}
.option-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.option-row .el-input {
  flex: 1;
}
</style>
