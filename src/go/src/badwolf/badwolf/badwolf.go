package badwolf

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"
	"context"
	"strings"
)

import (
	"github.com/gorilla/feeds"
)

import (
	"badwolf/logger"
	"badwolf/router"
	"badwolf/timevortex"
)

const (
	TYPE_OWNER     uint8 = 1
	TYPE_ANLYZER   uint8 = 2
	TYPE_GETTER    uint8 = 3
	TYPE_PUBLISHER uint8 = 4
)

type Owner struct {
	rt    *router.Router
	tv    *timevortex.TimeVortex
	r_tbl *router.RouteTable

	mtx    *sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup

	test_analyz_chan chan *timevortex.News
}

func NewOwner(b_ctx context.Context, tv_path string, sock_path string) (*Owner, error) {
	if b_ctx == nil {
		b_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(b_ctx)

	logger.PrintMsg("[init] open the timevortex at %s", tv_path)
	tv_cpath := filepath.Clean(tv_path)
	tv, err := timevortex.OpenTimeVortex(newChildContext(ctx), tv_cpath)
	if err != nil {
		logger.PrintErr("[init] Failed: open the timevortex: %s", err)
		return nil, err
	}
	logger.PrintMsg("[init] opened the timevortex.")

	logger.PrintMsg("[init] create the interface of router to %s", sock_path)
	sock_cpath := filepath.Clean(sock_path)
	rt, err := router.NewRouter(newChildContext(ctx), sock_cpath)
	if err != nil {
		logger.PrintErr("[init] Failed: create the interface: %s", err)
		return nil, err
	}
	logger.PrintMsg("[init] created the interface.")

	self := &Owner{
		rt: rt,
		tv: tv,
		r_tbl: router.NewRouteTable(),
		mtx: new(sync.Mutex),
		ctx: ctx,
		cancel: cancel,
		wg: new(sync.WaitGroup),

		test_analyz_chan: make(chan *timevortex.News),
	}

	return self, nil
}

func (self *Owner) Run() {
	self.recver()
}

func (self *Owner) TestRunPublisher(port int, f_conf *FeedConf) {
	self.run_publisher(port, f_conf)
}

func (self *Owner) TestRunAnalyzer(filter map[string]*Filter) {
	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		select {
		case <- self.ctx.Done():
			return
		case news := <- self.test_analyz_chan:
			go self.test_analyzer(filter, news)
		}
	}()
}

func (self *Owner) Close() {
	self.lock()
	defer self.unlock()

	defer self.wg.Wait()
	self.cancel()

	self.rt.Close()
	self.tv.Close()
}

func (self *Owner) run_publisher(port int, f_conf *FeedConf) {
	sv := &http.Server{
		Addr: "127.0.0.1:" + fmt.Sprintf("%v", port),
	}
	http.HandleFunc("/", func (w http.ResponseWriter, r *http.Request) {
		logger.PrintMsg("[HTTP Request] %s, %s, %s", r.RemoteAddr, r.Method, r.URL)
		switch r.Method {
		case "GET" :
			start := r.URL.Query().Get("start")
			end := r.URL.Query().Get("end")
			category := r.URL.Query().Get("category")
			tool := r.URL.Query().Get("tool")

			f, err := GenerateFeed(self.ctx, self.tv, start, end, category, tool, f_conf)
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

	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		self.wg.Add(1)
		go func() {
			defer self.wg.Done()

			if err := sv.ListenAndServe(); err != nil {
				if err != http.ErrServerClosed {
					logger.PrintErr("run_publisher: httpListenAndServe: %s", err)
				}
				logger.PrintMsg("run_publisher: httpListenAndServe: ServerClose")
				return
			}
		}()

		go func() {
			<- self.ctx.Done()
			if err := sv.Shutdown(self.ctx); err != nil {
				logger.PrintErr("run_publisher: httpListenAndServe: %s", err)
				return
			}
		}()
	}()
}

func (self *Owner) post2Analyzer(news *timevortex.News) {
	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		select {
		case <- self.ctx.Done():
			return
		case self.test_analyz_chan <- news:
			return
		}
	}()
}

func (self *Owner) test_analyzer(filter map[string]*Filter, news *timevortex.News) {
	check_val := news.Title + news.Summary + news.Link

	for cname, f := range filter {
		func() {
			if f.Or != nil {
				for _, val := range f.Or {
					if strings.Contains(check_val, val) {
						if err := self.tv.UpdateCategory("TEST_EMBEDED_ANALYZER", cname, [][]byte{news.Id()}); err != nil {
							logger.PrintErr("test_analyzer: failed update category: %v", err)
							return
						}
						logger.PrintMsg("[TEST FUNCTION] Added category: %s", cname)
						return
					}
				}
			}

			if f.And != nil {
				for _, val := range f.And {
					if !strings.Contains(check_val, val) {
						return
					}
				}
				if err := self.tv.UpdateCategory("TEST_EMBEDED_ANALYZER", cname, [][]byte{news.Id()}); err != nil {
					logger.PrintErr("test_analyzer: failed update category: %v", err)
					return
				}
				logger.PrintMsg("[TEST FUNCTION] Added category: %s", cname)
				return
			}
		}()
	}
}

func (self *Owner) recver() {
	for {
		select {
		case <- self.ctx.Done():
			return
		case f, ok := <- self.rt.Recv():
			if !ok {
				return
			}

			p, err := Bytes2Packet(f.Body())
			if err != nil {
				continue
			}

			from := f.SrcId()
			switch p.Flg() {
			case flg_SYSTEM_TYPE:
				self.run_recordRoute(from, p.Body())
				continue
			case flg_S_NEWS:
				self.run_recordNews(from, p.Body())
				continue
				/*
			case flg_R_ANLYZ:
				self.run_recordCategory(from, p.Body())
				continue
			case flg_S_SEARCH:
				self.run_searchTV(from, p.Body())
				continue
				*/
			default:
				logger.PrintErr("run_recver: unkown operation.")
				continue
			}
		}
	}
}

func (self *Owner) run_recordNews(from uint8, body []byte) {
	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		news, err := timevortex.Bytes2News(body)
		if err != nil {
			return
		}

		if err := self.tv.AddNews(news); err != nil {
			if err != timevortex.ErrAlreadyExist {
				logger.PrintErr("run_newsRecorder: failed add news : %s", err)
				return
			}

			logger.PrintErr("run_newsRecorder: tried already exist data.")
		}

		self.post2Analyzer(news)
		p_b := CreateBytesPacket(flg_R_NEWS, news.Id())
		if err := self.rt.Send(from, p_b); err != nil {
			logger.PrintErr("run_newsRecorder: failed reply : %s", err)
			return
		}
	}()
}

func (self *Getter) notice_type() {
	body := CreateBytesPacket(flg_SYSTEM_TYPE, []byte{TYPE_GETTER})

	if err := self.rt.Send(router.BLOADCAST_RID, body); err != nil {
		logger.PrintErr("run_recordType: failed send my type to from")
		return
	}
}

func (self *Owner) run_recordRoute(from uint8, body []byte) {
	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		if len(body) < 1 {
			logger.PrintErr("run_recordType: data size too short.")
			return
		}
		self.r_tbl.Add(uint8(body[0]), from)
		body := CreateBytesPacket(flg_SYSTEM_TYPE, []byte{TYPE_OWNER})
		if err := self.rt.Send(from, body); err != nil {
			logger.PrintErr("run_recordType: failed send my type to from")
			return
		}
	}()
}

func (self *Owner) lock() {
	self.mtx.Lock()
}

func (self *Owner) unlock() {
	self.mtx.Unlock()
}

type Getter struct {
	rt    *router.Router
	r_tbl *router.RouteTable

	socket_path string
	posted map[string]interface{}

	mtx    *sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup
}

func NewGetter(b_ctx context.Context, sock_path string) (*Getter, error) {
	if b_ctx == nil {
		b_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(b_ctx)

	sock_cpath := filepath.Clean(sock_path)
	rt, err := router.Connect(newChildContext(ctx), sock_cpath)
	if err != nil {
		return nil, err
	}

	self := &Getter{
		rt: rt,
		r_tbl: router.NewRouteTable(),
		posted: make(map[string]interface{}),
		socket_path: sock_cpath,
		mtx: new(sync.Mutex),
		ctx: ctx,
		cancel: cancel,
		wg: new(sync.WaitGroup),
	}
	self.run_recver()

	return self, nil
}

func (self *Getter) Close() {
	defer self.wg.Wait()
	self.cancel()

	self.rt.Close()
}

func (self *Getter) run_recver() {
	self.wg.Add(1)
	self.notice_type()

	go func(){
		defer self.wg.Done()

		for {
			select {
			case <- self.ctx.Done():
				return
			case f, ok := <- self.rt.Recv():
				if !ok {
					return
				}

				p, err := Bytes2Packet(f.Body())
				if err != nil {
					continue
				}

				from := f.SrcId()
				switch p.Flg() {
				case flg_SYSTEM_TYPE:
					self.run_recordRoute(from, p.Body())
					continue
				case flg_R_NEWS:
					self.posted[string(p.Body())] = nil
					continue
				default:
					logger.PrintErr("run_recver: unkown operation.")
					continue
				}
			}
		}
	}()
}

func (self *Getter) run_recordRoute(from uint8, body []byte) {
	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		if len(body) < 1 {
			logger.PrintErr("run_recordRoute: data size too short.")
			return
		}
		self.r_tbl.Add(uint8(body[0]), from)
	}()
}

func (self *Getter) Post(news *timevortex.News) error {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	_, ok := self.posted[string(news.Id())]
	if ok {
		return fmt.Errorf("already record")
	}

	n_b, err := news.Bytes()
	if err != nil {
		return fmt.Errorf("failed conver to news: %s", err)
	}
	p_b := CreateBytesPacket(flg_S_NEWS, n_b)

	dsts, err := self.r_tbl.Find(TYPE_OWNER)
	if err != nil {
		return fmt.Errorf("failed find target: %s", err)
	}
	for _, dst := range dsts {
		if err := self.rt.Send(dst, p_b); err != nil {
			if err != router.ErrUnconnectPort && err != router.ErrClosedPort {
				return err
			}

			self.rt.Close()
			self.rt, err = router.Connect(self.ctx, self.socket_path)
			if err != nil {
				return err
			}
			self.notice_type()
			if err := self.rt.Send(dst, n_b); err != nil {
				return err
			}
		}
	}
	return nil
}

type Analyzer struct {}
type Publisher struct {}

func newChildContext(ctx context.Context) context.Context {
	c_ctx, _ := context.WithCancel(ctx)
	return c_ctx
}

func GenerateFeed(ctx context.Context, tv *timevortex.TimeVortex,
	start string, end string, category string, tool string, f_conf *FeedConf) (*feeds.Feed, error) {
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
		Title:       f_conf.Title,
		Link:        &feeds.Link{Href: f_conf.Link},
		Description: f_conf.Description,
		Author:      &feeds.Author{Name: f_conf.AuthorName,
								Email: f_conf.AuthorEmal},
		Created:     time.Now(),
		Items:       items,
	}
	return body, nil
}

type FeedConf struct {
	Title       string
	Link        string
	Description string
	AuthorName  string
	AuthorEmal  string
}

type Filter struct {
	And []string
	Or  []string
}
