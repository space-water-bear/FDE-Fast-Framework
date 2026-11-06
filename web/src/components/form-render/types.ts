export type ControlType =
  | 'input'
  | 'textarea'
  | 'number'
  | 'switch'
  | 'datetime'
  | 'select'
  | 'radio'
  | 'table-select'

export interface FormOption {
  label: string
  value: string | number | boolean
  [key: string]: unknown
}

export interface XUi {
  [key: string]: unknown
  control?: ControlType
  placeholder?: string
  colSpan?: number
  options?: FormOption[]
  dict?: string | number
  step?: number
}

export interface LinkageWhen {
  [key: string]: unknown
  field: string
  eq?: unknown
}

export interface LinkageThen {
  [key: string]: unknown
  field: string
  visible?: boolean
  required?: boolean
}

export interface LinkageRule {
  [key: string]: unknown
  when: LinkageWhen
  then: LinkageThen
}

export interface FormFieldSchema {
  [key: string]: unknown
  title?: string
  type?: string
  required?: boolean
  default?: unknown
  enum?: (string | number | boolean)[]
  minimum?: number
  maximum?: number
  minLength?: number
  maxLength?: number
  pattern?: string
  format?: string
  'x-ui'?: XUi
  'x-fde'?: {
    [key: string]: unknown
    type?: BusinessFieldType
    searchable?: boolean
    inList?: boolean
    inForm?: boolean
    sort?: number
  }
}

export type BusinessFieldType = 'string' | 'text' | 'int' | 'float' | 'bool' | 'datetime' | 'enum' | 'relation'

export interface FormRelation {
  [key: string]: unknown
  field: string
  targetObject: string
  kind: 'many_to_one' | 'one_to_many'
}

export interface FormSchema {
  [key: string]: unknown
  $schema?: string
  objectKey: string
  name: string
  version?: number
  activeVersion?: number
  fields: Record<string, FormFieldSchema>
  'x-linkages'?: LinkageRule[]
  'x-relations'?: FormRelation[]
}
