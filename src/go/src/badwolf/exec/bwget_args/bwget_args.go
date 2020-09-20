package main

import (
	"os"
	"fmt"
	"flag"
	"path/filepath"
	"time"
	"strconv"
)

import (
	"badwolf/router"
	"badwolf/timevortex"
	"badwolf/packet"
)

const (
	RECORDER string = "bwget_args"

	USAGE string =  "Usage : bwget_args [-r <recorder name> ] <socket path> <Title> <Link url> <Summary> <Publish date(unixtime)> <Source url>"
)

var (
	Title    string
	Link     string
	Summary  string
	PubDate  time.Time
	Source   string
	Recorder string

	SockPath string
)

func bwget_args() error {
	news := &timevortex.News{
		Title: Title,
		Link: Link,
		Summary: Summary,
		PubDate: PubDate,
		Source: Source,
		Recorder: Recorder,
	}
	n_b, err := news.Bytes()
	if err != nil {
		return err
	}

	rt, err := router.Connect(nil, SockPath)
	if err != nil {
		return err
	}
	defer rt.Close()

	p_b := packet.CreateBytes(packet.F_S_NEW_NEWS, n_b)
	if err := rt.Send(router.BLOADCAST_RID, p_b); err != nil {
		return err
	}
	return nil
}

func die(s string, msg ...interface{}) {
	fmt.Fprintf(os.Stderr, s + "\n" , msg...)
	os.Exit(1)
}

func init() {
	var recorder string
	flag.StringVar(&recorder, "r", RECORDER, "name of recorder.")
	flag.Parse()

	if flag.NArg() < 6 {
		die(USAGE)
	}
	if recorder == "" {
		die("empty the name of recorder.\n" + USAGE)
	}
	Recorder = recorder

	if flag.Arg(0) == "" {
		die("empty the path of socket.\n" + USAGE)
	}
	SockPath = filepath.Clean(flag.Arg(0))

	if flag.Arg(1) == "" {
		die("empty the title.\n" + USAGE)
	}
	Title = flag.Arg(1)

	if flag.Arg(2) == "" {
		die("empty the link url.\n" + USAGE)
	}
	Link = flag.Arg(2)

	if flag.Arg(3) == "" {
		die("empty the summary.\n" + USAGE)
	}
	Summary = flag.Arg(3)

	if flag.Arg(4) == "" {
		die("empty the publish time.\n" + USAGE)
	}
	utime, err := strconv.ParseInt(flag.Arg(4), 10, 64)
	if err != nil {
		die("can't parse publish time : %s.\n" + USAGE, err)
	}
	PubDate = time.Unix(utime, 0)

	if flag.Arg(5) == "" {
		die("empty the source url.\n" + USAGE)
	}
	Source = flag.Arg(5)
}

func main() {
	if err := bwget_args(); err != nil {
		die("%s", err)
	}
}
