package main

import (
	"os"
	"os/signal"
	"fmt"
	"flag"
	"path/filepath"
	"sync"
	"time"
	"context"
	"net/http"
)

import (
	"github.com/gorilla/feeds"
)

import (
	"badwolf/logger"
	"badwolf/router"
	"badwolf/timevortex"
	"badwolf/packet"
)

var (
	SocketPath     string
	TimeVortexPath string
	ListenPort     string
)

func badwolf() error {
	logger.PrintMsg("starting badwolf....")
	defer logger.PrintMsg("Exit badwolf")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalInterruptHandler(cancel)

	logger.PrintMsg("Open Storage : %s", TimeVortexPath)
	tv, err := timevortex.OpenTimeVortex(newChildContext(ctx), TimeVortexPath)
	if err != nil {
		return err
	}
	defer logger.PrintMsg("Closed Storage.")
	defer tv.Close()
	defer logger.PrintMsg("Closing Storage : %s", TimeVortexPath)

	logger.PrintMsg("Start the router, connect to socket : %s", SocketPath)
	rt, err := router.NewRouter(newChildContext(ctx), SocketPath)
	if err != nil {
		return err
	}
	defer logger.PrintMsg("Closed the router.")
	defer rt.Close()

	wg := new(sync.WaitGroup)
	defer wg.Wait()

	logger.PrintMsg("Start the recver")
	if err := run_recver(wg, newChildContext(ctx), rt, tv); err != nil {
		return err
	}
	logger.PrintMsg("Start the publisher")
	if err := run_publisher(wg, newChildContext(ctx), tv); err != nil {
		return err
	}
	logger.PrintMsg("badwolf running!!")
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

func run_newsRecorder(wg *sync.WaitGroup, ctx context.Context, rt *router.Router, tv *timevortex.TimeVortex, from uint8, body []byte) {
	defer wg.Done()

	news, err := timevortex.Bytes2News(body)
	if err != nil {
		logger.PrintErr("run_newsRecorder: cant convert packet to news : %s", err)
		return
	}

	if err := tv.AddNews(news); err != nil {
		if err != timevortex.ErrAlreadyExist {
			logger.PrintErr("run_newsRecorder: failed add news : %s", err)
			return
		}

		logger.PrintErr("run_newsRecorder: tried already exist data.")
		p_b := packet.CreateBytes(packet.F_R_NEW_NEWS, news.Id())
		if err := rt.Send(from, p_b); err != nil {
			logger.PrintErr("run_newsRecorder: failed reply : %s", err)
			return
		}
		return
	}
	logger.PrintMsg("run_newsRecorder: recorded new news.")

	p_b := packet.CreateBytes(packet.F_R_NEW_NEWS, news.Id())
	if err := rt.Send(from, p_b); err != nil {
		logger.PrintErr("run_newsRecorder: failed reply : %s", err)
		return
	}
}

func run_recver(wg *sync.WaitGroup, ctx context.Context, rt *router.Router, tv *timevortex.TimeVortex) error {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer logger.PrintMsg("run_recver: exiting...")

		for {
			select {
			case <- ctx.Done():
				return
			case f, ok := <- rt.Recv():
				if !ok {
					return
				}

				p, err := packet.Bytes2Packet(f.Body())
				if err != nil {
					logger.PrintErr("run_recver: cant convert frame body to packet : %s", err)
					continue
				}

				from := f.SrcId()
				switch p.Flg() {
					case packet.F_S_NEW_NEWS:
						wg.Add(1)
						go run_newsRecorder(wg, ctx, rt, tv, from, p.Body())
					default:
						logger.PrintErr("run_recver: unkown operation.")
				}
			}
		}
	}()

	return nil
}

func run_publisher(wg *sync.WaitGroup, ctx context.Context, tv *timevortex.TimeVortex) error {
	sv := &http.Server{
		Addr: "127.0.0.1:" + ListenPort,
	}
	http.HandleFunc("/", func (w http.ResponseWriter, r *http.Request) {
		logger.PrintMsg("[HTTP Request] %s, %s, %s", r.RemoteAddr, r.Method, r.URL)
		switch r.Method {
		case "GET" :
			start := r.URL.Query().Get("start")
			end := r.URL.Query().Get("end")
			category := r.URL.Query().Get("category")
			tool := r.URL.Query().Get("tool")

			f, err := GenerateFeed(ctx, tv, start, end, category, tool)
			if err != nil {
				logger.PrintErr("[HTTP Request] GenerateFeed : %s", err)
				http.Error(w, "Failed: generate feed body.", http.StatusInternalServerError)
				return
			}
			rss, err := f.ToRss()
			if err != nil {
				logger.PrintErr("[HTTP Request] GenerateFeed : %s", err)
				http.Error(w, "Failed: generate feed body.", http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, rss + "\n")
		default :
			http.Error(w, "Method not allowed.", http.StatusBadRequest)
		}
	})

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

func GenerateFeed(ctx context.Context, tv *timevortex.TimeVortex,
	start string, end string, category string, tool string) (*feeds.Feed, error) {
	layout := "20060102"

	if start == "" {
		start = time.Now().AddDate(0, 0, -14).Format(layout)
	}
	st, err := time.Parse(layout, start)
	if err != nil {
		return nil, err
	}
	if end == "" {
		end = time.Now().AddDate(0, 0, 1).Format(layout)
	}
	et, err := time.Parse(layout, end)
	if err != nil {
		return nil, err
	}
	var opt *timevortex.Options
	if tool != "" || category != "" {
		opt = timevortex.NewOptions(tool, category)
	}

	news_s, err := tv.Find(ctx, st, et, opt)
	if err != nil {
		return nil, err
	}

	items := []*feeds.Item{}
	for _, news := range news_s {
		item := &feeds.Item{
			Title: news.Title,
			Link:  &feeds.Link{Href: news.Link},
			Description: news.Summary,
			Author: &feeds.Author{Name: news.Recorder},
			Created: news.PubDate,
		}
		items = append(items, item)
	}

	body := &feeds.Feed{
		Title:       "rss feed",
		Link:        &feeds.Link{Href: "http://example.com"},
		Description: "rss feed description",
		Author:      &feeds.Author{Name: "john smith", Email: "donotreply@example.com"},
		Created:     time.Now(),
		Items:       items,
	}
	return body, nil
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
