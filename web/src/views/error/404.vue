<template>
  <div class="not-found">
    <div class="code">404</div>
    <div class="text">页面不存在或已被移除</div>
    <el-button type="primary" @click="goHome">返回首页</el-button>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()

function goHome() {
  router.push('/')
}
</script>

<style scoped>
.not-found {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  background: #f0f2f5;
}
.code {
  font-size: 72px;
  font-weight: 700;
  color: #409eff;
}
.text {
  color: #909399;
}
</style>
