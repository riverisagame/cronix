/**
 * main.ts -- 整个 Vue 项目（网页应用）的"启动文件"。
 * 你可以把它理解成：程序入口，就像一本书的封面和目录，告诉电脑"从哪开始运行"。
 */

// 从 Vue 框架里取出 createApp 这个工具（一个函数，用来创建 Vue 应用实例）
import { createApp } from 'vue'

// ElementPlus 是一套现成的"网页组件库"（按钮、表格、输入框等，不用自己从头画）
import ElementPlus from 'element-plus'

// 引入 ElementPlus 组件库配套的 CSS 样式文件（没有这个，按钮表格都会很难看）
import 'element-plus/dist/index.css'

// 引入 ElementPlus 的"暗黑模式"样式文件（深色背景主题，夜里看着不刺眼）
import 'element-plus/theme-chalk/dark/css-vars.css'

// 用 * as 语法，把 ElementPlus 图标库里的所有图标一次性全部引入，存到 ElementPlusIconsVue 变量里
// 这样后面就能用 <el-icon><Odometer /></el-icon> 这样的写法来显示图标
import * as ElementPlusIconsVue from '@element-plus/icons-vue'

// 引入我们自己写的根组件 App.vue（整个页面的"骨架"组件）
import App from './App.vue'

// 引入路由配置（路由 = 根据网址决定显示哪个页面，比如 /login 显示登录页，/tasks 显示任务列表页）
import router from './router'

// 调用 createApp 函数，把 App 组件传进去，创建出一个 Vue 应用实例（相当于"app 是一个正在运行的程序"）
const app = createApp(App)

// 注册 ElementPlus 图标库中的所有图标为全局组件
// Object.entries() 把图标对象拆成 [键名, 组件] 这样的一对一对数据
// 然后用 for...of 循环，把每个图标都注册到 app 上，这样整个项目的任何地方都可以直接使用这些图标
// key 是图标的名字（比如 'Odometer'），component 是图标的实际渲染对象
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  // app.component(名字, 组件) 的意思是：全局注册一个组件，之后在模板里写 <名字 /> 就能用了
  app.component(key, component)
}

// 告诉 Vue 应用："我要使用 ElementPlus 组件库"（加载 ElementPlus 提供的所有按钮、表格等组件）
app.use(ElementPlus)

// 告诉 Vue 应用："我要使用路由功能"（让浏览器地址栏变化时，页面内容也跟着切换）
app.use(router)

// 把 Vue 应用"挂载"到 HTML 文件中 id="app" 的那个 div 元素上
// 挂载之后，页面上就能看到 Vue 渲染出来的内容了
// index.html 里有一个 <div id="app"></div>，Vue 会把内容填进去
app.mount('#app')
