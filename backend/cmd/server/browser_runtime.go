package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type browserService interface {
	Call(context.Context, string, any) (json.RawMessage, error)
}

type browserReply struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// 浏览器由后端按需启动，通过私有标准输入输出通信，不开放额外服务端口。
type browserRuntime struct {
	mu      sync.Mutex
	command *exec.Cmd
	input   io.WriteCloser
	pending map[uint64]chan browserReply
	nextID  uint64
}

func browserScript() string {
	if path := os.Getenv("AITOK_BROWSER_SCRIPT"); path != "" {
		return path
	}
	for _, path := range []string{"scripts/session-browser.mjs", "../scripts/session-browser.mjs", "/app/scripts/session-browser.mjs"} {
		if _, err := os.Stat(path); err == nil {
			absolute, _ := filepath.Abs(path)
			return absolute
		}
	}
	return ""
}

func (b *browserRuntime) startLocked() error {
	if b.command != nil {
		return nil
	}
	script := browserScript()
	if script == "" {
		return errors.New("服务器缺少浏览器组件，请更新完整应用后重试")
	}
	args := []string{script, "--stdio"}
	if path := os.Getenv("AITOK_BROWSER_CHROME"); path != "" {
		args = append(args, "--chrome", path)
	}
	if path := os.Getenv("AITOK_BROWSER_DIRECTORY"); path != "" {
		args = append(args, "--directory", path)
	}
	command := exec.Command(envDefault("AITOK_BROWSER_NODE", "node"), args...)
	input, err := command.StdinPipe()
	if err != nil {
		return errors.New("无法创建浏览器通信管道")
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		return errors.New("无法创建浏览器通信管道")
	}
	if err := command.Start(); err != nil {
		input.Close()
		output.Close()
		return errors.New("服务器无法启动浏览器组件，请安装 Node.js 22+ 和 Chromium")
	}
	b.command, b.input = command, input
	b.pending = make(map[uint64]chan browserReply)
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 64<<10), 8<<20)
		for scanner.Scan() {
			var reply browserReply
			if json.Unmarshal(scanner.Bytes(), &reply) != nil {
				continue
			}
			b.mu.Lock()
			if pending := b.pending[reply.ID]; pending != nil {
				pending <- reply
				delete(b.pending, reply.ID)
			}
			b.mu.Unlock()
		}
		input.Close()
		_ = command.Wait()
		b.mu.Lock()
		if b.command == command {
			b.command, b.input = nil, nil
			for id, pending := range b.pending {
				pending <- browserReply{Error: "浏览器组件已停止，请检查服务器的 Node.js、Chromium 和环境目录配置后重试"}
				delete(b.pending, id)
			}
		}
		b.mu.Unlock()
	}()
	return nil
}

func (b *browserRuntime) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return nil, errors.New("浏览器请求已取消")
	}
	b.mu.Lock()
	if err := b.startLocked(); err != nil {
		b.mu.Unlock()
		return nil, err
	}
	b.nextID++
	id := b.nextID
	request, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		b.mu.Unlock()
		return nil, errors.New("浏览器请求格式无效")
	}
	pending := make(chan browserReply, 1)
	b.pending[id] = pending
	_, err = b.input.Write(append(request, '\n'))
	if err != nil {
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, errors.New("浏览器服务连接失败，请稍后重试")
	}
	b.mu.Unlock()
	select {
	case reply := <-pending:
		if reply.Error != "" {
			return nil, errors.New(reply.Error)
		}
		return reply.Result, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, errors.New("浏览器响应超时，请刷新状态后重试")
	}
}

func (b *browserRuntime) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.input != nil {
		b.input.Close()
	}
}
