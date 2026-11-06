export type SearchFieldType = 'input' | 'number' | 'select' | 'daterange' | 'datetimerange'

export interface SearchOption {
  label: string
  value: string | number
}

export interface SearchField {
  field: string
  label: string
  type?: SearchFieldType
  placeholder?: string
  options?: SearchOption[]
  defaultValue?: unknown
  width?: number
  clearable?: boolean
}

export interface ToolbarAction {
  code: string
  label: string
  type?: 'primary' | 'success' | 'warning' | 'danger' | 'info'
  hidden?: boolean
}

export interface TableQuery {
  page: number
  pageSize: number
  [key: string]: any
}

export interface TableFetchResult {
  list: any[]
  total: number
}

export type TableFetch = (query: TableQuery) => Promise<TableFetchResult>
