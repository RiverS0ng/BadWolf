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
	"badwolf/packet"
)

const (
	USAGE string =  "Usage : bwget_feed [ -s <sleep time(second)> ] <socket path> <recorder name> <feed url>"
)

var (
	Recorder string
	SockPath string
	FeedUrl   string

	SleepTime int
)

func bwget_feed() error {
	logger.PrintMsg("bwget_feed starting...")
	defer logger.PrintMsg("bwget_feed exit")
	logger.PrintMsg("Recorder : %s", Recorder)
	logger.PrintMsg("FeedUrl : %s", FeedUrl)
	logger.PrintMsg("SockPath : %s", SockPath)
	ctx, cancel := context.WithCancel(context.Background())
	signalInterruptHandler(cancel)
	defer cancel()

	logger.PrintMsg("router connecting... : %s", SockPath)
	rt, err := router.Connect(ctx, SockPath)
	if err != nil {
		return err
	}
	defer rt.Close()
	logger.PrintMsg("router connected")

	posted := make(map[string]interface{})
	tc := time.NewTicker(time.Second * time.Duration(SleepTime))
	fp := gofeed.NewParser()

	go func() {
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
					logger.PrintErr("cant convert frame body to packet : %s", err)
					continue
				}
				if p.Flg() != packet.F_R_NEW_NEWS {
					logger.PrintErr("unkown operation")
					continue
				}

				posted[string(p.Body())] = nil
			}
		}
	}()

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

				_, ok := posted[string(news.Id())]
				if ok {
					continue
				}

				n_b, err := news.Bytes()
				if err != nil {
					logger.PrintErr("failed conver to news: %s", err)
					continue
				}
				p_b := packet.CreateBytes(packet.F_S_NEW_NEWS, n_b)
				if err := rt.Send(router.BLOADCAST_RID, p_b); err != nil {
					if err != router.ErrUnconnectPort && err != router.ErrClosedPort {
						return err
					}

					rt.Close()
					rt, err = router.Connect(ctx, SockPath)
					if err != nil {
						return err
					}
					if err := rt.Send(router.BLOADCAST_RID, n_b); err != nil {
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
	var sleep_time int
	flag.IntVar(&sleep_time, "s", 900, "sleep time(second).")
	flag.Parse()

	if flag.NArg() < 3 {
		die(USAGE)
	}
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
