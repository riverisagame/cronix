// ============================================================
// cmd/passwd.go - 设置或修改管理员密码的子命令
//
// 这个文件实现了 "cronix passwd" 命令。
// 当你第一次使用 Cronix 时，必须先设置一个管理员密码。
// 就像手机第一次开机要设置锁屏密码一样。
//
// 密码的处理流程：
//   1. 用户在命令行输入用户名和密码（密码输入时看不见，防止被偷看）
//   2. 用 bcrypt 算法把密码加密成一串乱码（哈希）
//   3. 把加密后的密码保存到 config.yaml 配置文件里
//
// 为什么要把密码变成乱码再存储？
//   因为如果直接存明文密码，万一配置文件被别人看到了，
//   密码就泄露了。而 bcrypt 加密是单向的，无法从乱码反推密码。
//   验证密码时，是把用户输入的密码再加密一次，比对加密结果是否一致。
// ============================================================
package cmd

import (
    // "bufio" 是带缓冲的输入输出工具
    // 可以一行一行地读用户输入（而不是一个字一个字地读）
    "bufio"

    // "fmt" 是格式化输出工具
    // 用来在屏幕上显示提示文字和结果
    "fmt"

    // "os" 是操作系统相关工具
    // 这里用于读取键盘输入和程序退出
    "os"

    // "strings" 是字符串处理工具
    // 用来去掉用户输入前后的空格和换行符
    "strings"

    // "syscall" 是底层的系统调用
    // 这里用来获取标准输入的文件描述符（一个数字编号）
    // 这是隐藏密码输入所必需的
    "syscall"

    // cobra 是命令行框架（用来构建命令系统）
    "github.com/spf13/cobra"

    // viper 是配置文件管理工具
    // 用来读写 config.yaml 文件
    "github.com/spf13/viper"

    // bcrypt 是一个加密库，专门用来安全地保存密码
    // 它会把密码变成一串无法还原的"乱码"（叫哈希值）
    "golang.org/x/crypto/bcrypt"

    // term 是一个终端操作工具
    // 这里用它的 ReadPassword 函数来读取密码
    // 特点是：用户打字时屏幕上不显示字符，防止别人偷看
    "golang.org/x/term"
)

// passwdConfigPath 存储用户指定的配置文件路径
// 用户可以用 --config 或 -c 参数来指定
// 默认值在 root.go 的 init() 中被设置为 "config.yaml"
var passwdConfigPath string

// passwdCmd 是密码设置子命令的定义
// 它是 cobra.Command 结构体的一个实例
// Use 是命令的名字，Short 是简短说明，Run 是按下这个按钮要执行的事
var passwdCmd = &cobra.Command{
    Use:   "passwd",                          // 用户输入 "cronix passwd" 触发此命令
    Short: "设置或修改网页管理界面的管理员密码", // --help 时显示的简短说明
    Run:   runPasswd,                         // 按下"按钮"后要执行的函数
}

// runPasswd 是 "cronix passwd" 命令的实际处理函数
// 它的工作流程：
//   读用户名 -> 读密码 -> 确认密码 -> 一致性检查 -> 加密 -> 存到配置文件
func runPasswd(cmd *cobra.Command, args []string) {
    // --- 第1步：创建一个"输入阅读器" ---
    // bufio.NewReader 创建一个带缓冲的阅读器，连接键盘输入（os.Stdin）
    // 缓冲的意思是：用户输入的内容先存放在一个临时区域，攒够一行了再处理
    reader := bufio.NewReader(os.Stdin)

    // --- 第2步：让用户输入用户名 ---
    // fmt.Print 在屏幕上显示提示文字（不换行）
    // "[admin]" 在方括号里的是默认值，意思是如果不输直接回车，就用 "admin"
    fmt.Print("请输入用户名 [默认: admin]: ")
    // reader.ReadString('\n') 读取用户输入，直到遇到换行符（回车键）
    // '\n' 代表换行符（你按回车时产生的字符）
    // _ 是一个"垃圾桶变量"，用来接住不需要用的返回值（这里忽略了错误）
    username, _ := reader.ReadString('\n')
    // strings.TrimSpace 把字符串前后多余的空白（空格、换行、制表符）全部去掉
    // 因为 ReadString 会保留用户输入的换行符，需要清理掉
    username = strings.TrimSpace(username)
    // 如果用户直接按了回车（没打字），就用默认值 "admin"
    if username == "" {
        username = "admin"
    }

    // --- 第3步：让用户输入密码（不显示在屏幕上）---
    fmt.Print("请输入密码: ")
    // term.ReadPassword 是一个特殊函数，读取用户的键盘输入
    // 但不在屏幕上显示任何字符（连 *** 都不显示），彻底防止被旁人看到
    // int(syscall.Stdin) 是键盘输入的文件描述符编号
    // 返回值 passwordBytes 是一个字节数组（byte slice），存着用户输入的密码
    passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
    if err != nil {
        // 如果读取出错了，打印错误并退出
        fmt.Fprintf(os.Stderr, "读取密码时出错: %v\n", err)
        os.Exit(1)
    }
    // fmt.Println() 输出一个空行（换行），因为 ReadPassword 不显示换行
    fmt.Println()

    // --- 第4步：让用户再输入一次密码（确认）---
    // 这是为了防止用户第一次输错自己不知道
    // 两次输入必须一模一样才算通过
    fmt.Print("请再次输入密码（确认）: ")
    confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
    if err != nil {
        fmt.Fprintf(os.Stderr, "读取确认密码时出错: %v\n", err)
        os.Exit(1)
    }
    fmt.Println()

    // --- 第5步：检查两次密码是否一致 ---
    // string() 把字节数组转换成字符串（人类可读的文字形式）
    // 然后用 != 比较两次输入是否不同
    if string(passwordBytes) != string(confirmBytes) {
        // 两次输入不一样，提示错误并退出
        fmt.Fprintln(os.Stderr, "错误：两次输入的密码不一致，请重试")
        os.Exit(1)
    }
    // 检查密码是否为空（用户什么都没输入就按了回车）
    if len(passwordBytes) == 0 {
        fmt.Fprintln(os.Stderr, "错误：密码不能为空")
        os.Exit(1)
    }

    // --- 第6步：用 bcrypt 算法加密密码 ---
    // bcrypt.GenerateFromPassword 把原始密码变成一串无法还原的乱码
    // 参数说明：
    //   passwordBytes     = 用户输入的原始密码
    //   bcrypt.DefaultCost = 加密强度（cost 值越高越安全，但越慢；10 是默认值）
    // 返回值 hash 是一串类似 "$2a$10$..." 的乱码，这就是存到文件里的东西
    hash, err := bcrypt.GenerateFromPassword(passwordBytes, bcrypt.DefaultCost)
    if err != nil {
        fmt.Fprintf(os.Stderr, "密码加密时出错: %v\n", err)
        os.Exit(1)
    }

    // --- 第7步：把加密后的密码写入配置文件 ---
    // viper.New() 创建一个新的 viper 实例（配置文件读写工具）
    v := viper.New()
    // 告诉 viper 配置文件在哪里
    v.SetConfigFile(passwdConfigPath)
    // 告诉 viper 配置文件是 YAML 格式（一种人类易读的配置格式）
    v.SetConfigType("yaml")

    // 如果配置文件已经存在，先读取里面的内容
    // os.Stat 检查文件是否存在，返回文件信息和错误
    // err == nil 表示文件存在且能访问
    if _, err := os.Stat(passwdConfigPath); err == nil {
        // v.ReadInConfig() 读取已有配置文件的内容
        if err := v.ReadInConfig(); err != nil {
            // 读取失败只警告，不退出（因为可能文件内容有误，但我们还是要写入新内容）
            fmt.Fprintf(os.Stderr, "警告：无法读取已有配置文件: %v\n", err)
        }
    }

    // 把用户名和加密后的密码设置到配置中
    // v.Set("路径", 值) 像在地图上标记位置一样，在配置树里设定一个值
    // "auth.username" 表示配置里 auth 下面的 username 项
    v.Set("auth.username", username)        // 设置用户名
    v.Set("auth.password", string(hash))    // 设置加密后的密码（hash 是字节数组，要转成字符串）

    // v.WriteConfig() 把内存中的配置写回文件
    // 如果文件已存在，它会覆盖原文件
    // 如果文件不存在，会报错（因为 viper 不知道要创建在哪里）
    if err := v.WriteConfig(); err != nil {
        // 错误可能是"文件不存在"
        if os.IsNotExist(err) {
            // 是文件不存在导致的，用 WriteConfigAs 指定路径新建文件
            if err := v.WriteConfigAs(passwdConfigPath); err != nil {
                fmt.Fprintf(os.Stderr, "写入配置文件时出错: %v\n", err)
                os.Exit(1)
            }
        } else {
            // 其他类型的错误
            fmt.Fprintf(os.Stderr, "写入配置文件时出错: %v\n", err)
            os.Exit(1)
        }
    }

    // --- 第8步：提示成功 ---
    fmt.Printf("密码设置成功！用户名: %s\n", username)
    fmt.Printf("配置已保存到: %s\n", passwdConfigPath)
}
