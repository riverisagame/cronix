// ============================================================
// internal/model/task_dep.go - 任务依赖关系数据模型
//
// 这个文件定义了"任务之间的依赖关系"在数据库里的样子。
//
// 什么是任务依赖？
//   假设你有两个任务：任务A（每天凌晨备份数据库）和
//   任务B（备份完成后压缩文件）。
//   任务B 依赖 任务A，意思是 A 必须先成功跑完，B 才能开始跑。
//   如果 A 失败了，B 就不应该跑。
//
//   这个依赖关系就像"串联开关"：
//   A 成功 -> B 可以跑
//   A 失败 -> B 不准跑
//
// 这张表是一个"中间表"（多对多关系的桥梁表）
// 一个任务可以依赖多个任务，也可以被多个任务依赖
// 就像一个人可以有多个朋友，也可以被多个人当作朋友
// ============================================================
package model

// TaskDep 代表一个"谁依赖谁"的关系
// 每一行记录就是一个依赖箭头：TaskID --> DependsOnID
// 读法是"TaskID 这道题，必须要等 DependsOnID 做完才能开始"
//
// 注意这个结构体没有导入 time 包
// 因为它不需要时间字段——依赖关系要么存在要么不存在，很简单
//
// 字段说明：
//   gorm:"primaryKey" = 主键
//   gorm:"not null"   = 不能为空
//   gorm:"index"      = 建索引，加快查询速度
//   json:"..."        = 转 JSON 时的字段名
type TaskDep struct {
    // ID 自增主键，唯一标识这条依赖记录
    // 删除依赖关系的时候就是按这个 ID 来删
    ID uint `gorm:"primaryKey" json:"id"`

    // TaskID 是"等着别人先完成"的那个任务的 ID
    // 可以理解为"学生"——必须等"老师"讲完课才能做作业
    // 比如作业任务（TaskID=2）依赖备份任务（DependsOnID=1）
    // not null 表示这个字段必填（不能有没人认领的依赖）
    // index 表示按 TaskID 建了索引
    //   方便查"这个任务依赖哪些其他任务"
    TaskID uint `gorm:"not null;index" json:"task_id"`

    // DependsOnID 是"被等待"的那个任务的 ID
    // 可以理解为"老师"——讲完了课学生才能做作业
    // 比如作业任务（TaskID=2）依赖备份任务（DependsOnID=1）
    // index 表示按 DependsOnID 建了索引
    //   方便查"有哪些任务依赖这个任务"
    DependsOnID uint `gorm:"not null;index" json:"depends_on_id"`

    // 举个完整例子：
    //   备份任务 ID=1（每天 3:00 备份数据库）
    //   压缩任务 ID=2（每天 3:30 压缩备份文件）
    //   清理任务 ID=3（每天 4:00 删除旧备份）
    //
    // 依赖关系：
    //   {TaskID: 2, DependsOnID: 1}  -> 压缩任务等备份任务
    //   {TaskID: 3, DependsOnID: 2}  -> 清理任务等压缩任务
    //
    // 执行顺序：
    //   3:00 备份 -> 备份完成 -> 3:30 压缩 -> 压缩完成 -> 4:00 清理
    //   （如果备份失败，压缩就不会跑，清理也不会跑）
}

// TableName 显式指定数据库表名为 "task_deps"
// deps 是 dependencies（依赖关系）的缩写
func (TaskDep) TableName() string {
    return "task_deps"
}
