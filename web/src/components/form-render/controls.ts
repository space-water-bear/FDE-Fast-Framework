import request from '@/utils/request'
import type { DictDataItem } from '@/api/dict'
import type { ControlType, FormFieldSchema, FormOption } from './types'

export interface ControlMeta {
  control: ControlType
  component: string
}

export const CONTROL_REGISTRY: Record<ControlType, ControlMeta> = {
  input: { control: 'input', component: 'ElInput' },
  textarea: { control: 'textarea', component: 'ElInputTextarea' },
  number: { control: 'number', component: 'ElInputNumber' },
  switch: { control: 'switch', component: 'ElSwitch' },
  datetime: { control: 'datetime', component: 'ElDatePicker' },
  select: { control: 'select', component: 'ElSelect' },
  radio: { control: 'radio', component: 'ElRadioGroup' },
  'table-select': { control: 'table-select', component: 'ElTableSelect' },
}

function hasOwn(obj: object, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(obj, key)
}

export function defaultControlForType(type?: string): ControlType {
  switch (type) {
    case 'text':
      return 'textarea'
    case 'number':
    case 'integer':
    case 'int':
    case 'float':
      return 'number'
    case 'bool':
    case 'boolean':
      return 'switch'
    case 'datetime':
      return 'datetime'
    case 'enum':
      return 'select'
    case 'relation':
      return 'table-select'
    default:
      return 'input'
  }
}

export function resolveControl(field: FormFieldSchema): ControlMeta {
  const control = field['x-ui']?.control
  if (control && hasOwn(CONTROL_REGISTRY, control)) {
    return CONTROL_REGISTRY[control as ControlType]
  }
  if (field.enum?.length) return CONTROL_REGISTRY.select
  if (field.format === 'date-time') return CONTROL_REGISTRY.datetime
  return CONTROL_REGISTRY[defaultControlForType(field['x-fde']?.type ?? field.type)]
}

export function enumOptions(field: FormFieldSchema): FormOption[] {
  return (field.enum ?? []).map((value) => ({ label: String(value), value }))
}

export function resolveFieldOptions(field: FormFieldSchema, loaded: FormOption[] = []): FormOption[] {
  const xui = field['x-ui']
  if (xui?.options && xui.options.length > 0) return xui.options
  if (field.enum && field.enum.length > 0) return enumOptions(field)
  return loaded
}

// 数字(或纯数字字符串)按现有 /api/dict/data/list?typeId= 加载；
// 字典编码字符串按 ?dict= 加载，兼容 P1 后端按字典编码查询的扩展。
export async function loadDictOptions(dict: string | number): Promise<FormOption[]> {
  const numeric = typeof dict === 'number' || (typeof dict === 'string' && /^\d+$/.test(dict))
  const params = numeric ? { typeId: Number(dict) } : { dict }
  const data = await request.get<any, { list: DictDataItem[] }>('/dict/data/list', { params })
  return (data.list ?? [])
    .filter((item) => item.enabled !== false)
    .map((item) => ({ label: item.label, value: item.value }))
}
