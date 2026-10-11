package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"codex-orchestrator/internal/broker"
	"codex-orchestrator/internal/proxy"
	"codex-orchestrator/internal/system"
)

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "proxy":
			return proxy.Run()
		case "broker":
			return broker.Run()
		case "native-policy":
			if len(os.Args) != 3 || os.Args[2] != "install" {
				return fmt.Errorf("usage: orchestrator native-policy install")
			}
			return system.NativeNetworkPolicy(true)
		case "native-check":
			flags := flag.NewFlagSet("native-check", flag.ContinueOnError)
			path := flags.String("config", "config.native.json", "native configuration")
			if e := flags.Parse(os.Args[2:]); e != nil {
				return e
			}
			b, e := os.ReadFile(*path)
			if e != nil {
				return e
			}
			var c system.Config
			if e = json.Unmarshal(b, &c); e != nil {
				return e
			}
			if c.Backend != "native-windows" {
				return fmt.Errorf("native backend required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if e = system.VerifyNativeIsolation(ctx, c); e != nil {
				return e
			}
			fmt.Println("Native isolation checks passed; no model or integration was used.")
			return nil
		}
	}
	config := flag.String("config", "config.json", "configuration file")
	check := flag.Bool("check", false, "validate configuration, Docker and API access without starting agents")
	web := flag.String("web", "127.0.0.1:8787", "local dashboard address; empty disables it")
	flag.Parse()
	c, e := system.LoadConfig(*config)
	if e != nil {
		return e
	}
	ctrl, e := system.NewController(c)
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *check {
		if e = ctrl.Runner.Preflight(ctx); e != nil {
			return e
		}
		if e = ctrl.API.Validate(ctx); e != nil {
			return e
		}
		fmt.Println("Configuration and required access verified; no agents started.")
		return nil
	}
	releaseInstallation, e := system.LockInstallation(*config)
	if e != nil {
		return e
	}
	defer releaseInstallation()
	// Single instance per state directory. An unclean shutdown leaves this lock intentionally
	// so an operator can first stop orphan containers instead of duplicating work.
	lock, e := os.OpenFile(c.StateDir+string(os.PathSeparator)+"controller.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fmt.Errorf("controller lock exists or unavailable; ensure old instance is stopped before removing it")
	}
	fmt.Fprint(lock, os.Getpid())
	lock.Close()
	ctrl.LockPath = lock.Name()
	defer ctrl.ReleaseControllerLock()
	if e := ctrl.StartPanel(ctx, *web, *config); e != nil {
		return e
	}
	return ctrl.Run(ctx)
}
