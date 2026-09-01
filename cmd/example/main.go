package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/byte-wright/guild"
)

var (
	once = false
	ui   = false
)

func main() {
	flag.BoolVar(&once, "once", false, "execute build once and exit")
	flag.BoolVar(&ui, "ui", false, "run with the terminal ui")

	flag.Parse()

	b, err := guild.New(".", []string{".git", "web/node_modules"})
	if err != nil {
		log.Fatal(err)
	}

	db := b.Container("postgres:17").
		Port(5432).
		Env("POSTGRES_PASSWORD", "dev").
		Out("postgres", 12, 0, 200, 200)

	dbURL := fmt.Sprintf("postgres://postgres:dev@127.0.0.1:%v/postgres?sslmode=disable",
		db.HostPort(5432))

	b.Publish("DATABASE_URL", dbURL)

	b.On("\\.(png|jpeg)$",
		guild.NewANSIOut("thumbnailer", 12, 255, 128, 0,
			guild.Func(func(c guild.Context) {
				c.Println("make thumbnail...")
			})))

	b.On("^web/locales/.*\\.yaml",
		guild.Debounce(time.Millisecond*100,
			guild.NewANSIOut("locales", 12, 255, 0, 128,
				guild.Func(func(c guild.Context) {
					c.Println("generate locales file")
				}))))

	b.On("Dockerfile$",
		guild.Debounce(time.Millisecond*100,
			guild.NewANSIOut("e2e model", 12, 0, 128, 255,
				guild.Func(func(c guild.Context) {
					c.Println("build dockerfile")
				}))))

	b.On(`\.go$`,
		guild.Debounce(time.Second,
			guild.NewANSIOut("compile", 12, 128, 255, 0,
				guild.Exec("go", "build", "-buildvcs=false",
					"-o", "./build/example",
					"./cmd/example"))))

	b.On("^build/example$",
		guild.Debounce(time.Millisecond*100,
			guild.NewANSIOut("backend", 12, 128, 128, 128,
				guild.Service("./build/example", "-once").
					Env("PORT", "8000").
					Env("DATABASE_URL", dbURL).
					Env("ALLOW_HTTP", "true").
					Env("DEV_PROXY", "http://localhost:3000").
					ForwardEnv("SECRET_PASSWORD"),
			)))

	if once {
		b.Once()
		return
	}

	if ui {
		err := b.ContinuousUI()
		if err != nil {
			log.Fatal(err)
		}

		return
	}

	b.Continuous()
}
