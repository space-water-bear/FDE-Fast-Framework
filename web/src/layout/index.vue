<template>
  <el-container class="layout">
    <el-aside width="220px" class="layout-aside">
      <div class="layout-logo">FDE-Fast-Framework</div>
      <el-scrollbar class="layout-menu">
        <el-menu
          :default-active="route.path"
          router
          background-color="#001529"
          text-color="#bfcbd9"
          active-text-color="#409eff"
        >
          <sidebar-item v-for="menu in store.menus" :key="menu.id" :menu="menu" />
        </el-menu>
      </el-scrollbar>
    </el-aside>
    <el-container>
      <el-header class="layout-header" height="56px">
        <div class="layout-title">{{ route.meta.title || '' }}</div>
        <el-dropdown @command="handleCommand">
          <span class="layout-user">
            <el-icon><UserFilled /></el-icon>
            <span>{{ displayName }}</span>
            <el-icon><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </el-header>
      <tabs-view />
      <el-main class="layout-main">
        <router-view v-slot="{ Component }">
          <component :is="Component" v-if="!route.meta.keepAlive" :key="route.path" />
          <keep-alive v-else>
            <component :is="Component" :key="route.path" />
          </keep-alive>
        </router-view>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowDown, UserFilled } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'
import SidebarItem from './components/SidebarItem.vue'
import TabsView from './components/TabsView.vue'

const route = useRoute()
const router = useRouter()
const store = useUserStore()

const displayName = computed(() => {
  const u = store.userInfo
  if (!u) return ''
  return u.nickname || u.username
})

function handleCommand(cmd: string) {
  if (cmd === 'logout') {
    store.logout().then(() => {
      router.push('/login')
    })
  }
}
</script>

<style scoped>
.layout {
  height: 100%;
}
.layout-aside {
  background: #001529;
  display: flex;
  flex-direction: column;
}
.layout-logo {
  height: 56px;
  line-height: 56px;
  text-align: center;
  color: #fff;
  font-size: 18px;
  font-weight: 600;
  letter-spacing: 1px;
}
.layout-menu {
  flex: 1;
  min-height: 0;
}
.layout-menu :deep(.el-menu) {
  border-right: none;
}
.layout-header {
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  box-shadow: 0 1px 4px rgba(0, 21, 41, 0.08);
  z-index: 1;
}
.layout-title {
  font-size: 15px;
  font-weight: 600;
  color: #303133;
}
.layout-user {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  color: #303133;
  outline: none;
}
.layout-main {
  background: #f0f2f5;
  padding: 16px;
  overflow: auto;
}
</style>
