<template>
  <el-sub-menu v-if="menu.children && menu.children.length > 0" :index="fullPath">
    <template #title>
      <el-icon v-if="iconComp"><component :is="iconComp" /></el-icon>
      <span>{{ menu.title }}</span>
    </template>
    <sidebar-item v-for="child in menu.children" :key="child.id" :menu="child" :base-path="fullPath" />
  </el-sub-menu>
  <el-menu-item v-else :index="fullPath">
    <el-icon v-if="iconComp"><component :is="iconComp" /></el-icon>
    <span>{{ menu.title }}</span>
  </el-menu-item>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import * as Icons from '@element-plus/icons-vue'
import type { MenuInfo } from '@/api/auth'

const props = defineProps<{ menu: MenuInfo; basePath?: string }>()

const fullPath = computed(() => {
  const base = props.basePath || ''
  const p = props.menu.path
  if (p.startsWith('/')) return p
  return base + '/' + p
})

const iconComp = computed(() => {
  const icons = Icons as unknown as Record<string, unknown>
  return icons[props.menu.icon] || null
})
</script>
