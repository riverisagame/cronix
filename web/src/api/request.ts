/**
 * api/request.ts -- Axios 实例配置文件。
 *
 * 什么是 Axios？
 *   Axios 是一个 JavaScript 库，专门用来发送 HTTP 请求（和服务器通信）。
 *   可以理解成：它就是一个"快递员"，帮前端把数据送到后端，再把后端的回复带回来。
 *
 * 本文件做了三件事：
 *   1. 创建一个配置好的 axios 实例（设置基础地址和超时时间）
 *   2. 添加"请求拦截器"：每次发请求前自动带上 token（登录凭证）
 *   3. 添加"响应拦截器"：收到回复后，如果发现 token 过期了，自动跳回登录页
 */

// 从 axios 包中导入 axios 对象（默认导出）
import axios from 'axios'

/**
 * 创建 axios 实例。
 *
 * axios.create() 相当于"定制一个专属快递员"，
 * 给它配好默认的收货地址（baseURL）和等待时限（timeout）。
 * 之后所有请求都通过这个实例发出，不用每次都重复配置。
 */
const api = axios.create({
  /**
   * baseURL：基础路径。
   * 所有请求的网址都会自动在前面加上 /api。
   * 比如调用 api.get('/tasks')，实际请求的是 /api/tasks
   * 这样写的好处是：如果后端地址变了，只改这一个地方就行。
   */
  baseURL: '/api',

  /**
   * timeout：超时时间（单位：毫秒）。
   * 15000 毫秒 = 15 秒。
   * 如果 15 秒内后端还没回复，就当作请求失败处理。
   * 防止用户一直无限等待。
   */
  timeout: 15000
})

/**
 * 请求拦截器（Request Interceptor）
 *
 * 什么是拦截器？
 *   可以理解成"关卡检查站"。
 *   请求拦截器在每次请求发出之前自动执行，可以修改请求内容。
 *   响应拦截器在每次收到回复后自动执行，可以统一处理错误。
 *
 * api.interceptors.request.use() 注册一个请求拦截器。
 * 参数是一个函数，这个函数接收请求配置 config，返回修改后的 config。
 */
api.interceptors.request.use(config => {
  /**
   * 从浏览器本地存储中读取 token（登录成功后保存的凭证）。
   * localStorage 是浏览器提供的"本地数据库"，数据存在用户电脑上，不会因为关掉网页而消失。
   * getItem('token') 读取名为 token 的数据，如果没有则返回 null。
   */
  const token = localStorage.getItem('token')

  // 如果 token 存在（用户已经登录）
  if (token) {
    /**
     * 在 HTTP 请求头（Headers）中添加 Authorization 字段。
     *
     * 什么是 Authorization？
     *   请求头是附带在请求里的"附加信息"，就像快递单上的备注。
     *   Authorization 字段专门用来传递身份认证信息。
     *   Bearer 是一种认证方式，后面跟 token，表示"持有这个令牌的人就是合法用户"。
     *
     * 后端收到请求后，会检查 Authorization 头里的 token 是否有效。
     */
    config.headers.Authorization = `Bearer ${token}`
  }

  // 必须返回 config，否则请求发不出去
  return config
})

/**
 * 响应拦截器（Response Interceptor）
 *
 * api.interceptors.response.use() 注册一个响应拦截器。
 * 它接收两个函数参数：
 *   第一个：处理成功响应（状态码 2xx）
 *   第二个：处理失败响应（状态码不是 2xx，如 401、500 等）
 */
api.interceptors.response.use(
  /**
   * 成功响应的处理函数：
   * 如果后端正常返回数据，直接把响应对象原样返回给调用方。
   * 不需要额外处理。
   */
  response => response,

  /**
   * 失败响应的处理函数：
   * 如果请求出错了，统一在这里处理。
   */
  error => {
    /**
     * 检查错误的状态码是否是 401。
     *
     * 401 是什么意思？
     *   HTTP 状态码 401 表示"未授权"（Unauthorized）。
     *   通常意味着 token 过期了或者无效了（用户长时间没操作，登录状态失效）。
     *
     * error.response?.status 中的 ?. 是"可选链"语法：
     *   如果 error.response 不存在（比如网络断开连不上服务器），
     *   后面的 .status 就不会执行，直接返回 undefined，避免程序崩溃。
     */
    if (error.response?.status === 401) {
      /**
       * token 过期了，做两件事：
       * 1. 清除本地存储中过期的 token
       * 2. 把页面重定向到登录页（让用户重新登录）
       */
      localStorage.removeItem('token')
      // window.location.href 直接修改浏览器地址，强制跳转到登录页
      window.location.href = '/login'
    }

    /**
     * Promise.reject(error) 把错误继续往外抛。
     *
     * 什么是 Promise？
     *   Promise 是 JavaScript 处理异步操作的一种方式。
     *   "异步操作"就是不会立刻完成的操作，比如网络请求（需要等服务器回复）。
     *   Promise.reject() 表示"这个操作失败了"，把错误信息传递给调用方。
     *
     * 这样调用方（比如 Login.vue 里的 catch）就能捕获到这个错误并显示提示信息。
     */
    return Promise.reject(error)
  }
)

// 把配置好的 axios 实例导出，让 api/index.ts 等其他文件可以使用
export default api
