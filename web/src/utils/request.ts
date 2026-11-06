import axios from 'axios'
import { ElMessage } from 'element-plus'

interface ApiResponse {
  code: number
  msg: string
  data: any
}

const TOKEN_KEY = 'fde-token'
const REFRESH_KEY = 'fde-refresh-token'

const service = axios.create({
  baseURL: '/api',
  timeout: 15000,
})

service.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers['x-token'] = token
  }
  return config
})

function persistTokens(token: string, refreshToken: string) {
  localStorage.setItem(TOKEN_KEY, token)
  localStorage.setItem(REFRESH_KEY, refreshToken)
}

function clearTokens() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_KEY)
}

function redirectLogin() {
  clearTokens()
  if (!location.hash.includes('/login')) {
    location.hash = '#/login'
  }
}

// 并发 401 共享同一次刷新
let refreshPromise: Promise<string> | null = null

function doRefresh(): Promise<string> {
  const rt = localStorage.getItem(REFRESH_KEY)
  if (!rt) {
    return Promise.reject(new Error('no-refresh-token'))
  }
  return axios
    .post('/api/auth/refresh', { refreshToken: rt })
    .then((res: any) => {
      const body = res.data as ApiResponse
      if (body.code !== 0 || !body.data) {
        throw new Error(body.msg || '刷新令牌失败')
      }
      persistTokens(body.data.token, body.data.refreshToken)
      return body.data.token as string
    })
    .finally(() => {
      refreshPromise = null
    })
}

service.interceptors.response.use(
  (response: any) => {
    const res = response.data as ApiResponse
    if (res.code === 0) {
      return res.data
    }
    if (res.code === 40004) {
      redirectLogin()
    } else {
      ElMessage.error(res.msg || '请求失败')
    }
    return Promise.reject(new Error(res.msg || '请求失败'))
  },
  async (error: any) => {
    const config = error.config || {}
    const status = error.response ? error.response.status : 0
    if (status === 401 && !config._retried) {
      if (!refreshPromise) {
        refreshPromise = doRefresh()
      }
      try {
        const newToken = await refreshPromise
        config._retried = true
        config.headers = config.headers || {}
        config.headers['x-token'] = newToken
        return service(config)
      } catch (e) {
        ElMessage.error('登录已过期，请重新登录')
        redirectLogin()
        return Promise.reject(e)
      }
    }
    const silent = status === 404 && config.silent404 === true
    if (status !== 401 && !silent) {
      ElMessage.error(error.message || '网络错误')
    }
    return Promise.reject(error)
  },
)

export default service
