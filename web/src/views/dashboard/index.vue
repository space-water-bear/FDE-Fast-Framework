<template>
  <div class="dashboard">
    <el-card shadow="never" class="welcome">
      <div class="hello">你好，{{ store.userInfo?.nickname || store.userInfo?.username }}</div>
      <div class="desc">欢迎使用 FDE-Fast-Framework —— 基于 gin + gorm + jwt + casbin 的后台管理系统脚手架</div>
    </el-card>
    <div class="cards">
      <el-card shadow="hover" class="card" @click="go('/system/user')">
        <el-icon :size="36" color="#409eff"><User /></el-icon>
        <div class="card-title">用户管理</div>
        <div class="card-desc">用户增删改查与角色分配</div>
      </el-card>
      <el-card shadow="hover" class="card" @click="go('/system/role')">
        <el-icon :size="36" color="#67c23a"><UserFilled /></el-icon>
        <div class="card-title">角色管理</div>
        <div class="card-desc">角色与菜单/接口权限绑定</div>
      </el-card>
      <el-card shadow="hover" class="card" @click="go('/system/menu')">
        <el-icon :size="36" color="#e6a23c"><Menu /></el-icon>
        <div class="card-title">菜单管理</div>
        <div class="card-desc">动态菜单与 casbin 策略</div>
      </el-card>
      <el-card shadow="hover" class="card" @click="go('/system/dict')">
        <el-icon :size="36" color="#909399"><Collection /></el-icon>
        <div class="card-title">字典管理</div>
        <div class="card-desc">字典类型与数据维护</div>
      </el-card>
      <el-card shadow="hover" class="card" @click="go('/system/oplog')">
        <el-icon :size="36" color="#f56c6c"><Document /></el-icon>
        <div class="card-title">操作日志</div>
        <div class="card-desc">接口调用留痕与审计</div>
      </el-card>
      <el-card shadow="hover" class="card" @click="go('/system/codegen')">
        <el-icon :size="36" color="#b88230"><MagicStick /></el-icon>
        <div class="card-title">代码生成器</div>
        <div class="card-desc">一键生成前后端 CRUD</div>
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { Collection, Document, MagicStick, Menu, User, UserFilled } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'

const router = useRouter()
const store = useUserStore()

function go(path: string) {
  router.push(path)
}
</script>

<style scoped>
.dashboard {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.welcome .hello {
  font-size: 20px;
  font-weight: 600;
  margin-bottom: 8px;
}
.welcome .desc {
  color: #909399;
}
.cards {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
}
.card {
  cursor: pointer;
  text-align: center;
  padding: 8px 0;
}
.card-title {
  font-size: 16px;
  font-weight: 600;
  margin: 12px 0 6px;
}
.card-desc {
  font-size: 12px;
  color: #909399;
}
</style>
