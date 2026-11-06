package codegen

const modelTpl = `package model

[[if .HasTime]]
import "time"
[[end]]

// [[.StructName]] [[.Business]]
type [[.StructName]] struct {
	Base
[[range .Fields]]	[[.FieldName]] [[.GoType]] [[printf "%c" 96]]json:"[[.JSONName]]" gorm:"[[.GormTag]]"[[printf "%c" 96]]
[[end]]}

func ([[.StructName]]) TableName() string { return "[[.TableName]]" }

func init() {
	RegisterTable(&[[.StructName]]{})
}
`

const serviceTpl = `package service

import (
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func List[[.StructName]]s(page, pageSize int[[range .SearchFields]], [[.JSONName]] [[.GoSearchType]][[end]]) ([]model.[[.StructName]], int64, error) {
	var list []model.[[.StructName]]
	var total int64
	q := global.DB.Model(&model.[[.StructName]]{})
[[range .SearchFields]][[if eq .GoSearchType "string"]]	if [[.JSONName]] != "" {
		q = q.Where("[[.ColumnName]] LIKE ?", "%"+[[.JSONName]]+"%")
	}
[[else]]	if [[.JSONName]] != nil {
		q = q.Where("[[.ColumnName]] = ?", *[[.JSONName]])
	}
[[end]][[end]]	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("id asc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func Create[[.StructName]](m *model.[[.StructName]]) error {
	return global.DB.Create(m).Error
}

func Update[[.StructName]](id uint, m *model.[[.StructName]]) error {
	var old model.[[.StructName]]
	if err := global.DB.First(&old, id).Error; err != nil {
		return err
	}
	m.ID = id
	m.CreatedAt = old.CreatedAt
	return global.DB.Save(m).Error
}

func Delete[[.StructName]](id uint) error {
	return global.DB.Delete(&model.[[.StructName]]{}, id).Error
}
`

const apiTpl = `package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

type [[.Snake]]ListQuery struct {
[[range .SearchFields]]	[[.FieldName]] [[.GoSearchType]] [[printf "%c" 96]]form:"[[.ColumnName]]"[[printf "%c" 96]]
[[end]]}

func List[[.StructName]]s(c *gin.Context) {
	var query [[.Snake]]ListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	page, pageSize := utils.ParsePagination(c)
	list, total, err := service.List[[.StructName]]s(page, pageSize[[range .SearchFields]], query.[[.FieldName]][[end]])
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

func Create[[.StructName]](c *gin.Context) {
	var m model.[[.StructName]]
	if err := utils.BindBusinessJSON(c, &m, [[.SchemaLiteral]]); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.Create[[.StructName]](&m); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, m)
}

func Update[[.StructName]](c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var m model.[[.StructName]]
	if err := utils.BindBusinessJSON(c, &m, [[.SchemaLiteral]]); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.Update[[.StructName]](id, &m); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func Delete[[.StructName]](c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.Delete[[.StructName]](id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
`

const routerTpl = `package router

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/api"
)

func init() {
	RegisterGenerated(register[[.StructName]])
}

func register[[.StructName]](g *gin.RouterGroup) {
	g.GET("/[[.Snake]]/list", api.List[[.StructName]]s)
	g.POST("/[[.Snake]]", api.Create[[.StructName]])
	g.PUT("/[[.Snake]]/:id", api.Update[[.StructName]])
	g.DELETE("/[[.Snake]]/:id", api.Delete[[.StructName]])
}
`

const apiTsTpl = `import request from '@/utils/request'

export interface [[.StructName]]Item {
  id: number
[[range .Fields]]  [[.JSONName]]: [[.TsType]]
[[end]]  createdAt: string
  updatedAt: string
}

export interface PageResult<T> {
  list: T[]
  total: number
}

export function list[[.StructName]]s(params: Record<string, any>) {
  return request.get<any, PageResult<[[.StructName]]Item>>('/[[.Snake]]/list', { params })
}

export function create[[.StructName]](data: Partial<[[.StructName]]Item>) {
  return request.post<any, null>('/[[.Snake]]', data)
}

export function update[[.StructName]](id: number, data: Partial<[[.StructName]]Item>) {
  return request.put<any, null>('/[[.Snake]]/' + id, data)
}

export function delete[[.StructName]](id: number) {
  return request.delete<any, null>('/[[.Snake]]/' + id)
}
`

const vueTpl = `<template>
  <div class="page">
    <div class="page-toolbar">
[[range .SearchFields]][[if eq .FormControl "number"]]      <el-input-number v-model="query.[[.JSONName]]" :controls="false" placeholder="[[.Title]]" class="toolbar-input" />
[[else]]      <el-input v-model="query.[[.JSONName]]" placeholder="[[.Title]]" clearable class="toolbar-input" @keyup.enter="handleSearch" @clear="handleSearch" />
[[end]][[end]][[if .SearchFields]]      <el-button type="primary" :icon="Search" @click="handleSearch">搜索</el-button>
      <el-button :icon="Refresh" @click="handleReset">重置</el-button>
[[end]]      <div class="toolbar-spacer" />
      <el-button type="primary" :icon="Plus" @click="openDialog()">新增[[.Business]]</el-button>
    </div>

    <div v-loading="loading">
      <vxe-table :data="list" height="auto" align="center" :row-config="{ isHover: true }">
        <vxe-column type="seq" title="#" width="60" />
[[range .ListFields]][[if eq .FormControl "switch"]]        <vxe-column title="[[.Title]]" width="100">
          <template #default="{ row }">
            <el-tag :type="row.[[.JSONName]] ? 'success' : 'info'" size="small">{{ row.[[.JSONName]] ? '启用' : '禁用' }}</el-tag>
          </template>
        </vxe-column>
[[else if eq .FormControl "datetime"]]        <vxe-column title="[[.Title]]" width="170">
          <template #default="{ row }">{{ formatTime(row.[[.JSONName]]) }}</template>
        </vxe-column>
[[else]]        <vxe-column field="[[.JSONName]]" title="[[.Title]]" min-width="120" />
[[end]][[end]]        <vxe-column title="创建时间" width="170">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </vxe-column>
        <vxe-column title="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
            <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
          </template>
        </vxe-column>
      </vxe-table>
    </div>

    <div class="page-pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.pageSize"
        background
        layout="total, sizes, prev, pager, next"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        @change="load"
      />
    </div>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑[[.Business]]' : '新增[[.Business]]'" width="560px" destroy-on-close>
[[if .SchemaDriven]]      <FormRender v-if="businessSchema" ref="formRef" :schema="businessSchema" :model-value="form" @update:model-value="Object.assign(form, $event)" />
      <el-alert v-else title="业务表单尚未初始化，请检查发布包安装结果" type="error" :closable="false" />
[[else]]      <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
[[range .FormFields]][[if eq .FormControl "input"]]        <el-form-item label="[[.Title]]" prop="[[.JSONName]]">
          <el-input v-model="form.[[.JSONName]]" placeholder="请输入[[.Title]]" />
        </el-form-item>
[[else if eq .FormControl "textarea"]]        <el-form-item label="[[.Title]]" prop="[[.JSONName]]">
          <el-input v-model="form.[[.JSONName]]" type="textarea" :rows="3" placeholder="请输入[[.Title]]" />
        </el-form-item>
[[else if eq .FormControl "number"]]        <el-form-item label="[[.Title]]" prop="[[.JSONName]]">
          <el-input-number v-model="form.[[.JSONName]]" :controls="false" placeholder="请输入[[.Title]]" style="width: 100%" />
        </el-form-item>
[[else if eq .FormControl "switch"]]        <el-form-item label="[[.Title]]">
          <el-switch v-model="form.[[.JSONName]]" active-text="启用" inactive-text="禁用" />
        </el-form-item>
[[else]]        <el-form-item label="[[.Title]]" prop="[[.JSONName]]">
          <el-date-picker v-model="form.[[.JSONName]]" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" placeholder="请选择[[.Title]]" style="width: 100%" />
        </el-form-item>
[[end]][[end]]      </el-form>
[[end]]      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
[[if .SchemaDriven]]import FormRender from '@/components/form-render/FormRender.vue'
import type { FormSchema } from '@/components/form-render/types'
import request from '@/utils/request'
[[else]]import type { FormInstance, FormRules } from 'element-plus'
[[end]]
import { Plus[[if .SearchFields]], Refresh, Search[[end]] } from '@element-plus/icons-vue'
import { create[[.StructName]], delete[[.StructName]], list[[.StructName]]s, update[[.StructName]] } from '@/api/[[.Snake]]'
import type { [[.StructName]]Item } from '@/api/[[.Snake]]'
import { formatTime } from '@/utils/format'

const loading = ref(false)
const list = ref<[[.StructName]]Item[]>([])
const total = ref(0)
const query = reactive<Record<string, any>>({
  page: 1,
  pageSize: 10,
[[range .SearchFields]]  [[.JSONName]]: [[.TsDefault]],
[[end]]})

const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
[[if .SchemaDriven]]const formRef = ref<InstanceType<typeof FormRender>>()
const businessSchema = ref<FormSchema>()
[[else]]const formRef = ref<FormInstance>()
[[end]]

const form = reactive<Record<string, any>>({
[[range .FormFields]]  [[.JSONName]]: [[.TsDefault]],
[[end]]})


[[if not .SchemaDriven]]const rules: FormRules = {
[[range .FormFields]][[if .Required]]  [[.JSONName]]: [{ required: true, message: '请填写[[.Title]]', trigger: 'blur' }],
[[end]][[end]]}

[[end]]
function buildParams() {
  const params: Record<string, any> = { page: query.page, pageSize: query.pageSize }
[[range .SearchFields]][[if eq .TsType "number"]]  if (query.[[.JSONName]] !== null && query.[[.JSONName]] !== undefined) {
    params.[[.JSONName]] = query.[[.JSONName]]
  }
[[else]]  if (query.[[.JSONName]]) {
    params.[[.JSONName]] = query.[[.JSONName]]
  }
[[end]][[end]]  return params
}

async function load() {
  loading.value = true
  try {
    const data = await list[[.StructName]]s(buildParams())
    list.value = data.list
    total.value = data.total
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  query.page = 1
  load()
}

function handleReset() {
[[range .SearchFields]][[if eq .TsType "number"]]  query.[[.JSONName]] = null
[[else]]  query.[[.JSONName]] = ''
[[end]][[end]]  query.page = 1
  load()
}

function openDialog(row?: [[.StructName]]Item) {
  editingId.value = row ? row.id : null
[[range .FormFields]]  form.[[.JSONName]] = row ? (row.[[.JSONName]] ?? [[.TsDefault]]) : [[.TsDefault]]
[[end]]  dialogVisible.value = true
}

async function handleSave() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    if (editingId.value) {
      await update[[.StructName]](editingId.value, { ...form })
      ElMessage.success('修改成功')
    } else {
      await create[[.StructName]]({ ...form })
      ElMessage.success('新增成功')
    }
    dialogVisible.value = false
    load()
  } finally {
    saving.value = false
  }
}

function handleDelete(row: [[.StructName]]Item) {
  ElMessageBox.confirm('确定删除该[[.Business]]记录吗？', '提示', { type: 'warning' })
    .then(async () => {
      await delete[[.StructName]](row.id)
      ElMessage.success('删除成功')
      load()
    })
    .catch(() => {})
}

[[if .SchemaDriven]]onMounted(async () => {
  businessSchema.value = await request.get<any, FormSchema>('/biz/schema/[[.ObjectKey]]')
  await load()
})[[else]]onMounted(load)[[end]]
</script>
`
