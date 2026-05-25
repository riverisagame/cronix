<!--
  App.vue -- 整个应用的"外壳"组件（根组件）。
  它决定了页面的整体布局结构：左侧导航菜单 + 顶部标题栏 + 中间主内容区域。
  登录页单独显示（没有侧边栏），其他页面都套在这个布局里。
-->

<template>
  <!--
    v-if="isLoginPage" 是一个条件判断：
    如果当前页面是登录页，就只显示一个全屏的深色容器，里面放路由切换内容（即登录表单）
    这样做是为了让登录页看起来独立、简洁，不显示左侧菜单栏
  -->
  <div v-if="isLoginPage" style="height:100vh;background:#141414">
    <!--
      <router-view /> 是一个"占位符"组件。
      Vue Router（路由系统）会根据当前浏览器地址，决定在这一块显示哪个页面组件。
      比如地址是 /login 就显示 Login.vue，地址是 /tasks 就显示 TaskList.vue
    -->
    <router-view />
  </div>

  <!--
    v-else：如果不是登录页（即用户已登录的状态），就用 el-container 搭一个标准的后台管理布局
    el-container 是 ElementPlus 提供的容器组件，用于搭建页面整体结构
    style="height:100vh" 让容器撑满整个浏览器窗口高度
  -->
  <el-container v-else style="height:100vh;background:#141414">
    <!--
      el-aside：左侧侧边栏区域，宽度固定为 220 像素
    -->
    <el-aside width="220px">
      <!--
        el-menu：ElementPlus 提供的导航菜单组件
        :default-active="activeMenu" 动态绑定当前激活的菜单项（根据网址自动高亮）
        router 属性开启路由模式 -- 点击菜单项时自动跳转到对应路径，不需要手动写跳转代码
        background-color：菜单背景色（深灰色）
        text-color：菜单文字颜色（浅灰色）
        active-text-color：当前选中菜单的文字颜色（蓝色）
        style="height:100%;border-right:none" 让菜单撑满整个侧边栏高度，去掉右边框线
      -->
      <el-menu
        :default-active="activeMenu"
        router
        background-color="#1d1e1f"
        text-color="#a3a6ad"
        active-text-color="#409EFF"
        style="height:100%;border-right:none"
      >
        <!--
          应用名称和 Logo：一个居中显示的标题 "Cronix"
          letter-spacing 让字母之间有一点间距，看起来更美观
        -->
        <div style="padding:18px 20px;font-weight:700;font-size:17px;color:#e5e7eb;text-align:center;letter-spacing:1px">Cronix</div>

        <!--
          菜单项 index="/"：点击后跳转到网站根路径 /（即 Dashboard 仪表盘页）
          el-icon 包裹图标组件，Odometer 是一个仪表盘图标
        -->
        <el-menu-item index="/">
          <el-icon><Odometer /></el-icon>
          <span>Dashboard</span>
        </el-menu-item>

        <!-- 菜单项：跳转到 /tasks（任务管理页） -->
        <el-menu-item index="/tasks">
          <el-icon><List /></el-icon>
          <span>Tasks</span>
        </el-menu-item>

        <!-- 菜单项：跳转到 /groups（任务组管理页） -->
        <el-menu-item index="/groups">
          <el-icon><Grid /></el-icon>
          <span>Groups</span>
        </el-menu-item>

        <!-- 菜单项：跳转到 /logs（执行日志页） -->
        <el-menu-item index="/logs">
          <el-icon><Files /></el-icon>
          <span>Execution Logs</span>
        </el-menu-item>

        <!-- 菜单项：跳转到 /settings（设置页） -->
        <el-menu-item index="/settings">
          <el-icon><Setting /></el-icon>
          <span>Settings</span>
        </el-menu-item>

        <!--
          退出登录菜单项：点击后不跳转路由，而是执行 doLogout 函数来清除登录状态
          @click="doLogout" 表示点击时触发 doLogout 方法
        -->
        <el-menu-item index="/login" @click="doLogout">
          <el-icon><SwitchButton /></el-icon>
          <span>Logout</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <!--
      右侧主体区域也是一个 el-container，包含页面顶部标题栏和下方主内容区
    -->
    <el-container>
      <!--
        页面顶部栏（el-header）：高度 52px，用 flex 布局使内容垂直居中
        border-bottom 在底部加一条分隔线，把标题栏和内容区分开
      -->
      <el-header style="display:flex;align-items:center;border-bottom:1px solid #333;background:#1d1e1f" height="52px">
        <!--
          标题文字：显示当前页面名称（Dashboard / Tasks / Settings 等）
          {{ pageTitle }} 是双花括号插值语法，把 JavaScript 变量的值显示在这里
        -->
        <span style="font-size:13px;color:#a3a6ad;letter-spacing:1px">{{ pageTitle }}</span>
      </el-header>

      <!--
        主内容区域（el-main）：页面切换动画的核心区域
        background 设置背景色为深色
      -->
      <el-main style="background:#141414">
        <!--
          router-view 的 v-slot 写法：拿到当前要显示的组件对象（Component）
          配合 <transition> 实现页面切换时的淡入淡出动画效果
          mode="out-in" 表示旧页面先淡出，新页面再淡入（更平滑）
        -->
        <router-view v-slot="{ Component }">
          <!--
            <transition> 是 Vue 的过渡动画组件
            name="fade" 对应下面 CSS 里定义的 .fade-enter-active 等样式
          -->
          <transition name="fade" mode="out-in">
            <!--
              <component :is="Component" /> 是 Vue 的动态组件语法
              根据 Component 的实际值来渲染对应的页面组件
            -->
            <component :is="Component" />
          </transition>
        </router-view>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
/**
 * <script setup> 是 Vue 3 的"语法糖"写法，写起来更简洁。
 * lang="ts" 表示这个 script 块使用 TypeScript 语言（带类型检查的 JavaScript）。
 */

// computed 是 Vue 的"计算属性"工具：它的值会根据依赖的数据自动重新计算，无需手动更新
import { computed } from 'vue'

// useRoute 获取当前路由信息（路径、参数等），useRouter 是路由跳转工具（用来切换页面）
import { useRoute, useRouter } from 'vue-router'

// 从 ElementPlus 图标库中引入我们需要用到的 5 个图标组件
import { Odometer, List, Grid, Files, Setting, SwitchButton } from '@element-plus/icons-vue'

// useRoute() 返回当前路由对象，通过 route.path 可以知道用户正在访问哪个网址路径
const route = useRoute()

// useRouter() 返回路由跳转工具，通过 router.push('/login') 可以跳转到登录页
const router = useRouter()

/**
 * activeMenu 是一个"计算属性"。
 * 它根据当前网址路径（route.path）来判断侧边栏哪个菜单项应该高亮。
 * 比如：当前在 /tasks/123 编辑页面时，侧边栏的 "Tasks" 菜单项就应该高亮。
 */
const activeMenu = computed(() => {
  // 取出当前路径，存到变量 p 里方便使用
  const p = route.path
  // 如果是根路径 /，高亮 Dashboard 菜单
  if (p === '/') return '/'
  // 如果路径以 /tasks 开头（包括 /tasks 和 /tasks/123），高亮 Tasks 菜单
  if (p.startsWith('/tasks')) return '/tasks'
  if (p.startsWith('/groups')) return '/groups'
  if (p.startsWith('/logs')) return '/logs'
  if (p.startsWith('/settings')) return '/settings'
  // 如果都不匹配，默认高亮 Dashboard
  return '/'
})

/**
 * doLogout 函数：处理退出登录操作
 * 1. 清除浏览器本地存储中保存的 token（登录凭证）
 * 2. 跳转到登录页面
 */
function doLogout() {
  // localStorage 是浏览器提供的"本地存储"功能，数据会一直保存在用户电脑上
  // removeItem('token') 删除名为 token 的数据，这样用户就退出了登录状态
  localStorage.removeItem('token')
  // router.push 是路由跳转方法，相当于在浏览器地址栏输入 /login 并回车
  router.push('/login')
}

/**
 * pageTitle 是一个"计算属性"。
 * 它根据当前路径，返回页面顶部标题栏应该显示的文字。
 */
const pageTitle = computed(() => {
  // 根路径显示 Dashboard（仪表盘）
  if (route.path === '/') return 'Dashboard'
  // 任务列表页显示 Task Management（任务管理）
  if (route.path === '/tasks') return 'Task Management'
  // 以 /tasks/ 开头的路径（如 /tasks/1 编辑页面）显示 Task Editor（任务编辑器）
  if (route.path.startsWith('/tasks/')) return 'Task Editor'
  if (route.path.startsWith('/groups/')) return 'Group Editor'
  if (route.path === '/groups') return 'Task Groups'
  // 日志页显示 Execution Logs（执行日志）
  if (route.path === '/logs') return 'Execution Logs'
  // 设置页显示 Settings（设置）
  if (route.path === '/settings') return 'Settings'
  // 默认显示应用名称
  return 'Cronix'
})

/**
 * isLoginPage 是一个"计算属性"。
 * 如果当前路径正好是 /login，返回 true；否则返回 false。
 * 模板里用这个值来决定是显示独立的登录页，还是显示带侧边栏的管理布局。
 */
const isLoginPage = computed(() => route.path === '/login')
</script>

<style>
/*
  全局样式（不带 scoped 属性，会应用到整个网站）

  设置 body 元素的基础样式：
  - margin: 0 去掉浏览器默认的外边距
  - font-family 设置字体优先级：Helvetica Neue（英文字体）> PingFang SC（苹果中文）> Hiragino Sans GB（旧苹果中文）
    > Microsoft YaHei（微软雅黑）> Arial（通用英文字体）> sans-serif（系统默认无衬线字体）
*/
body { margin: 0; font-family: 'Helvetica Neue', Helvetica, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', Arial, sans-serif; }

/*
  页面切换动画的样式定义（配合模板里的 <transition name="fade"> 使用）
  enter-active：新页面进入时的过渡效果 -- 透明度在 0.2 秒内平滑变化
  leave-active：旧页面离开时的过渡效果 -- 同样 0.2 秒
*/
.fade-enter-active, .fade-leave-active { transition: opacity 0.2s ease; }

/*
  enter-from：新页面进入动画开始时的状态 -- 完全透明（看不见）
  leave-to：旧页面离开动画结束时的状态 -- 完全透明（消失）
  效果就是：旧页面淡出，新页面淡入
*/
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
