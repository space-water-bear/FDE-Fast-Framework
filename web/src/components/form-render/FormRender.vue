<template>
  <div class="form-render">
    <el-form :model="formModel" :rules="formRules" :label-width="labelWidth">
      <el-row :gutter="16">
        <el-col v-for="entry in fieldEntries" :key="entry.name" :span="entry.colSpan">
          <el-form-item v-show="linkageState.visible[entry.name] !== false" :label="entry.title" :prop="entry.name">
            <el-input
              v-if="entry.component === 'ElInput'"
              :model-value="modelValue[entry.name]"
              :placeholder="entry.xui.placeholder ?? '请输入' + entry.title"
              :disabled="disabled"
              clearable
              @update:model-value="handleFieldInput(entry.name, $event)"
            />

            <el-input
              v-else-if="entry.component === 'ElInputTextarea'"
              :model-value="modelValue[entry.name]"
              type="textarea"
              :rows="3"
              :placeholder="entry.xui.placeholder ?? '请输入' + entry.title"
              :disabled="disabled"
              @update:model-value="handleFieldInput(entry.name, $event)"
            />

            <el-input-number
              v-else-if="entry.component === 'ElInputNumber'"
              :model-value="modelValue[entry.name]"
              :min="entry.field.minimum"
              :max="entry.field.maximum"
              :step="entry.xui.step ?? 1"
              :placeholder="entry.xui.placeholder ?? '请输入' + entry.title"
              :disabled="disabled"
              controls-position="right"
              class="control-full"
              @update:model-value="handleFieldInput(entry.name, $event)"
            />

            <el-switch
              v-else-if="entry.component === 'ElSwitch'"
              :model-value="modelValue[entry.name]"
              :disabled="disabled"
              @update:model-value="handleFieldInput(entry.name, $event)"
            />

            <el-date-picker
              v-else-if="entry.component === 'ElDatePicker'"
              :model-value="modelValue[entry.name]"
              type="datetime"
              value-format="YYYY-MM-DDTHH:mm:ssZ"
              :placeholder="entry.xui.placeholder ?? '请选择' + entry.title"
              :disabled="disabled"
              class="control-full"
              @update:model-value="handleFieldInput(entry.name, $event)"
            />

            <el-select
              v-else-if="entry.component === 'ElSelect'"
              :model-value="modelValue[entry.name]"
              :placeholder="entry.xui.placeholder ?? '请选择' + entry.title"
              :disabled="disabled"
              clearable
              filterable
              class="control-full"
              @update:model-value="handleFieldInput(entry.name, $event)"
            >
              <el-option
                v-for="opt in optionsFor(entry.name)"
                :key="String(opt.value)"
                :label="opt.label"
                :value="opt.value"
              />
            </el-select>

            <el-radio-group
              v-else-if="entry.component === 'ElRadioGroup'"
              :model-value="modelValue[entry.name]"
              :disabled="disabled"
              @update:model-value="handleFieldInput(entry.name, $event)"
            >
              <el-radio v-for="opt in optionsFor(entry.name)" :key="String(opt.value)" :value="opt.value">
                {{ opt.label }}
              </el-radio>
            </el-radio-group>

            <div v-else-if="entry.component === 'ElTableSelect'" class="table-select-control">
              <el-input
                :model-value="tableSelectLabel(entry.name)"
                :placeholder="entry.xui.placeholder ?? '请选择' + entry.title"
                :disabled="disabled"
                readonly
                @click="openTableSelect(entry.name)"
              />
              <el-button :disabled="disabled" @click="openTableSelect(entry.name)">选择</el-button>
            </div>

            <el-input
              v-else
              :model-value="modelValue[entry.name]"
              :placeholder="entry.xui.placeholder ?? '请输入' + entry.title"
              :disabled="disabled"
              clearable
              @update:model-value="handleFieldInput(entry.name, $event)"
            />
          </el-form-item>
        </el-col>
      </el-row>
    </el-form>

    <el-dialog
      v-model="tableSelectVisible"
      :title="tableSelectTitle"
      width="600px"
      destroy-on-close
      @closed="handleTableSelectClosed"
    >
      <el-input v-model="tableSelectKeyword" placeholder="输入关键字过滤" clearable class="table-select-filter" />
      <el-table
        :data="tableSelectFiltered"
        border
        size="small"
        max-height="360"
        highlight-current-row
        @row-click="handleTableSelectRow"
      >
        <el-table-column
          v-for="col in tableSelectColumns"
          :key="col.prop"
          :prop="col.prop"
          :label="col.label"
          min-width="110"
          show-overflow-tooltip
        />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FormItemRule, FormRules } from 'element-plus'
import { loadDictOptions, resolveControl, resolveFieldOptions } from './controls'
import { applyLinkages, buildLinkageState, detectLinkageCycles } from './linkage'
import type { LinkageState } from './linkage'
import type { ControlType, FormFieldSchema, FormOption, FormSchema, XUi } from './types'

interface FieldEntry {
  name: string
  title: string
  field: FormFieldSchema
  control: ControlType
  component: string
  colSpan: number
  xui: XUi
}

interface TableSelectColumn {
  prop: string
  label: string
  minWidth: number
}

const props = withDefaults(
  defineProps<{
    schema: FormSchema
    modelValue?: Record<string, any>
    disabled?: boolean
    labelWidth?: string
  }>(),
  {
    modelValue: () => ({}),
    disabled: false,
    labelWidth: '120px',
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: Record<string, any>): void
}>()

const fieldOptionsMap = ref<Record<string, FormOption[]>>({})
const linkageState = reactive<LinkageState>(buildLinkageState(props.schema, props.modelValue ?? {}))
const tableSelectField = ref<string | null>(null)
const tableSelectVisible = ref(false)
const tableSelectKeyword = ref('')

const formModel = computed(() => props.modelValue ?? {})

const fieldEntries = computed<FieldEntry[]>(() =>
  Object.entries(props.schema.fields ?? {})
    .sort((a, b) => (a[1]['x-fde']?.sort ?? 0) - (b[1]['x-fde']?.sort ?? 0))
    .map(([name, field]) => {
    const meta = resolveControl(field)
    const xui = field['x-ui'] ?? {}
    return {
      name,
      title: field.title ?? name,
      field,
      control: meta.control,
      component: meta.component,
      colSpan: xui.colSpan ?? 24,
      xui,
    }
  }),
)

const formRules = computed<FormRules>(() => {
  const rules: FormRules = {}
  for (const entry of fieldEntries.value) {
    const fieldRules = buildFieldRules(entry, linkageState.required[entry.name] === true)
    if (fieldRules.length > 0) {
      rules[entry.name] = fieldRules
    }
  }
  return rules
})

function buildFieldRules(entry: FieldEntry, required: boolean): FormItemRule[] {
  const field = entry.field
  const title = entry.title
  const rules: FormItemRule[] = []
  if (required) {
    rules.push({ required: true, message: title + '不能为空', trigger: ['blur', 'change'] })
  }
  if (field.minLength !== undefined) {
    rules.push({ min: field.minLength, message: title + '长度不能少于 ' + field.minLength + ' 个字符', trigger: 'blur' })
  }
  if (field.maxLength !== undefined) {
    rules.push({ max: field.maxLength, message: title + '长度不能超过 ' + field.maxLength + ' 个字符', trigger: 'blur' })
  }
  if (field.pattern) {
    try {
      rules.push({ pattern: new RegExp(field.pattern), message: title + '格式不正确', trigger: 'blur' })
    } catch {
      // 非法正则忽略，避免运行时崩溃
    }
  }
  if (field.minimum !== undefined || field.maximum !== undefined) {
    const rule: FormItemRule = {
      type: 'number',
      message: title + '数值不在允许范围内',
      trigger: ['blur', 'change'],
    }
    if (field.minimum !== undefined) rule.min = field.minimum
    if (field.maximum !== undefined) rule.max = field.maximum
    rules.push(rule)
  }
  return rules
}

function optionsFor(name: string): FormOption[] {
  const entry = fieldEntries.value.find((item) => item.name === name)
  if (!entry) return []
  return resolveFieldOptions(entry.field, fieldOptionsMap.value[name] ?? [])
}

let optionLoadToken = 0

function setupOptions(): void {
  optionLoadToken += 1
  const token = optionLoadToken
  const next: Record<string, FormOption[]> = {}
  for (const [name, field] of Object.entries(props.schema.fields ?? {})) {
    const xui = field['x-ui']
    const hasStatic = (xui?.options && xui.options.length > 0) || (field.enum && field.enum.length > 0)
    if (hasStatic) {
      next[name] = resolveFieldOptions(field, [])
      continue
    }
    if (xui?.dict !== undefined && xui.dict !== null && xui.dict !== '') {
      loadDictOptions(xui.dict)
        .then((list) => {
          if (token === optionLoadToken) {
            fieldOptionsMap.value[name] = list
          }
        })
        .catch(() => {
          if (token === optionLoadToken) {
            fieldOptionsMap.value[name] = []
          }
        })
    }
  }
  fieldOptionsMap.value = next
}

watch(
  () => props.schema,
  (schema) => {
    const nextState = buildLinkageState(schema, props.modelValue ?? {})
    linkageState.visible = nextState.visible
    linkageState.required = nextState.required
    const cyclic = detectLinkageCycles(schema['x-linkages'] ?? [])
    if (cyclic.size > 0) {
      console.warn('[form-render] 忽略存在循环依赖的联动字段: ' + Array.from(cyclic).join(', '))
    }
    setupOptions()
  },
  { immediate: true },
)

watch(
  () => props.modelValue,
  (model) => {
    applyLinkages(props.schema, model ?? {}, linkageState)
  },
  { deep: true },
)

function handleFieldInput(name: string, value: any): void {
  const next: Record<string, any> = { ...(props.modelValue ?? {}) }
  next[name] = value
  emit('update:modelValue', next)
  applyLinkages(props.schema, next, linkageState)
}

const tableSelectTitle = computed(() => {
  const fieldName = tableSelectField.value
  if (!fieldName) return '选择'
  const entry = fieldEntries.value.find((item) => item.name === fieldName)
  return entry ? '选择' + entry.title : '选择'
})

const tableSelectColumns = computed<TableSelectColumn[]>(() => {
  const fieldName = tableSelectField.value
  if (!fieldName) return []
  const keys: string[] = []
  for (const opt of optionsFor(fieldName)) {
    for (const key of Object.keys(opt)) {
      if (key !== 'value' && !keys.includes(key)) keys.push(key)
    }
  }
  if (keys.length === 0) keys.push('label')
  return keys.map((key) => ({ prop: key, label: key, minWidth: 110 }))
})

const tableSelectFiltered = computed(() => {
  const fieldName = tableSelectField.value
  if (!fieldName) return []
  const keyword = tableSelectKeyword.value.trim().toLowerCase()
  const options = optionsFor(fieldName)
  if (!keyword) return options
  return options.filter((opt) => String(opt.label).toLowerCase().includes(keyword))
})

function tableSelectLabel(name: string): string {
  const value = props.modelValue?.[name]
  const matched = optionsFor(name).find((opt) => opt.value === value)
  if (matched) return matched.label
  return value === undefined || value === null || value === '' ? '' : String(value)
}

function openTableSelect(name: string): void {
  if (props.disabled) return
  tableSelectField.value = name
  tableSelectKeyword.value = ''
  tableSelectVisible.value = true
}

function handleTableSelectRow(row: any): void {
  if (!tableSelectField.value) return
  handleFieldInput(tableSelectField.value, row.value)
  tableSelectVisible.value = false
}

function handleTableSelectClosed(): void {
  tableSelectField.value = null
  tableSelectKeyword.value = ''
}
</script>

<style scoped>
.form-render {
  width: 100%;
}
.control-full {
  width: 100%;
}
.table-select-control {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}
.table-select-control .el-input {
  flex: 1;
}
.table-select-filter {
  margin-bottom: 10px;
}
</style>
