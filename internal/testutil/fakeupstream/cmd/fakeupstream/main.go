// Command fakeupstream 以独立进程运行内置假上游，供 verify/*.sh 使用。
//
// 用法示例：
//
//	fakeupstream -addr 127.0.0.1:6801 -require-key sk-good -log /tmp/up.log
//
// 与进程内版本（internal/testutil/fakeupstream.New）共用同一份实现。
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/zzyyyds88/PowerBarRations/internal/testutil/fakeupstream"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:6801", "监听地址")
	requireKey := flag.String("require-key", "", "要求的下游 key；空表示不校验")
	models := flag.String("models", "", "只接受这些模型名（逗号分隔）；空表示全部接受")
	logPath := flag.String("log", "", "请求记录输出文件；空表示写 stderr")
	flag.Parse()

	cfg := fakeupstream.Config{RequireKey: *requireKey}
	if strings.TrimSpace(*models) != "" {
		for _, m := range strings.Split(*models, ",") {
			if m = strings.TrimSpace(m); m != "" {
				cfg.Models = append(cfg.Models, m)
			}
		}
	}
	if *logPath != "" {
		f, err := os.Create(*logPath)
		if err != nil {
			log.Fatalf("open log file: %v", err)
		}
		defer f.Close()
		cfg.LogWriter = f
	} else {
		cfg.LogWriter = os.Stderr
	}

	server := &http.Server{Addr: *addr, Handler: fakeupstream.Handler(cfg)}
	log.Printf("[fake-upstream] listening on %s (require-key=%t)", *addr, *requireKey != "")
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("fake upstream stopped: %v", err)
	}
}
