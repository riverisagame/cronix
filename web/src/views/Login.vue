<!--
  Login.vue -- 登录页面组件。
  用户在这里输入用户名和密码，点击"Sign In"按钮进行登录。
  登录成功后，后端返回一个 token（令牌），前端把它存到浏览器本地存储，
  之后每次请求都带上这个 token，证明"我已经登录过了"。
-->

<template>
  <!--
    登录页整体是一个居中的容器：
    - display:flex + justify-content:center + align-items:center 让登录卡片在屏幕正中央
    - height:100vh 占满整个浏览器窗口高度
    - background:radial-gradient 深色背景，和整体暗色主题一致
  -->
  <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
  <div style="display:flex;justify-content:center;align-items:center;height:100vh;background:var(--bg-color)">
    <!--
      el-card：ElementPlus 的"卡片"组件，这里被磨砂玻璃化
      style="width:400px" 卡片宽度固定 400 像素
    -->
    <el-card class="data-card" style="width:440px;padding:24px;">
      <!-- 标题：Cronix Login，深色文字，居中显示 -->
      <h2 style="text-align:center;margin-bottom:32px;color:var(--text-main);font-size:24px;font-weight:600">Cronix Login</h2>

      <!--
        el-form：ElementPlus 的表单组件
        @submit.prevent="handleLogin" 是 Vue 的事件绑定语法：
          - @submit 监听表单提交事件
          - .prevent 是"事件修饰符"，相当于调用了 event.preventDefault()
            阻止浏览器的默认表单提交行为（默认会刷新页面），改为执行我们的 handleLogin 函数
      -->
      <el-form @submit.prevent="handleLogin">
        <!--
          el-form-item：表单项，label="Username" 在输入框前面显示标签文字
        -->
        <el-form-item label="Username">
          <!--
            el-input：输入框组件
            v-model="username" 是 Vue 的"双向绑定"指令：
              输入框里的内容变化时，username 变量的值也跟着变；
              username 变量的值变化时，输入框里的内容也跟着变。
            placeholder="admin" 在输入框为空时显示提示文字 "admin"
          -->
          <el-input v-model="username" placeholder="admin" data-testid="login-username" size="large" />
        </el-form-item>

        <!-- 密码输入框表单项 -->
        <el-form-item label="Password">
          <!--
            type="password" 让输入内容显示为圆点（保密）
            show-password 属性添加一个"小眼睛"按钮，点击可以切换显示/隐藏密码
            @keyup.enter="handleLogin" 监听键盘事件：
              当用户在密码框里按下 Enter 键时，自动触发登录
          -->
          <el-input v-model="password" type="password" show-password @keyup.enter="handleLogin" data-testid="login-password" size="large" />
        </el-form-item>

        <!-- 登录按钮表单项 -->
        <div style="margin-top:32px;">
          <!-- 
            登录按钮：
            type="primary" 是主要的蓝色按钮样式
            native-type="submit" 让按钮触发刚才的 @submit 表单提交
            style="width:100%" 让按钮铺满整个卡片宽度
          -->
          <el-button type="primary" native-type="submit" :loading="loading" style="width:100%" size="large" data-testid="login-submit">
            Sign In
          </el-button>
        </div>
      </el-form>

      <!--
        el-alert：ElementPlus 的"警告提示"组件
        v-if="error"：只有当 error 变量的值不为空时，才显示这个错误提示
        :title="error" 动态绑定提示文字内容
        type="error" 使用红色错误样式
        show-icon 显示一个图标
        :closable="false" 禁止用户手动关闭这个提示（只能等下次登录时自动清除）
      -->
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" data-testid="login-error" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
/**
 * ref 是 Vue 的"响应式数据"工具。
 * 用 ref() 包裹的数据，当它的值变化时，页面上显示的内容会自动更新。
 * 就像 Excel 表格里的公式：一个格子变了，所有关联的格子自动重算。
 */
import { ref } from 'vue'

// useRouter 是路由跳转工具，登录成功后用它跳转到首页
import { useRouter } from 'vue-router'

// 导入登录 API 函数（authAPI.login），用于发送登录请求给后端
import { authAPI } from '../api/index'

// 获取路由跳转工具实例
const router = useRouter()

/**
 * username 是一个"响应式"数据，默认值是 'admin'。
 * 它和模板里输入框的 v-model="username" 双向绑定。
 * ref('admin') 的意思是：创建一个响应式变量，初始值是 'admin'。
 */
const username = ref('admin')

// 密码变量，初始值为空字符串（用户需要自己输入）
const password = ref('')

/**
 * loading 是"加载状态"标记。
 * 初始值为 false（没在加载中）。
 * 当用户点击登录按钮后，设为 true（显示加载动画，防止重复点击）。
 * 登录完成（成功或失败）后，在 finally 里设回 false。
 */
const loading = ref(false)

/**
 * error 是"错误信息"变量。
 * 初始值为空字符串（没有错误）。
 * 登录失败时，把后端的错误提示文字存到这里，页面上就会显示红色警告。
 */
const error = ref('')

/**
 * handleLogin 函数：处理登录操作。
 * async 关键字表示这是一个"异步函数"（async function）。
 *
 * 什么是异步？
 *   网络请求需要时间（等待服务器回复），如果程序停下来等，页面就会卡住。
 *   用 async/await 可以让程序在等待的时候去做别的事，不卡页面。
 *   await 的意思是"等这个操作完成后再继续往下走"。
 */
async function handleLogin() {
  // 步骤一：把 loading 设为 true，按钮开始转圈，防止用户重复点击
  loading.value = true

  // 步骤二：清空之前的错误信息
  error.value = ''

  /**
   * try...catch...finally 是异常处理结构：
   *   - try 块里放"可能出错"的代码
   *   - catch 块里放"出错后怎么办"的处理代码
   *   - finally 块里的代码无论成功还是失败都会执行
   */
  try {
    /**
     * 调用登录 API，把用户名和密码发给后端。
     * await 等待后端回复，回复的数据存到 res 变量里。
     *
     * 后端返回的数据结构大致是：
     *   res.data.data.token = "eyJhbGciOiJIUzI1NiIs..."
     *   （token 是一长串加密字符串，相当于"电子身份证"）
     */
    const res = await authAPI.login(username.value, password.value)

    /**
     * 登录成功！把 token 保存到浏览器的本地存储（localStorage）中。
     * setItem('token', ...) 的意思是：把数据保存起来，键名是 'token'。
     * 以后每次发送请求时，request.ts 里的拦截器会自动读取并附上这个 token。
     */
    localStorage.setItem('token', res.data.data.token)

    /**
     * 跳转到首页（/ 路径 = Dashboard 仪表盘）。
     * router.push() 相当于在浏览器地址栏输入新地址。
     */
    router.push('/')
  } catch (e: any) {
    /**
     * 登录失败！把错误信息显示出来。
     *
     * e.response?.data?.message 是什么？
     *   后端返回的错误通常长这样：{ response: { data: { message: "密码错误" } } }
     *   用 ?. 可选链一层一层取，如果某层不存在就返回 undefined，不会报错。
     *   || 是"或"运算符：如果前面取不到值，就用 'Login failed' 作为默认错误信息。
     */
    error.value = e.response?.data?.message || 'Login failed'
  } finally {
    /**
     * 不管登录成功还是失败，最后都把 loading 设回 false。
     * 这样按钮的加载动画停止，用户可以再次点击。
     * finally 块一定会执行，非常适合做"清理工作"。
     */
    loading.value = false
  }
}
</script>
