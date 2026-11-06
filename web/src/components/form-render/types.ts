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
  control?: ControlType
  placeholder?: string
  colSpan?: number
  options?: FormOption[]
  dict?: string | number
  step?: number
}

export interface LinkageWhen {
  field: string
  eq?: unknown
}

export interface LinkageThen {
  field: string
  visible?: boolean
  required?: boolean
}

export interface LinkageRule {
  when: LinkageWhen
  then: LinkageThen
}

export interface FormFieldSchema {
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
}

export interface FormSchema {
  $schema?: string
  objectKey: string
  name: string
  version?: number
  fields: Record<string, FormFieldSchema>
  'x-linkages'?: LinkageRule[]
}
