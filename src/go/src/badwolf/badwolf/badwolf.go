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
	TYPE_TIMELORD  uint8 = 1
	TYPE_ANLYZER   uint8 = 2
	TYPE_GETTER    uint8 = 3
	TYPE_PUBLISHER uint8 = 4

	HTTPGET_OPT_ORSTR string = " "
)

var (
	ErrAlreadyExistRecord error = fmt.Errorf("already exist record")
)

type Timelord struct {
	rt    *router.Router
	tv    *timevortex.TimeVortex
	r_tbl *router.RouteTable

	mtx    *sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup

	test_analyz_chan chan *timevortex.News
}

func NewTimelord(b_ctx context.Context, tv_path string, sock_path string) (*Timelord, error) {
	logger.PrintMsg("[init] initilazing timelord...")
	defer logger.PrintMsg("[init] done initialize.")

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

	self := &Timelord{
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

func (self *Timelord) Run() {
	logger.PrintMsg("run the timelord.")
	self.recver()
}

func (self *Timelord) TestRunPublisher(port int, f_conf *FeedConf) {
	logger.PrintMsg("[TEST_FUNCTION] call the function of publisher.")
	self.run_publisher(port, f_conf)
}

func (self *Timelord) TestRunAnalyzer(filter map[string]*Filter) {
	logger.PrintMsg("[TEST_FUNCTION] call the function of analyzer.")
	defer logger.PrintMsg("[Analyzer] start the analyzer.")

	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		for {
			select {
			case <- self.ctx.Done():
				return
			case news, ok := <- self.test_analyz_chan:
				if !ok {
					return
				}
				go self.test_analyzer(filter, news)
			}
		}
	}()
}

func (self *Timelord) Close() {
	self.lock()
	defer self.unlock()

	logger.PrintMsg("[Closer] closing timelord...")
	defer logger.PrintMsg("[Closer] closed timelord.")

	defer self.wg.Wait()
	self.cancel()

	self.rt.Close()
	self.tv.Close()
}

func (self *Timelord) run_publisher(port int, f_conf *FeedConf) {
	defer logger.PrintMsg("[Publisher] start the publsher. 127.0.0.1:%v", port)

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
				logger.PrintErr("Timelord.run_publisher: GenerateFeed : %s", err)
				http.Error(w, "Failed: generate feed body.", http.StatusInternalServerError)
				return
			}
			rss, err := f.ToRss()
			if err != nil {
				logger.PrintErr("Timelord.run_publisher: GenerateFeed : %s", err)
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
					logger.PrintErr("Timelord.run_publisher: httpListenAndServe: %s", err)
				}
				logger.PrintMsg("Timelord.run_publisher: httpListenAndServe: ServerClose")
				return
			}
		}()

		go func() {
			<- self.ctx.Done()
			if err := sv.Shutdown(self.ctx); err != nil {
				logger.PrintErr("Timelord.run_publisher: httpListenAndServe: %s", err)
				return
			}
		}()
	}()
}

func (self *Timelord) post2Analyzer(news *timevortex.News) {
	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		for {
			select {
			case <- self.ctx.Done():
				return
			case self.test_analyz_chan <- news:
				return
			}
		}
	}()
}

func (self *Timelord) test_analyzer(filter map[string]*Filter, news *timevortex.News) {
	check_val := news.Title + news.Summary + news.Link

	for cname, f := range filter {
		func() {
			if f.Or != nil {
				for _, val := range f.Or {
					if strings.Contains(check_val, val) {
						if err := self.tv.UpdateCategory(news.Id(), "TEST_EMBEDED_Analyzer", cname); err != nil {
							logger.PrintErr("Timelord.test_analyzer: failed update category: %v", err)
							return
						}
						logger.PrintMsg("[Analyzer] Added category: %s", cname)
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
				if err := self.tv.UpdateCategory(news.Id(), "TEST_EMBEDED_Analyzer", cname); err != nil {
					logger.PrintErr("Timelord.test_analyzer: failed update category: %v", err)
					return
				}
				logger.PrintMsg("[Analyzer] Added category: %s", cname)
				return
			}
		}()
	}
}

func (self *Timelord) recver() {
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
				logger.PrintErr("Timeload.run_recver: unkown operation.")
				continue
			}
		}
	}
}

func (self *Timelord) run_recordNews(from uint8, body []byte) {
	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		news, err := timevortex.Bytes2News(body)
		if err != nil {
			return
		}

		if err := self.tv.AddNews(news); err != nil {
			if err != timevortex.ErrAlreadyExist {
				logger.PrintErr("Timeload.run_recordNews: Failed news adding. : %s", err)
				return
			}

			logger.PrintErr("Timeload.run_recordNews: tried already exist data.")
			p_b := CreateBytesPacket(flg_R_NEWS, news.Id())
			if err := self.rt.Send(from, p_b); err != nil {
				logger.PrintErr("Timeload.run_recordNews: Failed message reply : %s", err)
				return
			}
			return
		}

		logger.PrintMsg("[Recorder] got news. recorded to timevortex.")
		self.post2Analyzer(news)
		p_b := CreateBytesPacket(flg_R_NEWS, news.Id())
		if err := self.rt.Send(from, p_b); err != nil {
			logger.PrintErr("Timeload.run_recordNews: Failed message reply : %s", err)
			return
		}
	}()
}

func (self *Timelord) run_recordRoute(from uint8, body []byte) {
	self.wg.Add(1)
	go func() {
		defer self.wg.Done()

		if len(body) < 1 {
			logger.PrintErr("Timeload.run_recordRoute: data size too short.")
			return
		}
		self.r_tbl.Add(uint8(body[0]), from)
		body := CreateBytesPacket(flg_SYSTEM_TYPE, []byte{TYPE_TIMELORD})
		if err := self.rt.Send(from, body); err != nil {
			logger.PrintErr("Timeload.run_recordRoute: failed send my type to from")
			return
		}
	}()
}

func (self *Timelord) lock() {
	self.mtx.Lock()
}

func (self *Timelord) unlock() {
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
	logger.PrintMsg("[init] initilazing timelord...")
	defer logger.PrintMsg("[init] done initialize.")

	if b_ctx == nil {
		b_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(b_ctx)

	logger.PrintMsg("[init] connect the interface of router to %s", sock_path)
	sock_cpath := filepath.Clean(sock_path)
	rt, err := router.Connect(newChildContext(ctx), sock_cpath)
	if err != nil {
		return nil, err
	}
	logger.PrintMsg("[init] connected the interface")

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
	logger.PrintMsg("[Closer] closing timelord...")
	defer logger.PrintMsg("[Closer] closed timelord.")

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
					logger.PrintErr("Getter.run_recver: unkown operation.")
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
			logger.PrintErr("Getter.run_recordRoute: data size too short.")
			return
		}
		self.r_tbl.Add(uint8(body[0]), from)
	}()
}

func (self *Getter) notice_type() {
	body := CreateBytesPacket(flg_SYSTEM_TYPE, []byte{TYPE_GETTER})

	if err := self.rt.Send(router.BLOADCAST_RID, body); err != nil {
		logger.PrintErr("Getter.notice_type: failed send my type to from")
		return
	}
}

func (self *Getter) Post(news *timevortex.News) error {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	_, ok := self.posted[string(news.Id())]
	if ok {
		return ErrAlreadyExistRecord
	}

	n_b, err := news.Bytes()
	if err != nil {
		return fmt.Errorf("Getter.Post: failed conver to news: %s", err)
	}
	p_b := CreateBytesPacket(flg_S_NEWS, n_b)

	dsts, err := self.r_tbl.Find(TYPE_TIMELORD)
	if err != nil {
		return fmt.Errorf("Getter.Post: failed find target: %s", err)
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
				return fmt.Errorf("Getter.Post: Can't send a data to badwolf lord: %s", err)
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
	layout := "2006-01-02"

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
		var tools []string
		var categories []string

		if tool != "" {
			tools = strings.SplitN(tool, HTTPGET_OPT_ORSTR, 255)
		}
		if category != "" {
			categories = strings.SplitN(category, HTTPGET_OPT_ORSTR, 255)
		}
		opt = timevortex.NewOptions(tools, categories)
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
