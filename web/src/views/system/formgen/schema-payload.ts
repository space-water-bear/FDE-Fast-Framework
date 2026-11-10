import type { FormgenObjectPayload } from '@/api/formgen'
import type { BusinessFieldType, FormFieldSchema, FormSchema } from '@/components/form-render/types'

const BUSINESS_TYPES: BusinessFieldType[] = ['string', 'text', 'int', 'float', 'bool', 'datetime', 'enum', 'relation']
const RULE_KEYS = ['enum', 'default', 'format', 'minimum', 'maximum', 'exclusiveMinimum', 'exclusiveMaximum',
  'multipleOf', 'minLength', 'maxLength', 'pattern', 'minItems', 'maxItems', 'uniqueItems', 'items']

export function cloneSchema<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

export function parseFormSchema(text: string): FormSchema {
  const value = JSON.parse(text)
  if (!value || typeof value !== 'object' || Array.isArray(value) || typeof value.objectKey !== 'string'
    || typeof value.name !== 'string' || !value.fields || typeof value.fields !== 'object' || Array.isArray(value.fields)
    || Object.values(value.fields).some((field) => !field || typeof field !== 'object' || Array.isArray(field))) {
    throw new Error('Schema 需包含 objectKey、name 和字段对象 fields')
  }
  if (value['x-linkages'] != null && (!Array.isArray(value['x-linkages'])
    || value['x-linkages'].some((rule: any) => !rule || typeof rule.when?.field !== 'string' || typeof rule.then?.field !== 'string'))) {
    throw new Error('x-linkages 需为包含 when.field 与 then.field 的规则数组')
  }
  if (value['x-relations'] != null && (!Array.isArray(value['x-relations'])
    || value['x-relations'].some((relation: any) => !relation || typeof relation.field !== 'string'))) {
    throw new Error('x-relations 需为包含 field 的关联数组')
  }
  return value as FormSchema
}

export function businessTypeForField(field: FormFieldSchema): BusinessFieldType {
  const declared = field['x-fde']?.type
  if (declared && BUSINESS_TYPES.includes(declared)) return declared
  if (field.type && BUSINESS_TYPES.includes(field.type as BusinessFieldType) && field.type !== 'string') {
    return field.type as BusinessFieldType
  }
  if (field.enum) return 'enum'
  switch (field.type ?? 'string') {
    case 'integer': return 'int'
    case 'number': return 'float'
    case 'boolean': return 'bool'
    case 'string':
      if (field.format === 'date-time') return 'datetime'
      return field['x-ui']?.control === 'textarea' ? 'text' : 'string'
    default: throw new Error('暂不支持业务字段类型 ' + field.type + '，原始配置已保留，请在 JSON 中核对')
  }
}

export function orderedFields(schema: FormSchema): [string, FormFieldSchema][] {
  return Object.entries(schema.fields).sort((a, b) => (a[1]['x-fde']?.sort ?? 0) - (b[1]['x-fde']?.sort ?? 0))
}

export function schemaToPayload(schema: FormSchema, mode: 'create' | 'update', expectedVersion?: number): FormgenObjectPayload {
  const snapshot = cloneSchema(schema)
  const fields = orderedFields(snapshot).map(([name, field], index) => {
    const rules: Record<string, unknown> = { ...(field.rules as Record<string, unknown> | undefined) }
    for (const key of RULE_KEYS) {
      if (Object.prototype.hasOwnProperty.call(field, key)) rules[key] = field[key]
    }
    const meta = field['x-fde']
    return {
      name, title: field.title, type: businessTypeForField(field), required: field.required === true,
      rules, 'x-ui': field['x-ui'],
      searchable: meta?.searchable ?? (field.searchable as boolean | undefined),
      inList: meta?.inList ?? (field.inList as boolean | undefined),
      inForm: meta?.inForm ?? (field.inForm as boolean | undefined),
      sort: meta?.sort ?? (field.sort as number | undefined) ?? index,
    }
  })
  return {
    objectKey: snapshot.objectKey, name: snapshot.name, fields, schema: snapshot,
    relations: snapshot['x-relations'], linkages: snapshot['x-linkages'], mode,
    ...(mode === 'update' ? { expectedVersion } : {}),
  }
}

// Rename only the references understood by this editor; all other schema properties remain intact.
export function renameSchemaField(schema: FormSchema, oldName: string, newName: string): FormSchema {
  if (!/^[a-z][a-z0-9_]*$/.test(newName)) throw new Error('字段名需以小写字母开头，仅包含小写字母、数字和下划线')
  if (oldName !== newName && Object.prototype.hasOwnProperty.call(schema.fields, newName)) throw new Error('字段名 ' + newName + ' 已存在')
  const next = cloneSchema(schema)
  next.fields = Object.fromEntries(Object.entries(next.fields).map(([name, field]) => [name === oldName ? newName : name, field]))
  for (const rule of next['x-linkages'] ?? []) {
    if (rule.when.field === oldName) rule.when.field = newName
    if (rule.then.field === oldName) rule.then.field = newName
  }
  for (const relation of next['x-relations'] ?? []) {
    if (relation.field === oldName) relation.field = newName
  }
  return next
}

export function fieldHasReferences(schema: FormSchema, name: string): boolean {
  return (schema['x-linkages'] ?? []).some((rule) => rule.when.field === name || rule.then.field === name)
    || (schema['x-relations'] ?? []).some((relation) => relation.field === name)
}

export function moveSchemaField(schema: FormSchema, name: string, direction: -1 | 1): FormSchema {
  const entries = orderedFields(cloneSchema(schema))
  const index = entries.findIndex(([key]) => key === name)
  const target = index + direction
  if (index < 0 || target < 0 || target >= entries.length) return schema
  const [entry] = entries.splice(index, 1)
  entries.splice(target, 0, entry)
  return { ...schema, fields: Object.fromEntries(entries.map(([key, field], sort) =>
    [key, { ...field, 'x-fde': { ...field['x-fde'], sort } }])) }
}

export function parseOptionValue(text: string, previous: string | number | boolean): string | number | boolean {
  if (typeof previous === 'number') {
    const value = Number(text)
    if (!text.trim() || !Number.isFinite(value)) throw new Error('此选项的值必须为数字')
    return value
  }
  if (typeof previous === 'boolean') {
    if (text === 'true') return true
    if (text === 'false') return false
    throw new Error('此选项的值必须为 true 或 false')
  }
  return text
}
