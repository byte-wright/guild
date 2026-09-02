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

terminal ui
===========

`ContinuousUI` runs the build like `Continuous`, but draws a terminal ui instead of
printing a stream of prefixed lines.

    if ui {
    	err := b.ContinuousUI()
    	if err != nil {
    		log.Fatal(err)
    	}

    	return
    }

    b.Continuous()

Every named output gets a button in the column on the right, in its own color, that
shows or hides its lines. Hidden lines are still collected, so showing an output
again reveals what it said while it was hidden rather than only what comes next.
Guild's own messages are an output like any other and can be filtered the same way.

Holding ctrl over a button shows that output alone for as long as ctrl is held,
which is a way to look at one service without losing the selection you had. A
ctrl+click makes it stick: the clicked output stays the only shown one.

The log follows the newest line until you scroll away from it, and follows again
once you return to the bottom. Scroll with the wheel, by dragging the scroll bar,
or with PgUp/PgDn/Home/End and the arrow keys. `f` toggles following, Esc or Ctrl-C
quits.

Outputs do not have to be registered by hand. `On` walks the matcher chain it is
given and picks up every `NewANSIOut` in it, including the ones wrapped in a
`Debounce`, and `Container.Out` registers itself.

While the ui runs it owns the terminal, so stdout and stderr are redirected to
`.guild/stdout.log` and `.guild/stderr.log`. Anything a matcher prints goes through
a Context and lands in the ui, so those two only collect what escapes it, like a
stray `fmt.Println` or a panic trace. Check them after a crash.

Containers are started in the background in this mode, so the ui is up while they
come up and a container that fails to start leaves a readable error behind instead
of ending the run.

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
    
    var (
    	once = false
    	ui   = false
    )
    
    func main() {
    	flag.BoolVar(&once, "once", false, "execute build once and exit")
    	flag.BoolVar(&ui, "ui", false, "run with the terminal ui")
    
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
    
    	if ui { // draw a terminal ui instead of printing lines
    		err := b.ContinuousUI()
    		if err != nil {
    			log.Fatal(err)
    		}
    
    		return
    	}
    
    	b.Continuous()
    }

