package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"strings"

	"skill-platform/backend/hooks"
	_ "skill-platform/backend/migrations"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
)

// all: 前缀必须保留。Vite/Rollup 生成的 chunk 文件名可能以 "_" 开头
//（同名去重时加前缀），而 go:embed 默认会排除 "." 和 "_" 开头的文件/目录。
// 少了 all: 就会漏掉 _LjICcBR.js 这类文件 → 服务端对未知路径回退到 index.html
// 并返回 text/html → 浏览器因严格 MIME 校验拒绝执行该 module → 整个页面白屏。
//go:embed all:pb_public/*
var staticFS embed.FS

func main() {
	app := pocketbase.New()

	isGoRun := strings.HasPrefix(os.Args[0], os.TempDir())

	// Docs https://pocketbase.io/docs/go-migrations/
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Automigrate: isGoRun,
	})

	serveStatic(app)
	hooks.Register(app)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func serveStatic(app core.App) {
	// Docs https://pocketbase.io/docs/go-overview/
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		pb_public, err := fs.Sub(staticFS, "pb_public")
		if err != nil {
			return err
		}
		se.Router.GET("/{path...}", apis.Static(pb_public, true))
		return se.Next()
	})
}
