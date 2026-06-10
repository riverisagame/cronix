// diganose: 诊断 Go exec 在 OpenCloudOS 上的异常行为
// 编译后上传到腾讯云运行：GOOS=linux GOARCH=amd64 go build -o diganose ./cmd/diganose/
package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func main() {
	shells := []string{"/bin/sh", "/usr/bin/sh", "/bin/bash", "/usr/bin/bash"}

	for _, s := range shells {
		// === 测试 A: exec.Command（无 Context、无 SysProcAttr）===
		cmdA := exec.Command(s, "-c", "echo ok")
		outA, errA := cmdA.Output()
		fmt.Printf("[A] exec.Command(%s) → out=%q err=%v\n", s, strings.TrimSpace(string(outA)), errA)

		// === 测试 B: exec.CommandContext（有 Context、SysProcAttr={}）===
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmdB := exec.CommandContext(ctx, s, "-c", "echo ok")
		cmdB.SysProcAttr = &syscall.SysProcAttr{}
		outB, errB := cmdB.Output()
		fmt.Printf("[B] CommandContext+Attr{%s} → out=%q err=%v\n", s, strings.TrimSpace(string(outB)), errB)

		// === 测试 C: exec.CommandContext + Stdin 管道 ===
		ctxC, cancelC := context.WithCancel(context.Background())
		defer cancelC()
		cmdC := exec.CommandContext(ctxC, s)
		cmdC.SysProcAttr = &syscall.SysProcAttr{}
		cmdC.Stdin = strings.NewReader("echo ok")
		outC, errC := cmdC.Output()
		fmt.Printf("[C] CommandContext(%s via stdin) → out=%q err=%v\n", s, strings.TrimSpace(string(outC)), errC)

		// === 测试 D: 完整模拟 ExecuteShell — Stdin + StdoutPipe + StderrPipe ===
		ctxD, cancelD := context.WithCancel(context.Background())
		defer cancelD()
		cmdD := exec.CommandContext(ctxD, s)
		cmdD.SysProcAttr = &syscall.SysProcAttr{}
		cmdD.Stdin = strings.NewReader("echo ok")
		stdout, _ := cmdD.StdoutPipe()
		stderr, _ := cmdD.StderrPipe()
		if err := cmdD.Start(); err != nil {
			fmt.Printf("[D] Start fail(%s) → err=%v\n", s, err)
		} else {
			outBytes := make([]byte, 1024)
			n, _ := stdout.Read(outBytes)
			fmt.Printf("[D] CommandContext+stdin+pipe(%s) → out=%q\n", s, strings.TrimSpace(string(outBytes[:n])))
			cmdD.Wait()
			_ = stderr
		}
	}
}
