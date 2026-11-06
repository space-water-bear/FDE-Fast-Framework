<template>
  <div class="schema-editor">
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
              :model-value="field.type ?? 'string'"
              size="small"
              class="control-full"
              @update:model-value="onTypeChange(name, $event)"
            >
              <el-option v-for="item in TYPE_OPTIONS" :key="item" :label="item" :value="item" />
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
import { CONTROL_REGISTRY, defaultControlForType } from '@/components/form-render/controls'
import type { ControlType, FormFieldSchema, FormOption, FormSchema, XUi } from '@/components/form-render/types'

const props = defineProps<{ modelValue: FormSchema }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: FormSchema): void }>()

const TYPE_OPTIONS = ['string', 'text', 'int', 'float', 'bool', 'datetime', 'enum', 'relation'] as const
const CONTROL_OPTIONS = Object.keys(CONTROL_REGISTRY) as ControlType[]

const schema = computed(() => props.modelValue)
const fieldEntries = computed<[string, FormFieldSchema][]>(() =>
  Object.entries(props.modelValue.fields ?? {}),
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
  fields[name] = { ...field, type, 'x-ui': xui } as FormFieldSchema
  emitSchema({ ...props.modelValue, fields })
}

function currentControl(field: FormFieldSchema): ControlType {
  return field['x-ui']?.control ?? defaultControlForType(field.type)
}

function optionsOf(field: FormFieldSchema): FormOption[] {
  return (field['x-ui']?.options ?? []).map((opt) => ({ label: opt.label, value: opt.value }))
}

function setOption(name: string, index: number, key: 'label' | 'value', value: string): void {
  const field = props.modelValue.fields[name]
  const xui = field?.['x-ui'] ?? {}
  const options = [...(xui.options ?? [])]
  const current = options[index] ?? { label: '', value: '' }
  options[index] = { ...current, [key]: value }
  setXui(name, 'options', options)
}

function addOption(name: string): void {
  const field = props.modelValue.fields[name]
  const xui = field?.['x-ui'] ?? {}
  const options = [...(xui.options ?? []), { label: '', value: '' }]
  setXui(name, 'options', options)
}

function removeOption(name: string, index: number): void {
  const field = props.modelValue.fields[name]
  const xui = field?.['x-ui'] ?? {}
  const options = [...(xui.options ?? [])]
  options.splice(index, 1)
  setXui(name, 'options', options)
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
    'x-ui': { control: 'input', colSpan: 12 },
  }
  emitSchema({ ...props.modelValue, fields })
}

function removeField(name: string): void {
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
  const entries = Object.entries(props.modelValue.fields)
  const index = entries.findIndex(([itemName]) => itemName === name)
  const target = index + dir
  if (index < 0 || target < 0 || target >= entries.length) return
  const [item] = entries.splice(index, 1)
  entries.splice(target, 0, item)
  emitSchema({
    ...props.modelValue,
    fields: Object.fromEntries(entries) as Record<string, FormFieldSchema>,
  })
}

function renameField(oldName: string, rawName: string | number): void {
  const newName = String(rawName).trim()
  if (!newName) {
    renameTick.value += 1
    ElMessage.warning('字段名不能为空')
    return
  }
  if (newName === oldName) return
  const fields = props.modelValue.fields
  if (Object.prototype.hasOwnProperty.call(fields, newName)) {
    renameTick.value += 1
    ElMessage.warning('字段名 ' + newName + ' 已存在')
    return
  }
  const nextFields: Record<string, FormFieldSchema> = {}
  for (const [name, field] of Object.entries(fields)) {
    nextFields[name === oldName ? newName : name] = field
  }
  emitSchema({ ...props.modelValue, fields: nextFields })
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
