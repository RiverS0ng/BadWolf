package main

import (
	"os"
	"os/signal"
	"fmt"
	"flag"
	"path/filepath"
	"sync"
	"context"
	"net/http"
)

import (
	"badwolf/logger"
	"badwolf/router"
	"badwolf/timevortex"
)

const (
	ROUTER_ID uint8 = 1
)

var (
	SocketPath     string
	TimeVortexPath string
	ListenPort     string
)

func badwolf() error {
	logger.PrintMsg("badwolf start")
	defer logger.PrintMsg("badwolf Exit")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalInterruptHandler(cancel)

	logger.PrintMsg("OpenTimeVortex, %s", TimeVortexPath)
	tv, err := timevortex.OpenTimeVortex(newChildContext(ctx), TimeVortexPath)
	if err != nil {
		return err
	}
	defer logger.PrintMsg("ClosedTimeVortex, %s", TimeVortexPath)
	defer tv.Close()

	logger.PrintMsg("Start Router, %s", SocketPath)
	rt, err := router.NewRouter(newChildContext(ctx), ROUTER_ID, SocketPath)
	if err != nil {
		return err
	}
	defer logger.PrintMsg("Closed Router")
	defer rt.Close()

	wg := new(sync.WaitGroup)
	defer wg.Wait()

	logger.PrintMsg("start the recver")
	if err := run_recver(wg, newChildContext(ctx), rt, tv); err != nil {
		return err
	}
	logger.PrintMsg("start the publisher")
	if err := run_publisher(wg, newChildContext(ctx), tv); err != nil {
		return err
	}
	return nil
}

func signalInterruptHandler(f func()) {
	go func() {
		s := make(chan os.Signal)
		signal.Notify(s, os.Interrupt)
		<- s
		f()
	}()
}

func run_recver(wg *sync.WaitGroup, ctx context.Context, rt *router.Router, tv *timevortex.TimeVortex) error {
	//ev_ch_n := make(chan *timevortex.Event) //TODO: create noticer
	ev_ch_a := make(chan *timevortex.Event)

	if err := run_analyzer(wg, newChildContext(ctx), ev_ch_a); err != nil {
		return err
	}
	/* TODO: create noticer
	if err := run_noticer(wg, newChildContext(ctx), ev_ch_n); err != nil {
		return err
	}
	*/

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(ev_ch_a)
		//defer close(ev_ch_n)

		for {
			select {
			case <- ctx.Done():
				logger.PrintMsg("run_recver: canceled ctx")
				return
			case f, ok := <- rt.Recv():
				if !ok {
					logger.PrintMsg("run_recver: closed f_ch")
					return
				}

				news, err := timevortex.Bytes2News(f.Body())
				if err != nil {
					logger.PrintErr("run_recver: cant convert frame to news : %s", err)
					continue
				}

				evt, err := tv.AddNewEvent(news.Source, news)
				if err != nil {
					logger.PrintErr("run_recver: failed add event : %s", err)
					continue
				}

				go func() {
					select {
					case <- ctx.Done():
						return
					case ev_ch_a <- evt:
						return
					}
				}()
				/*//TODO: create noticer
				go func() {
					select {
					case ctx.Done():
						return
					case ev_ch_n <- evt:
						return
					}
				}()
				*/
			}
		}
	}()

	return nil
}

func run_analyzer(wg *sync.WaitGroup, ctx context.Context, evt_ch chan *timevortex.Event) error {
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <- ctx.Done():
				return
			case _, ok := <- evt_ch: //TODO: create analyzer
				if !ok {
					logger.PrintMsg("run_analyzer: closed evt_ch")
					return
				}
			}
		}
	}()
	return nil
}

/*TODO: create noticer
func run_noticer(wg *sync.WaitGroup, ctx context.Context, evt_ch chan *timevortex.Event) error {
	wg.Add(1)
	defer wg.Done()
	return nil
}
*/

func run_publisher(wg *sync.WaitGroup, ctx context.Context, tv *timevortex.TimeVortex) error {
	sv := &http.Server{
		Addr: "127.0.0.1:" + ListenPort,
	}
	http.HandleFunc("/", request_handler)

	wg.Add(1)
	go func() {
		defer wg.Done()

		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := sv.ListenAndServe(); err != nil {
				if err != http.ErrServerClosed {
					logger.PrintErr("run_publisher: httpListenAndServe: %s", err)
				}
				logger.PrintMsg("run_publisher: httpListenAndServe: ServerClose")
				return
			}
		}()

		go func() {
			<- ctx.Done()
			if err := sv.Shutdown(ctx); err != nil {
				logger.PrintErr("run_publisher: httpListenAndServe: %s", err)
				return
			}
		}()
	}()

	return nil
}

func request_handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "hello!\n")
}

func die(s string, msg ...interface{}) {
	logger.PrintErr(s, msg...)
	os.Exit(1)
}

func newChildContext(ctx context.Context) context.Context {
	c_ctx, _ := context.WithCancel(ctx)
	return c_ctx
}

func init() {
	var s_path string
	var t_path string
	var listen_port int
	flag.StringVar(&s_path, "s", "", ".sock file path.")
	flag.StringVar(&t_path, "t", "", "TimeVortex path.")
	flag.IntVar(&listen_port, "p", 3000, "listen port.")
	flag.Parse()

	if s_path == "" {
		die("empty socket path.\nUsage : badwolf -s <socket file path> -t <TimeVortex path>")
	}
	if t_path == "" {
		die("empty TimeVortex path.\nUsage : badwolf -s <socket file path> -t <TimeVortex path>")
	}
	if 0 >= listen_port || listen_port > 65535 {
		die("port number out of range.")
	}

	SocketPath = filepath.Clean(s_path)
	TimeVortexPath = filepath.Clean(t_path)
	ListenPort = fmt.Sprintf("%v", listen_port)
}

func main() {
	if err := badwolf(); err != nil {
		die("%s", err)
	}
}
