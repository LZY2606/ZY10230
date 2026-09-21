package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"fossilcourt/internal/engine"
	"fossilcourt/internal/store"
	"fossilcourt/internal/web"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5570", "监听地址")
	dbPath := flag.String("db", envOr("FOSSIL_COURT_DB", "fossil-court.db"), "SQLite 数据库路径")
	flag.Parse()

	if dir := filepath.Dir(*dbPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	svc := engine.NewService(db)
	empty, err := svc.IsEmpty()
	if err != nil {
		log.Fatal(err)
	}
	if empty {
		if err := svc.Seed(); err != nil {
			log.Fatalf("写入 fixture 失败: %v", err)
		}
		log.Printf("已从固定 fixture 初始化数据库: %s", *dbPath)
	}
	srv := web.NewServer(db, svc)
	fmt.Printf("化石带议庭已启动: http://%s （数据库 %s）\n", *listen, *dbPath)
	if err := http.ListenAndServe(*listen, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
