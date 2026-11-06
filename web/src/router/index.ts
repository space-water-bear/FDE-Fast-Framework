import { createRouter, createWebHashHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import type { Component } from 'vue'
import type { MenuInfo } from '@/api/auth'
import { useUserStore } from '@/stores/user'
import { useTabsStore } from '@/stores/tabs'
import Layout from '@/layout/index.vue'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    icon?: string
    keepAlive?: boolean
    affix?: boolean
    hideTab?: boolean
  }
}

const views = import.meta.glob('../views/**/*.vue')

const constantRoutes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/login/index.vue'),
    meta: { title: '登录' },
  },
  {
    path: '/',
    name: 'Layout',
    component: Layout,
    redirect: '/dashboard',
    children: [
      {
        path: 'system/formgen',
        name: 'FormgenPreview',
        component: () => import('@/views/system/formgen/index.vue'),
        meta: { title: '表单预览' },
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/error/404.vue'),
    meta: { title: '404' },
  },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes: constantRoutes,
})

function buildRoutes(menus: MenuInfo[]): RouteRecordRaw[] {
  const routes: RouteRecordRaw[] = []
  for (const menu of menus) {
    if (menu.type === 2) continue
    const base = {
      path: menu.path,
      name: menu.name,
      meta: { title: menu.title, icon: menu.icon, keepAlive: menu.keepAlive, affix: menu.affix, hideTab: menu.hideTab },
    }
    let route: RouteRecordRaw
    if (menu.type === 1 && menu.component) {
      const key = '../views/' + menu.component + '.vue'
      const comp = views[key]
      route = { ...base, component: comp as unknown as Component }
    } else {
      route = { ...base, children: buildRoutes(menu.children || []) }
    }
    routes.push(route)
  }
  return routes
}

let routesLoaded = false
export function setupDynamicRoutes(menus: MenuInfo[]) {
  if (routesLoaded) return
  for (const route of buildRoutes(menus)) {
    router.addRoute('Layout', route)
  }
  routesLoaded = true
}

router.beforeEach(async (to) => {
  const title = to.meta.title
  document.title = title ? title + ' - FDE-Fast-Framework' : 'FDE-Fast-Framework'
  const store = useUserStore()
  if (!store.token) {
    if (to.path === '/login') return true
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login') return { path: '/' }
  if (!store.userInfo) {
    try {
      const data = await store.fetchUserInfo()
      setupDynamicRoutes(data.menus)
      return { path: to.path, query: to.query, replace: true }
    } catch {
      store.reset()
      return { path: '/login' }
    }
  }
  if (!routesLoaded) {
    setupDynamicRoutes(store.menus)
    return { path: to.path, query: to.query, replace: true }
  }
  return true
})

// 每次导航完成后同步标签导航栏
router.afterEach((to) => {
  const title = to.meta.title
  if (!title || to.path === '/login' || to.name === 'NotFound') return
  if (to.meta.hideTab) return
  if (!useUserStore().token) return
  const tabs = useTabsStore()
  tabs.addTab({ path: to.path, title, name: to.name as string | undefined, affix: !!to.meta.affix })
})

export default router
