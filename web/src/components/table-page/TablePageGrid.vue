<template>
  <div class="table-page-grid">
    <div class="grid-body">
      <vxe-grid ref="gridRef" v-bind="gridProps" @toolbar-button-click="onToolbarButtonClick">
        <!-- 单元格插槽透传 -->
        <template v-for="(slot, name) in cellSlots" :key="name" #[name]="slotProps">
          <slot :name="name" v-bind="slotProps ?? {}" />
        </template>
        <!-- 左侧原生按钮后的自定义内容(#toolbar 插槽) -->
        <template v-if="$slots.toolbar" #toolbar_buttonSuffix>
          <slot name="toolbar" />
        </template>
        <!-- 右侧原生工具(刷新/全屏/列设置)后的自定义内容(#tools 插槽) -->
        <template v-if="$slots.tools" #toolbar_toolSuffix>
          <slot name="tools" />
        </template>
      </vxe-grid>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, useSlots, watch } from 'vue'
import type { TableFetch, ToolbarAction } from './types'

const props = withDefaults(
  defineProps<{
    columns: any[]
    fetch: TableFetch
    queryParams?: Record<string, any>
    toolbarActions?: ToolbarAction[]
    pageSize?: number
    autoLoad?: boolean
    height?: number | string
  }>(),
  {
    queryParams: () => ({}),
    toolbarActions: () => [{ code: 'add', label: '新增', type: 'primary' }],
    pageSize: 20,
    autoLoad: true,
    height: 'auto',
  },
)

const emit = defineEmits<{
  (e: 'add'): void
  (e: 'action', code: string): void
  (e: 'refresh'): void
}>()

const slots = useSlots()
const gridRef = ref<any>()
const currentParams = ref<Record<string, any>>({ ...(props.queryParams || {}) })

// vxe 按钮 status 支持 primary/success/info/warning/error，前端传的 danger 映射为 error
function mapStatus(type?: ToolbarAction['type']) {
  if (type === 'danger') return 'error'
  return type
}

const actionButtons = computed(() =>
  (props.toolbarActions || [])
    .filter((a) => !a.hidden)
    .map((a) => ({ code: a.code, content: a.label, status: mapStatus(a.type) })),
)

// 排除自定义工具栏插槽，其余(单元格)插槽原样透传给 vxe-grid
const cellSlots = computed(() => {
  const reserved = ['toolbar', 'tools']
  const result: Record<string, boolean> = {}
  for (const name of Object.keys(slots)) {
    if (!reserved.includes(name)) result[name] = true
  }
  return result
})

const gridProps = computed((): any => ({
  border: true,
  stripe: true,
  align: 'center',
  showOverflow: true,
  autoResize: true,
  height: props.height === 'auto' ? '100%' : props.height,
  columns: props.columns,
  rowConfig: { isHover: true, isCurrent: true },
  columnConfig: { resizable: true },
  // vxe-grid 原生工具栏: 刷新(query=保持当前页)/最大化/个性化列 均为内置工具
  toolbarConfig: {
    enabled: true,
    refresh: true,
    refreshOptions: { code: 'query' },
    zoom: true,
    custom: true,
    buttons: actionButtons.value,
    slots: {
      buttonSuffix: 'toolbar_buttonSuffix',
      toolSuffix: 'toolbar_toolSuffix',
    },
  },
  pagerConfig: {
    enabled: true,
    pageSize: props.pageSize,
    pageSizes: [20, 50, 100],
    layouts: ['Total', 'Sizes', 'PrevPage', 'Number', 'NextPage'],
    background: true,
  },
  proxyConfig: {
    enabled: true,
    autoLoad: props.autoLoad,
    response: { result: 'list', total: 'total' },
    ajax: {
      query: (params: any) =>
        props.fetch({
          page: params.page.currentPage,
          pageSize: params.page.pageSize,
          ...(currentParams.value || {}),
        }),
    },
  },
}))

watch(
  () => props.queryParams,
  (val) => {
    currentParams.value = { ...(val || {}) }
    reload(false)
  },
  { deep: true },
)

function reload(keepPage = true) {
  const grid = gridRef.value
  if (!grid) return
  if (keepPage) {
    grid.commitProxy('query')
  } else {
    grid.commitProxy('reload')
  }
}

function handleZoom() {
  gridRef.value?.zoom()
}

// 原生左侧按钮点击: 内置指令由 vxe 自行处理, 自定义 code 在这里转成组件事件
function onToolbarButtonClick(params: any) {
  const code = params && (params.code || (params.button && params.button.code))
  if (!code) return
  if (code === 'add') {
    emit('add')
    return
  }
  if (code === 'refresh') {
    reload()
    emit('refresh')
    return
  }
  emit('action', code)
}

defineExpose({
  reload,
  zoom: handleZoom,
  getGrid: () => gridRef.value,
})
</script>

<style scoped>
.table-page-grid {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: #fff;
  border-radius: 4px;
  padding: 10px 12px 12px;
  min-height: 0;
}
.grid-body {
  flex: 1;
  min-height: 0;
}
</style>
