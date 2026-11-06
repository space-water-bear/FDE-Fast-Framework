import { defineStore } from 'pinia'

export interface TabItem {
  path: string
  title: string
  name?: string
  affix?: boolean
}

export const HOME_PATH = '/dashboard'
const HOME_TAB: TabItem = { path: HOME_PATH, title: '首页' }

export const useTabsStore = defineStore('tabs', {
  state: () => ({
    tabs: [] as TabItem[],
    activePath: '',
  }),
  actions: {
    ensureHome() {
      if (!this.tabs.some((t) => t.path === HOME_PATH)) {
        this.tabs.unshift({ ...HOME_TAB })
      }
    },
    addTab(tab: TabItem) {
      if (!tab.path || tab.path === '/login') return
      this.ensureHome()
      const exists = this.tabs.find((t) => t.path === tab.path)
      if (exists) {
        exists.title = tab.title
        if (tab.affix !== undefined) exists.affix = tab.affix
      } else {
        this.tabs.push({ ...tab })
      }
      this.activePath = tab.path
    },
    // 关闭标签, 返回关闭后应跳转的路径(固定标签/不存在时不变)
    removeTab(path: string): string {
      const idx = this.tabs.findIndex((t) => t.path === path)
      if (idx === -1) return this.activePath
      if (this.tabs[idx].affix) return this.activePath
      this.tabs.splice(idx, 1)
      if (this.tabs.length === 0) {
        this.tabs.push({ ...HOME_TAB })
      }
      if (path === this.activePath) {
        const target = this.tabs[idx] || this.tabs[idx - 1] || this.tabs[0]
        this.activePath = target.path
      }
      return this.activePath
    },
    // 关闭除 keepPath 外的所有标签(固定标签始终保留)
    closeOthers(keepPath: string): string {
      this.tabs = this.tabs.filter((t) => t.path === keepPath || t.path === HOME_PATH || t.affix)
      if (this.tabs.length === 0) {
        this.tabs.push({ ...HOME_TAB })
      }
      this.activePath = keepPath
      return keepPath
    },
    // 关闭全部(固定标签保留), 返回应跳转的路径
    closeAll(): string {
      const affixTabs = this.tabs.filter((t) => t.affix)
      this.tabs = affixTabs.length ? affixTabs : []
      this.ensureHome()
      this.activePath = this.tabs[0].path
      return this.activePath
    },
  },
})
