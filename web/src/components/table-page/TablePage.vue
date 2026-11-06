<template>
  <div class="table-page" :class="{ 'is-tree': hasTree }">
    <div v-if="hasTree" class="table-page-aside" :style="{ width: treeWidth + 'px' }">
      <table-page-tree
        :data="treeData || []"
        :label-key="treeLabelKey"
        :node-key="treeNodeKey"
        :title="treeTitle"
        :show-search="treeSearchable"
        :default-expand-all="treeDefaultExpandAll"
        @node-click="onTreeClick"
      />
    </div>
    <div class="table-page-main">
      <table-page-search
        v-if="searchFields && searchFields.length"
        :fields="searchFields"
        @search="onSearch"
        @reset="onReset"
      />
      <table-page-grid
        ref="gridRef"
        :columns="columns"
        :fetch="fetch"
        :query-params="queryParams"
        :toolbar-actions="toolbarActions"
        :page-size="pageSize"
        :auto-load="autoLoad"
        :height="gridHeight"
        @add="onAdd"
        @action="onAction"
      >
        <template v-for="(slot, name) in cellSlots" :key="name" #[name]="slotProps">
          <slot :name="name" v-bind="slotProps ?? {}" />
        </template>
        <template v-if="$slots.toolbar" #toolbar>
          <slot name="toolbar" />
        </template>
        <template v-if="$slots.tools" #tools>
          <slot name="tools" />
        </template>
      </table-page-grid>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, useSlots } from 'vue'
import TablePageGrid from './TablePageGrid.vue'
import TablePageSearch from './TablePageSearch.vue'
import TablePageTree from './TablePageTree.vue'
import type { SearchField, TableFetch, ToolbarAction } from './types'

const props = withDefaults(
  defineProps<{
    searchFields?: SearchField[]
    columns: any[]
    fetch: TableFetch
    treeData?: any[]
    treeLabelKey?: string
    treeNodeKey?: string
    treeTitle?: string
    treeWidth?: number
    treeSearchable?: boolean
    treeDefaultExpandAll?: boolean
    treeQueryField?: string
    toolbarActions?: ToolbarAction[]
    pageSize?: number
    autoLoad?: boolean
    gridHeight?: number | string
  }>(),
  {
    searchFields: () => [],
    treeData: undefined,
    treeLabelKey: 'label',
    treeNodeKey: 'id',
    treeTitle: '',
    treeWidth: 260,
    treeSearchable: true,
    treeDefaultExpandAll: true,
    treeQueryField: 'treeId',
    toolbarActions: undefined,
    pageSize: 20,
    autoLoad: true,
    gridHeight: 'auto',
  },
)

const emit = defineEmits<{
  (e: 'search', query: Record<string, any>): void
  (e: 'reset'): void
  (e: 'add'): void
  (e: 'action', code: string): void
  (e: 'tree-change', node: any | null): void
}>()

const slots = useSlots()
const gridRef = ref<any>()

const hasTree = computed(() => Array.isArray(props.treeData) && props.treeData.length > 0)

const cellSlots = computed(() => {
  const reserved = ['toolbar', 'tools']
  const result: Record<string, boolean> = {}
  for (const name of Object.keys(slots)) {
    if (!reserved.includes(name)) result[name] = true
  }
  return result
})

const searchParams = ref<Record<string, any>>({})
const treeParam = ref<Record<string, any>>({})

const queryParams = computed(() => ({ ...searchParams.value, ...treeParam.value }))

function onSearch(payload: Record<string, any>) {
  searchParams.value = { ...payload }
  emit('search', { ...payload })
}

function onReset() {
  searchParams.value = {}
  emit('reset')
}

function onTreeClick(node: any | null) {
  if (!node) {
    treeParam.value = {}
  } else {
    treeParam.value = { [props.treeQueryField]: node[props.treeNodeKey] }
  }
  emit('tree-change', node)
}

function onAdd() {
  emit('add')
}

function onAction(code: string) {
  emit('action', code)
}

defineExpose({
  reload: () => gridRef.value?.reload(),
  zoom: () => gridRef.value?.zoom(),
  getGrid: () => gridRef.value?.getGrid(),
})
</script>

<style scoped>
.table-page {
  display: flex;
  gap: 12px;
  height: 100%;
  min-height: 0;
}
.table-page-aside {
  flex: 0 0 auto;
  min-height: 0;
}
.table-page-main {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
</style>
