package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/CatMsg/NovaPanel/app"
	"github.com/CatMsg/NovaPanel/cmd"
)

func runApp() {
	app := app.NewApp()

	err := app.Init()
	if err != nil {
		log.Fatal(err)
	}

	err = app.Start()
	if err != nil {
		log.Fatal(err)
	}

	sigCh := make(chan os.Signal, 1)
	// Trap shutdown signals
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(sigCh)
	for {
		sig := <-sigCh

		switch sig {
		case syscall.SIGHUP:
			if err := app.RestartApp(); err != nil {
				log.Printf("restart app failed: %v", err)
			}
		default:
			app.Stop()
			return
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		runApp()
		return
	} else {
		cmd.ParseCmd()
	}
}
