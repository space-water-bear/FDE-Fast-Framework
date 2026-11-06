<template>
  <div class="page demo-page">
    <table-page
      :columns="columns"
      :fetch="fetchStaff"
      :search-fields="searchFields"
      :tree-data="deptTree"
      tree-label-key="name"
      tree-node-key="id"
      tree-title="部门"
      tree-query-field="deptId"
      @add="ElMessage.info('示例页面：新增按钮已触发')"
    >
      <template #status="{ row }">
        <el-tag size="small">{{ row.status }}</el-tag>
      </template>
      <template #ops="{ row }">
        <el-button link type="primary" @click="ElMessage.info('示例页面：编辑 ' + row.name)">编辑</el-button>
        <el-button link type="danger" @click="ElMessage.info('示例页面：删除 ' + row.name)">删除</el-button>
      </template>
    </table-page>
  </div>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { TablePage } from '@/components/table-page'
import type { SearchField, TableQuery } from '@/components/table-page'

const deptTree = [
  { id: 1, name: '运营部' },
  { id: 2, name: '仓库部' },
  { id: 3, name: '人事部' },
]

const searchFields: SearchField[] = [
  { field: 'name', label: '姓名', placeholder: '请输入姓名' },
  {
    field: 'status',
    label: '状态',
    type: 'select',
    options: [
      { label: '正常', value: '正常' },
      { label: '停用', value: '停用' },
    ],
  },
  { field: 'joinRange', label: '入职日期', type: 'daterange' },
]

const columns = [
  { type: 'seq', title: '#', width: 60 },
  { field: 'username', title: '用户名', minWidth: 130 },
  { field: 'name', title: '姓名', minWidth: 100 },
  { field: 'phone', title: '手机号', minWidth: 130 },
  { field: 'dept', title: '所属部门', minWidth: 110 },
  { field: 'job', title: '职务', minWidth: 100 },
  { title: '状态', width: 90, slots: { default: 'status' } },
  { title: '操作', width: 130, fixed: 'right', slots: { default: 'ops' } },
]

interface StaffItem {
  username: string
  name: string
  phone: string
  dept: string
  job: string
  status: string
  deptId: number
}

const staff: StaffItem[] = [
  { username: 'demo01', name: '演示用户01', phone: '138****0001', dept: '仓库部', job: '董事长', status: '正常', deptId: 2 },
  { username: 'demo02', name: '演示用户02', phone: '138****0002', dept: '运营部', job: '员工', status: '正常', deptId: 1 },
  { username: 'demo03', name: '演示用户03', phone: '138****0003', dept: '运营部', job: '员工', status: '正常', deptId: 1 },
  { username: 'demo04', name: '演示用户04', phone: '138****0004', dept: '运营部', job: '董事长', status: '正常', deptId: 1 },
  { username: 'demo05', name: '演示用户05', phone: '138****0005', dept: '仓库部', job: '员工', status: '正常', deptId: 2 },
  { username: 'demo06', name: '演示用户06', phone: '138****0006', dept: '运营部', job: '员工', status: '正常', deptId: 1 },
  { username: 'demo07', name: '演示用户07', phone: '138****0007', dept: '运营部', job: '董事长', status: '正常', deptId: 1 },
  { username: 'demo08', name: '演示用户08', phone: '138****0008', dept: '人事部', job: '董事长', status: '正常', deptId: 3 },
  { username: 'demo09', name: '演示用户09', phone: '138****0009', dept: '运营部', job: '董事长', status: '正常', deptId: 1 },
  { username: 'demo10', name: '演示用户10', phone: '138****0010', dept: '仓库部', job: '员工', status: '停用', deptId: 2 },
  { username: 'demo11', name: '演示用户11', phone: '138****0011', dept: '人事部', job: '员工', status: '正常', deptId: 3 },
  { username: 'demo12', name: '演示用户12', phone: '138****0012', dept: '运营部', job: '员工', status: '正常', deptId: 1 },
]

async function fetchStaff(query: TableQuery) {
  let rows = staff.slice()
  if (query.deptId) {
    rows = rows.filter((r) => r.deptId === Number(query.deptId))
  }
  if (query.name) {
    rows = rows.filter((r) => r.name.includes(String(query.name)))
  }
  if (query.status) {
    rows = rows.filter((r) => r.status === query.status)
  }
  const total = rows.length
  const start = (query.page - 1) * query.pageSize
  return { list: rows.slice(start, start + query.pageSize), total }
}
</script>

<style scoped>
.demo-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
</style>
