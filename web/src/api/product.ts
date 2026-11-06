import request from '@/utils/request'

export interface ProductItem {
  id: number
  name: string
  price: number
  stock: number
  onSale: boolean
  releasedAt: string
  remark: string
  createdAt: string
  updatedAt: string
}

export interface PageResult<T> {
  list: T[]
  total: number
}

export function listProducts(params: Record<string, any>) {
  return request.get<any, PageResult<ProductItem>>('/product/list', { params })
}

export function createProduct(data: Partial<ProductItem>) {
  return request.post<any, null>('/product', data)
}

export function updateProduct(id: number, data: Partial<ProductItem>) {
  return request.put<any, null>('/product/' + id, data)
}

export function deleteProduct(id: number) {
  return request.delete<any, null>('/product/' + id)
}
