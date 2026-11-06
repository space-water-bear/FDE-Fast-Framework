<template>
  <div class="table-page-tree">
    <div v-if="title" class="tree-title">{{ title }}</div>
    <div v-if="showSearch" class="tree-search">
      <el-input v-model="keyword" placeholder="请输入节点名称" clearable :prefix-icon="Search" @input="handleFilter" />
    </div>
    <el-scrollbar class="tree-scroll">
      <div class="tree-node-all" :class="{ active: currentId === null }" @click="selectAll">全部</div>
      <el-tree
        ref="treeRef"
        :data="data"
        :props="{ label: labelKey, children: childrenKey }"
        :node-key="nodeKey"
        highlight-current
        :default-expand-all="defaultExpandAll"
        :expand-on-click-node="false"
        :filter-node-method="filterNode"
        @node-click="onNodeClick"
      />
    </el-scrollbar>
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { Search } from '@element-plus/icons-vue'

const props = withDefaults(
  defineProps<{
    data: any[]
    labelKey?: string
    childrenKey?: string
    nodeKey?: string
    title?: string
    showSearch?: boolean
    defaultExpandAll?: boolean
  }>(),
  {
    labelKey: 'label',
    childrenKey: 'children',
    nodeKey: 'id',
    title: '',
    showSearch: true,
    defaultExpandAll: true,
  },
)

const emit = defineEmits<{
  (e: 'node-click', node: any | null): void
}>()

const treeRef = ref<any>()
const keyword = ref('')
const currentId = ref<any>(null)

function filterNode(value: string, data: any) {
  if (!value) return true
  const label = (data && data[props.labelKey]) || ''
  return String(label).includes(value)
}

function handleFilter() {
  treeRef.value?.filter(keyword.value)
}

function onNodeClick(node: any) {
  currentId.value = node ? node[props.nodeKey] : null
  emit('node-click', node)
}

function selectAll() {
  currentId.value = null
  nextTick(() => {
    treeRef.value?.setCurrentKey(null)
  })
  emit('node-click', null)
}
</script>

<style scoped>
.table-page-tree {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: #fff;
  border-radius: 4px;
  padding: 12px;
}
.tree-title {
  font-weight: 600;
  margin-bottom: 8px;
  color: #303133;
}
.tree-search {
  margin-bottom: 8px;
}
.tree-scroll {
  flex: 1;
  min-height: 0;
}
.tree-node-all {
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
  color: #606266;
}
.tree-node-all.active,
.tree-node-all:hover {
  background: #ecf5ff;
  color: #409eff;
}
</style>
