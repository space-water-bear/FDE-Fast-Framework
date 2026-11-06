<template>
  <div class="tabs-view">
    <el-scrollbar class="tabs-scroll">
      <div class="tabs-wrap">
        <div
          v-for="tab in tabsStore.tabs"
          :key="tab.path"
          class="tab-item"
          :class="{ active: tab.path === tabsStore.activePath }"
          @click="handleClick(tab)"
          @contextmenu.prevent="openContextMenu($event, tab)"
        >
          <span class="tab-title">{{ tab.title }}</span>
          <el-icon v-if="!tab.affix" class="tab-close" @click.stop="handleClose(tab)"><Close /></el-icon>
        </div>
      </div>
    </el-scrollbar>

    <ul v-show="menuVisible" class="tabs-context-menu" :style="{ left: menuLeft + 'px', top: menuTop + 'px' }">
      <li @click="handleCloseCurrent">关闭当前</li>
      <li @click="handleCloseOthers">关闭其他</li>
      <li @click="handleCloseAll">关闭全部</li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Close } from '@element-plus/icons-vue'
import { useTabsStore } from '@/stores/tabs'
import type { TabItem } from '@/stores/tabs'

const route = useRoute()
const router = useRouter()
const tabsStore = useTabsStore()

const menuVisible = ref(false)
const menuLeft = ref(0)
const menuTop = ref(0)
const menuTab = ref<TabItem | null>(null)

function handleClick(tab: TabItem) {
  if (tab.path !== route.path) {
    router.push(tab.path)
  }
}

function handleClose(tab: TabItem) {
  const next = tabsStore.removeTab(tab.path)
  if (tab.path === route.path) {
    router.push(next)
  }
  hideMenu()
}

function handleCloseCurrent() {
  if (!menuTab.value) return
  handleClose(menuTab.value)
}

function handleCloseOthers() {
  const keep = menuTab.value ? menuTab.value.path : route.path
  const next = tabsStore.closeOthers(keep)
  if (route.path !== keep) {
    router.push(next)
  }
  hideMenu()
}

function handleCloseAll() {
  const next = tabsStore.closeAll()
  router.push(next)
  hideMenu()
}

function openContextMenu(e: MouseEvent, tab: TabItem) {
  menuTab.value = tab
  menuLeft.value = e.clientX
  menuTop.value = e.clientY
  menuVisible.value = true
}

function hideMenu() {
  menuVisible.value = false
  menuTab.value = null
}

function onGlobalClick() {
  if (menuVisible.value) hideMenu()
}

onMounted(() => {
  document.addEventListener('click', onGlobalClick)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onGlobalClick)
})
</script>

<style scoped>
.tabs-view {
  position: relative;
  height: 40px;
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
  box-shadow: 0 1px 2px rgba(0, 21, 41, 0.06);
}
.tabs-scroll {
  height: 40px;
}
.tabs-wrap {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 40px;
  padding: 0 10px;
  white-space: nowrap;
}
.tab-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  background: #f5f7fa;
  color: #606266;
  font-size: 13px;
  cursor: pointer;
  user-select: none;
  flex: 0 0 auto;
}
.tab-item:hover {
  color: #409eff;
  border-color: #b3d8ff;
}
.tab-item.active {
  color: #409eff;
  background: #ecf5ff;
  border-color: #409eff;
}
.tab-close {
  font-size: 12px;
  border-radius: 50%;
  padding: 1px;
}
.tab-close:hover {
  background: #409eff;
  color: #fff;
}
.tabs-context-menu {
  position: fixed;
  z-index: 3000;
  margin: 0;
  padding: 4px 0;
  list-style: none;
  background: #fff;
  border-radius: 4px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.15);
  font-size: 13px;
  color: #303133;
  min-width: 100px;
}
.tabs-context-menu li {
  padding: 6px 14px;
  cursor: pointer;
}
.tabs-context-menu li:hover {
  background: #f5f7fa;
  color: #409eff;
}
</style>
