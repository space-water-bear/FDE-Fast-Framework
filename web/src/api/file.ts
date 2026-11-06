import request from '@/utils/request'

export interface FileItem {
  id: number
  name: string
  url: string
  ext: string
  size: number
  uploaderId: number
  uploader: string
  createdAt: string
}

export function uploadFile(file: File) {
  const fd = new FormData()
  fd.append('file', file)
  return request.post<any, { file: FileItem }>('/file/upload', fd, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
}

export function listFiles(params: { page: number; pageSize: number; keyword?: string }) {
  return request.get<any, { list: FileItem[]; total: number }>('/file/list', { params })
}

export function deleteFile(id: number) {
  return request.delete<any, null>('/file/' + id)
}
