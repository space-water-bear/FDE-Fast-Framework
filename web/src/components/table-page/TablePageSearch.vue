<template>
  <el-form inline class="table-page-search">
    <el-form-item v-for="f in fields" :key="f.field" :label="f.label">
      <el-input
        v-if="normalizedType(f) === 'input'"
        v-model="form[f.field]"
        :placeholder="f.placeholder || '请输入' + f.label"
        :clearable="f.clearable !== false"
        :style="{ width: (f.width || 200) + 'px' }"
        @keyup.enter="handleSearch"
      />
      <el-input-number
        v-else-if="normalizedType(f) === 'number'"
        v-model="form[f.field]"
        :controls="false"
        :style="{ width: (f.width || 180) + 'px' }"
      />
      <el-select
        v-else-if="normalizedType(f) === 'select'"
        v-model="form[f.field]"
        :placeholder="f.placeholder || '请选择' + f.label"
        :clearable="f.clearable !== false"
        :style="{ width: (f.width || 200) + 'px' }"
      >
        <el-option v-for="o in f.options || []" :key="String(o.value)" :label="o.label" :value="o.value" />
      </el-select>
      <el-date-picker
        v-else-if="f.type === 'daterange'"
        v-model="form[f.field]"
        type="daterange"
        value-format="YYYY-MM-DD"
        start-placeholder="开始日期"
        end-placeholder="结束日期"
        :style="{ width: (f.width || 260) + 'px' }"
      />
      <el-date-picker
        v-else-if="f.type === 'datetimerange'"
        v-model="form[f.field]"
        type="datetimerange"
        value-format="YYYY-MM-DD HH:mm:ss"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        :style="{ width: (f.width || 360) + 'px' }"
      />
    </el-form-item>
    <el-form-item>
      <el-button type="primary" :icon="Search" @click="handleSearch">查询</el-button>
      <el-button :icon="Refresh" @click="handleReset">重置</el-button>
    </el-form-item>
  </el-form>
</template>

<script setup lang="ts">
import { reactive, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import type { SearchField, SearchFieldType } from './types'

const props = defineProps<{ fields: SearchField[] }>()
const emit = defineEmits<{
  (e: 'search', payload: Record<string, any>): void
  (e: 'reset', payload: Record<string, any>): void
}>()

const form = reactive<Record<string, any>>({})

function normalizedType(f: SearchField): SearchFieldType {
  return f.type || 'input'
}

function initForm() {
  const next: Record<string, any> = {}
  for (const f of props.fields) {
    if (f.defaultValue !== undefined) {
      next[f.field] = f.defaultValue
    } else if (f.type === 'daterange' || f.type === 'datetimerange') {
      next[f.field] = null
    } else {
      next[f.field] = ''
    }
  }
  Object.keys(form).forEach((k) => delete form[k])
  Object.assign(form, next)
}

watch(() => props.fields, initForm, { immediate: true, deep: true })

function handleSearch() {
  emit('search', { ...form })
}

function handleReset() {
  initForm()
  emit('reset', { ...form })
}
</script>

<style scoped>
.table-page-search {
  padding: 12px 12px 0;
  background: #fff;
  border-radius: 4px;
  margin-bottom: 6px;
}
</style>
