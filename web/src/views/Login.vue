<template>
  <div class="cex-landing">
    <main class="landing-content">
      <!-- Login Form (Glassmorphism) -->
      <div class="login-container">
        <div class="glass-card login-box">
          <div class="logo-simple">
            <el-icon><Monitor /></el-icon>
            <span class="logo-text">CRONIX</span>
          </div>
          <h2 class="login-title">Welcome Back</h2>
          <p class="login-desc">Sign in to access your secure dashboard</p>

          <el-alert
            v-if="error"
            :title="error"
            type="error"
            show-icon
            :closable="false"
            class="mb-4 glass-alert"
            data-testid="login-error"
          />

          <el-form @submit.prevent="handleLogin" class="login-form">
            <el-form-item>
              <el-input
                v-model="username"
                placeholder="Email or Username"
                size="large"
                data-testid="login-username"
                class="glass-input"
              >
                <template #prefix>
                  <el-icon><User /></el-icon>
                </template>
              </el-input>
            </el-form-item>

            <el-form-item>
              <el-input
                v-model="password"
                type="password"
                placeholder="Password"
                show-password
                size="large"
                data-testid="login-password"
                class="glass-input"
              >
                <template #prefix>
                  <el-icon><Key /></el-icon>
                </template>
              </el-input>
            </el-form-item>

            <div class="form-actions">
              <el-button 
                type="primary" 
                native-type="submit" 
                class="w-full cex-btn" 
                size="large"
                :loading="loading"
                data-testid="login-submit"
              >
                {{ loading ? 'Authenticating...' : 'Secure Login' }}
              </el-button>
            </div>
          </el-form>
          
          <div class="login-footer">
            <a href="#" class="forgot-link">Forgot Password?</a>
            <p class="signup-prompt">New to Cronix? <a href="#" class="signup-link">Create an account</a></p>
          </div>
        </div>
      </div>
    </main>

    <!-- Background Effects -->
    <div class="bg-glow glow-1"></div>
    <div class="bg-glow glow-2"></div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Monitor, User, Key } from '@element-plus/icons-vue'
import { authAPI } from '../api'

const router = useRouter()
const username = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')

async function handleLogin() {
  if (!username.value || !password.value) {
    error.value = 'Please enter username and password'
    return
  }

  try {
    loading.value = true
    error.value = ''
    const res = await authAPI.login(username.value, password.value)
    
    // axios 返回的 res.data 才是服务端的 response body，即 { code: 0, data: { token: '...' } }
    if (res.data && res.data.code === 0 && res.data.data && res.data.data.token) {
      localStorage.setItem('token', res.data.data.token)
      router.push('/')
    } else if (res.data && res.data.token) {
      // 兼容某些直接返回 token 的情况
      localStorage.setItem('token', res.data.token)
      router.push('/')
    } else {
      error.value = 'Login failed: Invalid response from server'
    }
  } catch (err: any) {
    console.error('Login error:', err)
    error.value = err.response?.data?.message || 'Invalid credentials or network error'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.cex-landing {
  min-height: 100vh;
  background-color: var(--cex-bg-dark);
  color: var(--text-main);
  position: relative;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* Background Glowing Orbs */
.bg-glow {
  position: absolute;
  border-radius: 50%;
  filter: blur(120px);
  z-index: 0;
  opacity: 0.4;
  animation: pulseBg 10s infinite alternate;
}
.glow-1 {
  width: 600px;
  height: 600px;
  background: var(--cex-accent-purple);
  top: -200px;
  right: -100px;
}
.glow-2 {
  width: 500px;
  height: 500px;
  background: var(--cex-primary-gold);
  bottom: -200px;
  left: -100px;
  animation-delay: -5s;
}

@keyframes pulseBg {
  0% { transform: scale(1) translate(0, 0); opacity: 0.3; }
  100% { transform: scale(1.2) translate(-50px, 50px); opacity: 0.5; }
}

.landing-content {
  position: relative;
  z-index: 10;
  flex: 1;
  display: flex;
  justify-content: center;
  align-items: center;
  padding: 40px 20px;
}

/* Login Form Area */
.login-container {
  display: flex;
  justify-content: center;
  width: 100%;
}

.logo-simple {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  font-family: var(--font-display);
  font-size: 32px;
  font-weight: 700;
  color: var(--text-main);
  letter-spacing: 2px;
  margin-bottom: 24px;
}
.logo-simple .el-icon {
  color: var(--cex-primary-gold);
}

.glass-card {
  background: rgba(30, 41, 59, 0.6);
  border: 1px solid rgba(255, 255, 255, 0.1);
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border-radius: 24px;
  padding: 50px 60px;
  width: 100%;
  max-width: 500px; /* Made it a bit larger */
}

.login-title {
  font-family: var(--font-display);
  font-size: 32px;
  margin: 0 0 8px 0;
  text-align: center;
}

.login-desc {
  color: var(--text-secondary);
  text-align: center;
  margin: 0 0 32px 0;
  font-size: 16px;
}

.glass-input :deep(.el-input__wrapper) {
  background: rgba(15, 23, 42, 0.6) !important;
  border-radius: 12px;
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.1) !important;
  padding: 0 16px;
  height: 56px; /* Larger input */
}
.glass-input :deep(.el-input__wrapper.is-focus) {
  box-shadow: inset 0 0 0 1px var(--cex-primary-gold) !important;
  background: rgba(15, 23, 42, 0.8) !important;
}
.glass-input :deep(.el-input__inner) {
  color: var(--text-main);
  font-size: 16px;
}
.glass-input :deep(.el-input__prefix-inner) {
  font-size: 20px;
}

.cex-btn {
  height: 56px;
  border-radius: 12px;
  font-size: 18px;
  font-weight: 600;
  letter-spacing: 0.5px;
  background: linear-gradient(135deg, var(--cex-primary-gold), #D97706) !important;
  border: none !important;
  color: #fff;
  transition: all 0.3s ease;
  margin-top: 10px;
}
.cex-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 20px -6px rgba(245, 158, 11, 0.6);
}

.glass-alert {
  background: rgba(239, 68, 68, 0.1) !important;
  border: 1px solid rgba(239, 68, 68, 0.2);
  border-radius: 8px;
}

.login-footer {
  margin-top: 24px;
  text-align: center;
  font-size: 15px;
}
.forgot-link {
  color: var(--text-secondary);
  text-decoration: none;
  transition: color 0.2s;
  display: inline-block;
  margin-bottom: 16px;
}
.forgot-link:hover {
  color: var(--text-main);
}
.signup-prompt {
  color: var(--text-secondary);
  margin: 0;
}
.signup-link {
  color: var(--cex-primary-gold);
  text-decoration: none;
  font-weight: 500;
  margin-left: 4px;
}
.signup-link:hover {
  text-decoration: underline;
}

.mb-4 { margin-bottom: 16px; }
.w-full { width: 100%; }

/* Responsive adjustments */
@media (max-width: 600px) {
  .glass-card {
    padding: 40px 30px;
  }
}
</style>
