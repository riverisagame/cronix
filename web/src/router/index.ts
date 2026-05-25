/**
 * router/index.ts -- Vue Router（路由）配置文件。
 *
 * 什么是"路由"？
 *   路由就是"根据浏览器地址栏的网址，决定显示哪个页面"的一套规则。
 *   比如：用户访问 /login 显示登录页，访问 /tasks 显示任务列表页。
 *   就像一个导航系统，告诉程序"这个地址对应哪个房间"。
 */

// 从 vue-router 包里引入两个工具函数
// createRouter：用来创建路由实例（整个路由系统的"管家"）
// createWebHistory：让路由使用 HTML5 的 History 模式（网址看起来像正常的 /login、/tasks，没有 # 号）
import { createRouter, createWebHistory } from 'vue-router'

/**
 * 创建路由实例。
 *
 * createRouter 的参数是一个配置对象：
 * - history: 使用 createWebHistory() 创建 HTML5 历史记录模式的路由
 *   （另一种模式是 createWebHashHistory，网址会带 # 号，如 /#/login，不太好看）
 * - routes: 一个数组，定义每条"路径 -> 页面组件"的对应关系
 */
const router = createRouter({
  // 使用 HTML5 历史模式，网址看起来干净（/login 而不是 /#/login）
  history: createWebHistory(),

  /**
   * routes 数组：定义所有页面的路由规则。
   * 每条规则是一个对象，包含：
   *   - path（路径）：浏览器地址栏中的网址路径
   *   - name（名字）：给这个路由起个名字，方便在代码里引用
   *   - component（组件）：这个路径对应要显示的页面文件
   *   - meta（元信息）：附加数据，比如 requiresAuth 表示"需要登录才能访问"
   *
   * import() 是"懒加载"写法：只有当用户真正访问这个页面时，才去下载对应的 .vue 文件。
   * 这样网站首次加载更快，不需要一次性下载所有页面代码。
   */
  routes: [
    {
      // /login 路径对应登录页面
      path: '/login',
      name: 'Login',
      // () => import(...) 是动态导入：打开登录页时才加载 Login.vue 文件
      component: () => import('../views/Login.vue')
      // 注意：登录页没有 requiresAuth，因为未登录的用户也能访问
    },
    {
      // / 路径（网站根地址）对应仪表盘页面
      path: '/',
      name: 'Dashboard',
      component: () => import('../views/Dashboard.vue'),
      // meta.requiresAuth = true 表示：访问这个页面之前，必须先登录（有 token）
      meta: { requiresAuth: true }
    },
    {
      // /tasks 路径对应任务列表页面
      path: '/tasks',
      name: 'TaskList',
      component: () => import('../views/TaskList.vue'),
      meta: { requiresAuth: true }
    },
    {
      // /tasks/:id 是一个"动态路由"路径
      // :id 表示冒号后面是一个变量，可以匹配任意值
      // 比如 /tasks/123 会显示 id=123 的任务编辑页，/tasks/new 会显示新建任务页
      path: '/tasks/:id',
      name: 'TaskEdit',
      component: () => import('../views/TaskEdit.vue'),
      meta: { requiresAuth: true }
    },
    {
      // /logs 路径对应执行日志页面
      path: '/logs',
      name: 'ExecutionLogs',
      component: () => import('../views/ExecutionLogs.vue'),
      meta: { requiresAuth: true }
    },
    {
      // /settings 路径对应设置页面
      path: '/settings',
      name: 'Settings',
      component: () => import('../views/Settings.vue'),
      meta: { requiresAuth: true }
    },
    {
      // /groups 路径对应任务组管理页面
      path: '/groups',
      name: 'GroupList',
      component: () => import('../views/GroupList.vue'),
      meta: { requiresAuth: true }
    },
    {
      path: '/groups/:id',
      name: 'GroupEdit',
      component: () => import('../views/GroupEdit.vue'),
      meta: { requiresAuth: true }
    }
  ]
})

/**
 * router.beforeEach 是一个"全局导航守卫"。
 * 它在每次页面跳转发生之前自动执行，像一个"门卫"，检查用户有没有权限访问目标页面。
 *
 * 参数说明：
 *   - to（去哪里）：目标路由的信息（路径、meta 等）
 *   - _from（从哪里来）：当前路由的信息（下划线开头表示这个参数虽然传进来了但没用到）
 *   - next（下一步）：一个函数，调用它来"放行"或"重定向"
 *     - next() 不传参数：放行，正常跳转
 *     - next('/login')：重定向到登录页
 */
router.beforeEach((to, _from, next) => {
  // 从浏览器的本地存储中读取 token（登录凭证）
  // 如果有 token 说明用户已登录，没有则说明未登录
  const token = localStorage.getItem('token')

  // 情况一：目标页面需要登录（requiresAuth 为 true），但用户没有 token
  if (to.meta.requiresAuth && !token) {
    // 把用户重定向到登录页面，不让访问需要权限的页面
    next('/login')
  }
  // 情况二：用户已经有 token 了，却想去登录页（说明已经登录过了，不用再登录）
  else if (to.path === '/login' && token) {
    // 直接重定向到首页（仪表盘）
    next('/')
  }
  // 情况三：其他所有正常情况，直接放行
  else {
    next()
  }
})

// 把配置好的路由实例导出（export default），让 main.ts 和其他文件可以导入使用
export default router
