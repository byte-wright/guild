guild
=====

is a very simple build tool. Implemented in go.
Can build in watch mode.

It's written in go, that allows to use the versioning of the go.mod file
and no additional tools have to be installed when used from a go project.

Listens for file changes, when a chnage happen any action can be triggered.
Useful for code generation, preparing of assets (thumbnails) or building of files.
Can also be used to (re)start apps/servers.

It can also run the docker containers a project needs during development,
like databases, and hand their address to the services and tools that use them.

containers
==========

Containers are started when guild starts and removed when it stops. They are always
created fresh and never carry named volumes, so every run begins from clean state.
Leftovers from a run that was killed hard are reaped on the next start.

Guild binds a loopback port for every exposed container port while the build is
wired up, and forwards it to the port docker picked. That means:

- the address is known before the container exists, so it can be put into config
  and env vars as a plain string
- nothing is published on anything but 127.0.0.1
- several projects can run at the same time without colliding on ports
- connections arriving before the container is ready are held open until it is,
  instead of being refused, so services and tools need no connect retry logic

Values passed to `Publish` are written to `.guild/env.sh` once all containers are up,
and removed again on shutdown. Tools running outside of guild, like a seeder, can
source it. Add `.guild/` to your `.gitignore`.

    source .guild/env.sh && ./seed --profile=minimal

The values are exported and single quoted, so they survive sourcing unchanged and
are passed on to child processes.

    db := b.Container("postgres:17").
    	Port(5432).
    	Env("POSTGRES_PASSWORD", "dev").
    	Out("postgres", 12, 0, 200, 200)

    dbURL := fmt.Sprintf("postgres://postgres:dev@127.0.0.1:%v/postgres?sslmode=disable",
    	db.HostPort(5432))

    b.Publish("DATABASE_URL", dbURL)

example
=======

package main

    import (
    	"flag"
    	"fmt"
    	"log"
    	"time"
    
    	"github.com/byte-wright/guild"
    )
    
    var once = false
    
    func main() {
    	flag.BoolVar(&once, "once", false, "execute build once and exit")
    
    	flag.Parse()

        // . is root folder, ignore unwanted files
    	b, err := guild.New(".", []string{".git", "web/node_modules"})
    	if err != nil {
    		log.Fatal(err)
    	}

    	db := b.Container("postgres:17"). // started with guild, removed on exit
    		Port(5432).
    		Env("POSTGRES_PASSWORD", "dev").
    		Out("postgres", 12, 0, 200, 200)

    	dbURL := fmt.Sprintf("postgres://postgres:dev@127.0.0.1:%v/postgres?sslmode=disable",
    		db.HostPort(5432))

    	b.Publish("DATABASE_URL", dbURL) // written to .guild/env.sh for external tools
    
    	b.On("\\.(png|jpeg)$", // regexp to match images
    		guild.NewANSIOut("thumbnailer", 12, 255, 128, 0, // print output under given prefix and color
    			guild.Func(func(c guild.Context) {
    				c.Println("make thumbnail...")
    			})))
    
    	b.On("^web/locales/.*\\.yaml",
    		guild.Debounce(time.Millisecond*100, // throttle calls for deduplication
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
    				guild.Exec("go", "build", "-buildvcs=false", // call command
    					"-o", "./build/example",
    					"./cmd/example"))))
    
    	b.On("^build/example$",
    		guild.Debounce(time.Millisecond*100,
    			guild.NewANSIOut("backend", 12, 128, 128, 128,
    				guild.Service("./build/example", "-once"). // call a program, restart when triggered
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
    
    	b.Continuous()
    }

