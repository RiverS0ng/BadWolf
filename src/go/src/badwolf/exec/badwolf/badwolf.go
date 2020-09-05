package main

//import "log"

import (
	"os"
	"fmt"
	"flag"
	"path/filepath"
	"sync"
	"context"
)

import (
//	"badwolf/logger"
	"badwolf/router"
	"badwolf/timevortex"
)

const (
	ROUTER_ID uint8 = 1
)

var (
	SocketPath     string
	TimeVortexPath string
)

func badwolf() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tv, err := timevortex.OpenTimeVortex(ctx, TimeVortexPath)
	if err != nil {
		return err
	}
	defer tv.Close()

	ctx_rt, _ := context.WithCancel(ctx)
	rt, err := router.NewRouter(ctx_rt, ROUTER_ID, SocketPath)
	if err != nil {
		return err
	}
	defer rt.Close()

	wg := new(sync.WaitGroup)
	defer wg.Wait()

	ctx_r, _ := context.WithCancel(ctx)
	if err := run_recver(wg, ctx_r, rt, tv); err != nil {
		return err
	}
	ctx_p, _ := context.WithCancel(ctx)
	if err := run_publisher(wg, ctx_p, tv); err != nil {
		return err
	}
	return nil
}

func run_recver(wg *sync.WaitGroup, ctx context.Context, rt *router.Router, tv *timevortex.TimeVortex) error {
	return nil
}

func run_publisher(wg *sync.WaitGroup, ctx context.Context, tv *timevortex.TimeVortex) error {
	return nil
}

func die(s string, msg ...interface{}) {
	fmt.Fprintf(os.Stderr, s + "\n" , msg...)
	os.Exit(1)
}

func init() {
	var s_path string
	var t_path string
	flag.StringVar(&s_path, "s", "", ".sock file path")
	flag.StringVar(&t_path, "t", "", "TimeVortex path")
	flag.Parse()

	if s_path == "" {
		die("empty socket path.\nUsage : badwolf -s <socket file path> -t <TimeVortex path>")
	}
	if t_path == "" {
		die("empty TimeVortex path.\nUsage : badwolf -s <socket file path> -t <TimeVortex path>")
	}
	SocketPath = filepath.Clean(s_path)
	TimeVortexPath = filepath.Clean(t_path)
}

func main() {
	if err := badwolf(); err != nil {
		die("%s", err)
	}
}
