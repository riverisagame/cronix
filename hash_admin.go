//go:build ignore

// 这个文件只能通过 go run hash_admin.go 单独运行，不会被编译到主程序中
// 用于生成 admin 用户的 bcrypt 密码哈希值
package main
import (
	"fmt"
	"golang.org/x/crypto/bcrypt"
)
func main() {
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	fmt.Println(string(hash))
}
