import { defineStore } from 'pinia'
import { getUserInfo, login as loginApi, logout as logoutApi } from '@/api/auth'
import type { MenuInfo, RoleInfo, UserInfo } from '@/api/auth'

const TOKEN_KEY = 'fde-token'
const REFRESH_KEY = 'fde-refresh-token'

export const useUserStore = defineStore('user', {
  state: () => ({
    token: localStorage.getItem(TOKEN_KEY) || '',
    refreshToken: localStorage.getItem(REFRESH_KEY) || '',
    userInfo: null as UserInfo | null,
    roles: [] as RoleInfo[],
    menus: [] as MenuInfo[],
  }),
  actions: {
    async login(form: { username: string; password: string; captchaId?: string; captchaCode?: string }) {
      const data = await loginApi(form)
      this.token = data.token
      this.refreshToken = data.refreshToken
      localStorage.setItem(TOKEN_KEY, data.token)
      localStorage.setItem(REFRESH_KEY, data.refreshToken)
      await this.fetchUserInfo()
    },
    async fetchUserInfo() {
      const data = await getUserInfo()
      this.userInfo = data.user
      this.roles = data.roles
      this.menus = data.menus
      return data
    },
    async logout() {
      try {
        await logoutApi(this.refreshToken)
      } catch {
        /* ignore */
      }
      this.reset()
    },
    reset() {
      this.token = ''
      this.refreshToken = ''
      this.userInfo = null
      this.roles = []
      this.menus = []
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem(REFRESH_KEY)
    },
  },
})
