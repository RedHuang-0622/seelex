// Package root 是会话存储的基座机制：数据根独占锁、进程存活探测、无锁共享读、
// 原子发布、JSONL 残尾纪律与存储运行参数。
//
// 生态位与边界见 README.md。本包的依赖闭包是**纯标准库**（无 Seelex 包、无第三方），
// 由 e2e 的 I6 门禁机械断言；上层（store）通过 import 别名 `storeroot` 使用它。
package root
