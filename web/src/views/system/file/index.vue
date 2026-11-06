<template>
  <div class="page file-page">
    <table-page
      ref="pageRef"
      :columns="columns"
      :fetch="fetchFiles"
      :search-fields="searchFields"
      :toolbar-actions="[]"
    >
      <template #toolbar>
        <el-upload
          :show-file-list="false"
          :http-request="doUpload"
          accept=".png,.jpg,.jpeg,.gif,.webp,.pdf,.doc,.docx,.xls,.xlsx,.txt,.zip"
        >
          <el-button type="primary" :icon="Upload" :loading="uploading">上传文件</el-button>
        </el-upload>
      </template>
      <template #preview="{ row }">
        <img v-if="isImage(row.ext)" :src="row.url" class="file-thumb" @click="openPreview(row)" />
        <el-tag v-else size="small">{{ row.ext }}</el-tag>
      </template>
      <template #size="{ row }">{{ formatSize(row.size) }}</template>
      <template #createdAt="{ row }">{{ formatTime(row.createdAt) }}</template>
      <template #ops="{ row }">
        <el-button link type="primary" @click="openUrl(row.url)">下载</el-button>
        <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </table-page>

    <el-dialog v-model="previewVisible" title="图片预览" width="720px">
      <img :src="previewUrl" style="max-width: 100%; display: block; margin: 0 auto" />
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { UploadRequestOptions } from 'element-plus'
import { Upload } from '@element-plus/icons-vue'
import { deleteFile, listFiles, uploadFile } from '@/api/file'
import type { FileItem } from '@/api/file'
import { formatSize, formatTime } from '@/utils/format'
import { TablePage } from '@/components/table-page'
import type { SearchField, TableQuery } from '@/components/table-page'

const pageRef = ref<InstanceType<typeof TablePage>>()
const uploading = ref(false)

const searchFields: SearchField[] = [
  { field: 'keyword', label: '关键词', placeholder: '文件名 / 上传人' },
]

const columns = [
  { field: 'id', title: 'ID', width: 70 },
  { title: '预览', width: 90, slots: { default: 'preview' } },
  { field: 'name', title: '文件名', minWidth: 200 },
  { title: '大小', width: 100, slots: { default: 'size' } },
  { field: 'uploader', title: '上传人', width: 110 },
  { title: '上传时间', width: 170, slots: { default: 'createdAt' } },
  { title: '操作', width: 150, fixed: 'right', slots: { default: 'ops' } },
]

async function fetchFiles(query: TableQuery) {
  const data = await listFiles({
    page: query.page,
    pageSize: query.pageSize,
    keyword: (query.keyword as string) || '',
  })
  return { list: data.list, total: data.total }
}

const previewVisible = ref(false)
const previewUrl = ref('')

const imageExts = ['.png', '.jpg', '.jpeg', '.gif', '.webp']

function isImage(ext: string) {
  return imageExts.includes(ext.toLowerCase())
}

async function doUpload(options: UploadRequestOptions) {
  uploading.value = true
  try {
    await uploadFile(options.file as File)
    ElMessage.success('上传成功')
    pageRef.value?.reload()
  } finally {
    uploading.value = false
  }
}

function openPreview(row: FileItem) {
  previewUrl.value = row.url
  previewVisible.value = true
}

function openUrl(url: string) {
  window.open(url, '_blank')
}

function handleDelete(row: FileItem) {
  ElMessageBox.confirm('确定删除文件「' + row.name + '」吗？磁盘文件将一并删除。', '提示', { type: 'warning' })
    .then(async () => {
      await deleteFile(row.id)
      ElMessage.success('删除成功')
      pageRef.value?.reload()
    })
    .catch(() => {})
}
</script>

<style scoped>
.file-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.file-thumb {
  width: 44px;
  height: 44px;
  object-fit: cover;
  border-radius: 4px;
  cursor: zoom-in;
  border: 1px solid #ebeef5;
}
</style>
