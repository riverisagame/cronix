/**
 * env.d.ts -- TypeScript 类型声明文件。
 *
 * 这个文件的作用是什么？
 *   TypeScript 是一种"带类型检查的 JavaScript"。它要求每个变量和文件都有明确的"类型"。
 *   但是 .vue 文件不是标准的 JavaScript 文件，TypeScript 不认识它。
 *   当你写 import App from './App.vue' 时，TypeScript 会报错："找不到这个模块的类型！"
 *
 *   这个文件就是来解决这个问题的：它告诉 TypeScript，
 *   "所有以 .vue 结尾的文件，都是 Vue 组件模块，可以放心导入"。
 *
 *   有了这个声明，TypeScript 就不会在 import .vue 文件时报错了。
 */

/**
 * declare module '*.vue' -- 声明一个"模块类型"。
 *
 * 什么是"模块"？
 *   在 TypeScript 中，每个文件都是一个"模块"（module）。
 *   通过 import 和 export 来共享代码。
 *
 * 这里的 '*.vue' 是一个"通配符模块名"：
 *   - 星号 * 表示匹配任意文件名
 *   - .vue 是文件后缀
 *   - 合起来：匹配所有以 .vue 结尾的文件（如 App.vue、Login.vue）
 *
 * 也就是说，下面花括号 {} 里的内容，就是告诉 TypeScript：
 *   "当你在代码里写 import xxx from './某个.vue' 时，这个 xxx 的类型是什么"
 */
declare module '*.vue' {
  /**
   * import type { DefineComponent } from 'vue'
   *
   * import type 是"仅导入类型"的语法（TypeScript 3.8 新增）。
   * 它与普通 import 的区别：
   *   - 普通 import 会把真正的代码也加载进来
   *   - import type 只导入"类型信息"，编译后的 JavaScript 代码里完全不会出现这行
   *   这样做的好处是：不影响最终打包的文件大小。
   *
   * DefineComponent 是 Vue 定义的一个"组件类型"。
   * 它描述了 Vue 组件应该有哪些属性和方法。
   */
  import type { DefineComponent } from 'vue'

  /**
   * const component: DefineComponent<{}, {}, any>
   *
   * 这行代码声明了一个变量 component，它的类型是 DefineComponent。
   * 尖括号 <> 里的三个参数是"泛型参数"（Generic Types），可以理解为"类型的配置项"：
   *
   *   - 第一个 {}：Props 类型（组件接收的属性）。
   *     空对象表示这个组件不接收任何外部传入的属性。
   *
   *   - 第二个 {}：RawBindings 类型（组件内部暴露的数据和方法）。
   *     空对象表示不限制组件内部的数据结构。
   *
   *   - 第三个 any：其他类型。
   *     any 是 TypeScript 的特殊类型，表示"可以是任何类型，不做检查"。
   *     这里用 any 是因为不同 .vue 组件的结构差异很大，无法用一个固定类型来描述。
   *
   *   简单理解：这行告诉 TypeScript "component 是一个 Vue 组件"。
   */
  const component: DefineComponent<{}, {}, any>

  /**
   * export default component
   *
   * 把 component 作为这个模块的"默认导出"。
   * 这样当其他文件写 import App from './App.vue' 时，
   * TypeScript 就知道 App 的类型是 DefineComponent（Vue 组件类型），
   * 不会报"找不到类型"的错误了。
   */
  export default component
}
