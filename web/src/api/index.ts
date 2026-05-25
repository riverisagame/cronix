/**
 * api/index.ts -- 后端 API（接口）调用函数的集合文件。
 *
 * 什么是 API？
 *   API 可以理解为"前后端之间的通信约定"。
 *   前端（网页）通过 API 向后端（服务器）发送请求，获取数据或执行操作。
 *   就像去餐厅点菜：前端是客人，后端是厨房，API 就是菜单。
 *
 * 本文件的每一个函数都对应后端的一个 API 接口。
 */

// 导入我们在 request.ts 里创建好的 axios 实例（配置好了基础地址、token 等）
import api from './request'

/**
 * authAPI：认证（登录/注册）相关的 API 调用函数。
 * 这里只有一个 login 函数，用来发送登录请求。
 */
export const authAPI = {
  /**
   * 登录函数。
   * @param username 用户名（字符串类型）
   * @param password 密码（字符串类型）
   * 调用后端 POST /login 接口，把用户名和密码发送过去
   * 后端会返回一个 token（相当于"入场券"），前端拿到后存到浏览器本地存储里
   */
  login(username: string, password: string) {
    // api.post() 发送 POST 请求（POST 通常用于提交数据、登录等操作）
    // 第二个参数 { username, password } 是发送给后端的数据（请求体）
    return api.post('/login', { username, password })
  }
}

/**
 * taskAPI：任务管理相关的 API 调用函数。
 * 包含对任务的增删改查（CRUD）等所有操作。
 */
export const taskAPI = {
  /**
   * 获取任务列表（分页查询）。
   * @param params 查询参数对象（如 { page: 1, page_size: 20 }），类型为 any（任意类型）
   * api.get() 发送 GET 请求（GET 通常用于获取/查询数据）
   * { params } 把参数拼接到网址后面，比如 /tasks?page=1&page_size=20
   */
  list(params: any) { return api.get('/tasks', { params }) },

  /**
   * 创建新任务。
   * @param data 新任务的数据对象（包含名称、Cron 表达式等）
   * api.post() 发送 POST 请求到 /tasks，把任务数据放在请求体里发给后端
   */
  create(data: any) { return api.post('/tasks', data) },

  /**
   * 获取单个任务的详细信息。
   * @param id 任务的数字 ID
   * 把 id 拼接到路径里：比如 id=5 时，请求 /tasks/5
   */
  get(id: number) { return api.get('/tasks/' + id) },

  /**
   * 更新（修改）一个已有任务。
   * @param id 要修改的任务 ID
   * @param data 要修改的字段数据
   * api.put() 发送 PUT 请求（PUT 通常用于更新已有数据）
   */
  update(id: number, data: any) { return api.put('/tasks/' + id, data) },

  /**
   * 删除一个任务。
   * @param id 要删除的任务 ID
   * api.delete() 发送 DELETE 请求（DELETE 用于删除数据）
   */
  delete(id: number) { return api.delete('/tasks/' + id) },

  /**
   * 手动触发执行一个任务。
   * @param id 要执行的任务 ID
   * 请求 POST /tasks/5/run，后端收到后立即执行该任务
   */
  run(id: number) { return api.post('/tasks/' + id + '/run') },

  /**
   * 获取某个任务的执行日志列表。
   * @param id 任务 ID
   * @param params 分页参数
   */
  getLogs(id: number, params: any) { return api.get('/tasks/' + id + '/logs', { params }) },

  /**
   * 获取某个任务的依赖任务列表（DAG 依赖关系）。
   * @param id 任务 ID
   */
  getDeps(id: number) { return api.get('/tasks/' + id + '/deps') },

  /**
   * 更新某个任务的依赖关系。
   * @param id 任务 ID
   * @param depIds 依赖任务的 ID 数组，如 [2, 3, 5]
   * 注意：后端接收的字段名是 dep_ids（下划线命名），不是 depIds（驼峰命名）
   */
  updateDeps(id: number, depIds: number[]) { return api.put('/tasks/' + id + '/deps', { dep_ids: depIds }) }
}

/**
 * logAPI：执行日志相关的 API 调用函数。
 */
export const logAPI = {
  list(params: any) { return api.get('/logs', { params }) },
  // 清空所有日志
  clearAll() { return api.delete('/logs') },
  // 清空指定任务的日志
  clearTask(id: number) { return api.delete('/tasks/' + id + '/logs') },
}

/**
 * dashboardAPI：仪表盘（首页统计面板）相关的 API 调用函数。
 */
export const dashboardAPI = {
  /**
   * 获取仪表盘的统计数据（任务总数、成功率、今日运行次数等）。
   * 请求 GET /dashboard/stats
   */
  stats() { return api.get('/dashboard/stats') }
}

/**
 * settingsAPI：系统设置相关的 API 调用函数。
 */
export const settingsAPI = {
  /**
   * 获取当前的系统设置值。
   */
  get() { return api.get('/settings') },

  /**
   * 更新（保存）系统设置。
   * @param data 新的设置数据对象
   */
  update(data: any) { return api.put('/settings', data) }
}

/**
 * groupAPI：任务组管理相关的 API 调用函数。
 */
export const groupAPI = {
  list() { return api.get('/groups') },
  create(data: any) { return api.post('/groups', data) },
  get(id: number) { return api.get('/groups/' + id) },
  update(id: number, data: any) { return api.put('/groups/' + id, data) },
  delete(id: number) { return api.delete('/groups/' + id) },
  setMembers(id: number, taskIDs: number[]) { return api.put('/groups/' + id + '/members', { task_ids: taskIDs }) },
  run(id: number) { return api.post('/groups/' + id + '/run') },
  getLogs(id: number, params?: any) { return api.get('/groups/' + id + '/logs', { params }) },
}
