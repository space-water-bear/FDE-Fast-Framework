import request from '@/utils/request'

export interface CustomerItem {
  id: number
  name: string
  age: number
  email: string
  enabled: boolean
  remark: string
  orderedAt: string
  createdAt: string
  updatedAt: string
}

export interface PageResult<T> {
  list: T[]
  total: number
}

export function listCustomers(params: Record<string, any>) {
  return request.get<any, PageResult<CustomerItem>>('/customer/list', { params })
}

export function createCustomer(data: Partial<CustomerItem>) {
  return request.post<any, null>('/customer', data)
}

export function updateCustomer(id: number, data: Partial<CustomerItem>) {
  return request.put<any, null>('/customer/' + id, data)
}

export function deleteCustomer(id: number) {
  return request.delete<any, null>('/customer/' + id)
}
