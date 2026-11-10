<template>
  <div class="credentials-panel">
    <p><strong>{{ title }}登录信息</strong> <span>仅本次返回，请安全保存；离开页面后不再保留。</span></p>
    <div class="credentials-fields">
      <label>用户名 <el-input :model-value="credentials.username" readonly /></label>
      <label>密码 <el-input :model-value="credentials.password" type="password" show-password readonly autocomplete="off" /></label>
      <el-button size="small" @click="copyLogin">复制登录信息</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import type { DevCredentials } from '@/api/development'

const props = defineProps<{ title: string; credentials: DevCredentials }>()

async function copyLogin(): Promise<void> {
  try {
    await navigator.clipboard.writeText('用户名：' + props.credentials.username + '\n密码：' + props.credentials.password)
    ElMessage.success('登录信息已复制，请安全保存')
  } catch {
    ElMessage.warning('无法访问剪贴板，请手动复制登录信息')
  }
}
</script>

<style scoped>
.credentials-panel { border: 1px solid var(--el-border-color); border-radius: 4px; padding: 10px 12px; margin: 12px 0; }
.credentials-panel p { margin: 0 0 8px; font-size: 12px; }
.credentials-panel p span { color: var(--el-text-color-secondary); }
.credentials-fields { display: flex; gap: 10px; align-items: end; flex-wrap: wrap; }
.credentials-fields label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; max-width: 260px; }
</style>
