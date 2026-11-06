<template>
  <div class="login-container">
    <el-card class="login-card" shadow="always">
      <div class="login-title">FDE-Fast-Framework</div>
      <div class="login-subtitle">gin + gorm + jwt + casbin 后台管理脚手架</div>
      <el-form ref="formRef" :model="form" :rules="rules" size="large">
        <el-form-item prop="username">
          <el-input v-model="form.username" placeholder="用户名" :prefix-icon="User" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="密码"
            :prefix-icon="Lock"
            show-password
            @keyup.enter="handleLogin"
          />
        </el-form-item>
        <el-form-item v-if="captchaEnabled" prop="captchaCode">
          <div class="captcha-row">
            <el-input v-model="form.captchaCode" placeholder="验证码" class="captcha-input" @keyup.enter="handleLogin" />
            <img
              v-if="captchaPic"
              :src="captchaPic"
              class="captcha-img"
              title="点击刷新验证码"
              @click="loadCaptcha"
            />
          </div>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" class="login-btn" :loading="loading" @click="handleLogin">登 录</el-button>
        </el-form-item>
      </el-form>
      <div class="login-tip">默认账号 admin / 123456，演示账号 user / 123456</div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { Lock, User } from '@element-plus/icons-vue'
import { getCaptcha } from '@/api/auth'
import { useUserStore } from '@/stores/user'

const router = useRouter()
const route = useRoute()
const store = useUserStore()

const formRef = ref<FormInstance>()
const loading = ref(false)
const captchaEnabled = ref(false)
const captchaId = ref('')
const captchaPic = ref('')
const form = reactive({ username: 'admin', password: '', captchaCode: '' })

const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
  captchaCode: [
    {
      validator: (_rule: any, value: string, callback: any) => {
        if (captchaEnabled.value && !value) {
          callback(new Error('请输入验证码'))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
}

async function loadCaptcha() {
  try {
    const data = await getCaptcha()
    captchaEnabled.value = data.captchaEnabled
    if (data.captchaEnabled) {
      captchaId.value = data.captchaId
      captchaPic.value = data.picPath
      form.captchaCode = ''
    }
  } catch {
    /* 后端未就绪时静默 */
  }
}

async function handleLogin() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  loading.value = true
  try {
    await store.login({
      username: form.username,
      password: form.password,
      captchaId: captchaEnabled.value ? captchaId.value : undefined,
      captchaCode: captchaEnabled.value ? form.captchaCode : undefined,
    })
    ElMessage.success('登录成功')
    const redirect = (route.query.redirect as string) || '/'
    router.push(redirect)
  } catch {
    loadCaptcha()
  } finally {
    loading.value = false
  }
}

onMounted(loadCaptcha)
</script>

<style scoped>
.login-container {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1f3a5f 0%, #2c5aa0 50%, #409eff 100%);
}
.login-card {
  width: 400px;
  padding: 8px 12px;
}
.login-title {
  font-size: 24px;
  font-weight: 700;
  text-align: center;
  color: #303133;
}
.login-subtitle {
  font-size: 13px;
  text-align: center;
  color: #909399;
  margin: 8px 0 24px;
}
.login-btn {
  width: 100%;
}
.login-tip {
  font-size: 12px;
  text-align: center;
  color: #909399;
}
.captcha-row {
  display: flex;
  align-items: center;
  width: 100%;
}
.captcha-input {
  flex: 1;
}
.captcha-img {
  height: 40px;
  margin-left: 10px;
  cursor: pointer;
  border-radius: 4px;
  border: 1px solid #dcdfe6;
}
</style>
