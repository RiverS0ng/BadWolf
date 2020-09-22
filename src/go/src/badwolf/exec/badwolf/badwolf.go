package main

import (
	"os"
	"os/signal"
	"flag"
	"path/filepath"
	"context"
)

import (
	"github.com/BurntSushi/toml"
)

import (
	"badwolf/badwolf"
	"badwolf/logger"
)

var (
	Conf *Config
)

func bw_own() error {
	logger.PrintMsg("starting badwolf....")
	defer logger.PrintMsg("Exit badwolf")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalInterruptHandler(cancel)

	bw_o, err := badwolf.NewOwner(ctx, Conf.Server.Db, Conf.Server.Socket)
	if err != nil {
		return err
	}
	defer bw_o.Close()

	f_conf := &badwolf.FeedConf{
		Title: Conf.Feed.Title,
		Link: Conf.Feed.Link,
		Description: Conf.Feed.Description,
		AuthorName: Conf.Feed.AuthorName,
		AuthorEmal: Conf.Feed.AuthorEmail,
	}
	bw_o.TestRunPublisher(Conf.Server.Port, f_conf)
	bw_o.TestRunAnalyzer(Conf.Filters)

	bw_o.Run()
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
	logger.PrintErr(s, msg...)
	os.Exit(1)
}

type Config struct {
	Server  *Server
	Feed    *Feed
	Filters map[string]*badwolf.Filter //TODO:Test
}

type Server struct {
	Db     string
	Port   int
	Socket string
}

type Feed struct {
	Title       string
	Link        string
	Description string
	AuthorName  string `toml:"author_name"`
	AuthorEmail  string `toml:"author_email"`
}

func loadConfig(path string) (*Config, error) {
	var conf Config

	fpath := filepath.Clean(path)
	if _, err := toml.DecodeFile(fpath, &conf); err != nil {
		return nil, err
	}
	return &conf, nil
}

func init() {
	var c_path string
	flag.StringVar(&c_path, "c", "", "config path.")
	flag.Parse()

	if c_path == "" {
		die("empty config path.\nUsage : badwolf -c <config path>")
	}
	cnf, err := loadConfig(c_path)
	if err != nil {
		die("can't load config : %s", err)
	}
	if 0 >= cnf.Server.Port || cnf.Server.Port > 65535 {
		die("port number out of range.")
	}
	Conf = cnf
}

func main() {
	if err := bw_own(); err != nil {
		die("%s", err)
	}
}
