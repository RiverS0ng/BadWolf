package main

import (
	"os"
	"os/signal"
	"fmt"
	"flag"
	"time"
	"path/filepath"
	"context"
)

import (
	"github.com/mmcdole/gofeed"
)

import (
	"badwolf/router"
	"badwolf/logger"
	"badwolf/timevortex"
)

const (
	CORE_ROUTER_ID uint8 = 1

	ROUTER_ID uint8 = 129

	USAGE string =  "Usage : bwget_feed [ -rid <router id> ] [ -s <sleep time(second)> ] <socket path> <recorder name> <feed url>"
)

var (
	Recorder string
	SockPath string
	FeedUrl   string

	RouterId  uint8
	SleepTime int
)

func bwget_feed() error {
	logger.PrintMsg("bwget_feed starting...")
	defer logger.PrintMsg("bwget_feed exit")
	logger.PrintMsg("Recorder : %s", Recorder)
	logger.PrintMsg("FeedUrl : %s", FeedUrl)
	logger.PrintMsg("SockPath : %s", SockPath)
	logger.PrintMsg("RouterId : %v", RouterId)
	ctx, cancel := context.WithCancel(context.Background())
	signalInterruptHandler(cancel)
	defer cancel()

	logger.PrintMsg("router connecting... : %s", SockPath)
	rt, err := router.Connect(ctx, RouterId, SockPath)
	if err != nil {
		return err
	}
	defer rt.Close()
	logger.PrintMsg("router connected")

	posted := make(map[string]interface{})
	tc := time.NewTicker(time.Second * time.Duration(SleepTime))
	fp := gofeed.NewParser()
	for {
		select {
		case <- ctx.Done():
			return nil
		case <- tc.C:
			logger.PrintMsg("get feed")
			feed, err := fp.ParseURLWithContext(FeedUrl, ctx)
			if err != nil {
				logger.PrintErr("failed parse feed : %s", err)
				continue
			}
			logger.PrintMsg("got feed item : %v", len(feed.Items))

			now := time.Now()
			for _, item := range feed.Items {
				_, ok := posted[item.GUID + item.Updated]
				if ok {
					continue
				}
				posted[item.GUID + item.Updated] = nil

				var source string
				var pubdate time.Time = now
				if item.Author != nil {
					source = item.Author.Name
				}

				if item.UpdatedParsed != nil {
					pubdate = *item.UpdatedParsed
				}
				if item.UpdatedParsed == nil && item.PublishedParsed != nil {
					pubdate = *item.PublishedParsed
				}

				news := &timevortex.News{
					Title: item.Title,
					Link: item.Link,
					Summary: item.Description,
					PubDate: pubdate,
					Source: source,
					Recorder: Recorder,
				}

				n_b, err := news.Bytes()
				if err != nil {
					logger.PrintErr("failed conver to news: %s", err)
					continue
				}
				if err := rt.Send(CORE_ROUTER_ID, n_b); err != nil {
					if err != router.ErrUnconnectPort && err != router.ErrClosedPort {
						return err
					}

					rt.Close()
					rt, err = router.Connect(ctx, RouterId, SockPath)
					if err != nil {
						return err
					}
					if err := rt.Send(CORE_ROUTER_ID, n_b); err != nil {
						return err
					}
				}
			}
		}
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

func die(s string, msg ...interface{}) {
	fmt.Fprintf(os.Stderr, s + "\n" , msg...)
	os.Exit(1)
}

func init() {
	var rid        int
	var sleep_time int
	flag.IntVar(&rid, "rid", int(ROUTER_ID), "router id.")
	flag.IntVar(&sleep_time, "s", 60, "sleep time(second).")
	flag.Parse()

	if flag.NArg() < 3 {
		die(USAGE)
	}
	if 0 > rid || rid > 255 {
		die("router id out of range from 0 to 255\n" + USAGE)
	}
	RouterId = uint8(rid)
	if 0 > sleep_time {
		die("sleep time too small than zero.\n" + USAGE)
	}
	SleepTime = sleep_time

	if flag.Arg(0) == "" {
		die("empty the path of socket.\n" + USAGE)
	}
	SockPath = filepath.Clean(flag.Arg(0))

	if flag.Arg(1) == "" {
		die("empty the name of recorder.\n" + USAGE)
	}
	Recorder = flag.Arg(1)

	if flag.Arg(2) == "" {
		die("empty the feed url.\n" + USAGE)
	}
	FeedUrl = flag.Arg(2)
}

func main() {
	if err := bwget_feed(); err != nil {
		die("%s", err)
	}
}
