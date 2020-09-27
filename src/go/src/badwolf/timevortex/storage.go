package timevortex

import (
	"os"
	"fmt"
	"path/filepath"
	"sync"
	"time"
	"context"
	"encoding/binary"
)

import (
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

const (
	UMASK_DIR os.FileMode = 0775
	FNAME_TIMELINE string = "tl"
	FNAME_CATEGORY string = "ctgry"

	CATEGORY_ORG   string = ""

	SIZE_MAX_CATEGORYNAME int = 1024
	SIZE_LIMIT_CATEGORYNAME int = 2040
	SIZE_MAX_TOOLNAME int = 1024
	SIZE_LIMIT_TOOLNAME int = 2040
)

var (
	ErrAlreadyExist error = fmt.Errorf("the data is already exist.")
)

type TimeVortex struct {
	timeline *leveldb.DB //id(time(uint64)+uuid(int64)), type of news
	category *leveldb.DB //id(time(uint64)+flag(int64)+category(2040)+tools(2040){key size is blocksize}, []timeline_id

	ctx    context.Context
	cancel context.CancelFunc

	wg     *sync.WaitGroup
	mtx    *sync.RWMutex
}

func CreateTimeVortex(path string) error {
	c_path := filepath.Clean(path)

	if err := os.Mkdir(c_path, UMASK_DIR); err != nil {
		return err
	}

	tl_path := filepath.Join(c_path, FNAME_TIMELINE)
	tl_db, err := leveldb.OpenFile(tl_path, &opt.Options{ ErrorIfExist: true })
	if err != nil {
		return err
	}
	tl_db.Close()

	ct_path := filepath.Join(c_path, FNAME_CATEGORY)
	ct_db, err := leveldb.OpenFile(ct_path, &opt.Options{ ErrorIfExist: true })
	if err != nil {
		return err
	}
	ct_db.Close()

	return nil
}

func DeleteTimeVortex(path string) error {
	c_path := filepath.Clean(path)
	tl_path := filepath.Join(c_path, FNAME_TIMELINE)
	ct_path := filepath.Join(c_path, FNAME_CATEGORY)

	if err := os.RemoveAll(tl_path); err != nil {
		return err
	}
	if err := os.RemoveAll(ct_path); err != nil {
		return err
	}
	if err := os.Remove(c_path); err != nil {
		return err
	}
	return nil
}

func OpenTimeVortex(bg_ctx context.Context, path string) (*TimeVortex, error) {
	if bg_ctx == nil {
		bg_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(bg_ctx)


	c_path := filepath.Clean(path)
	tl_path := filepath.Join(c_path, FNAME_TIMELINE)
	timeline, err := leveldb.OpenFile(tl_path, &opt.Options{ ErrorIfMissing: false })
	if err != nil {
		return nil, err
	}
	ct_path := filepath.Join(c_path, FNAME_CATEGORY)
	category, err := leveldb.OpenFile(ct_path, &opt.Options{ ErrorIfMissing: false })
	if err != nil {
		return nil, err
	}

	self := &TimeVortex{
		timeline:timeline,
		category:category,

		ctx:ctx,
		cancel:cancel,
		wg:new(sync.WaitGroup),
		mtx:new(sync.RWMutex),
	}
	return self, nil
}

func (self *TimeVortex) Close() {
	self.lock()
	defer self.unlock()

	self.cancel()

	self.timeline.Close()
	self.category.Close()
	self.wg.Wait()
}

func (self *TimeVortex) AddNews(news *News) error {
	self.lock()
	defer self.unlock()

	id := news.Id()
	news_b, err := news.Bytes()
	if err != nil {
		return err
	}

	if _, err := self.timeline.Get(id, nil); err == nil {
		return ErrAlreadyExist
	}
	if err := self.timeline.Put(id, news_b, nil); err != nil {
		return err
	}

	if err := self.updateCategory(id, news.Recorder, CATEGORY_ORG); err != nil {
		return err
	}
	return nil
}

func (self *TimeVortex) DeleteNews(id []byte) error {
	self.lock()
	defer self.unlock()

	return self.timeline.Delete(id, nil)
}

func (self *TimeVortex) UpdateCategory(id []byte, tool string, category string) error {
	self.lock()
	defer self.unlock()

	return self.updateCategory(id, tool, category)
}

func (self *TimeVortex) updateCategory(id []byte, tool string, category string) error {
	b_tool := []byte(tool)
	if len(b_tool) > SIZE_MAX_TOOLNAME {
		return fmt.Errorf("over the tool value size.")
	}
	b_category := []byte(category)
	if len(b_category) > SIZE_MAX_CATEGORYNAME {
		return fmt.Errorf("over the category value size.")
	}

	cid := make([]byte, 0, 4096)
	cid = append(cid, id...)

	c_trunc := SIZE_LIMIT_CATEGORYNAME - len(b_category)
	c_dummy := make([]byte, c_trunc)
	cid = append(cid, b_category...)
	cid = append(cid, c_dummy...)

	t_trunc := SIZE_LIMIT_TOOLNAME - len(b_tool)
	t_dummy := make([]byte, t_trunc)
	cid = append(cid, b_tool...)
	cid = append(cid, t_dummy...)

	if err := self.category.Put(cid, nil, nil); err != nil {
		return err
	}
	return nil
}

func (self *TimeVortex) Find(c context.Context, st time.Time, et time.Time, opt *Options) ([]*News, error) {
	self.rlock()
	defer self.runlock()

	b_st := make([]byte, 8)
	b_et := make([]byte, 8)
	binary.BigEndian.PutUint64(b_st, uint64(st.Unix()))
	binary.BigEndian.PutUint64(b_et, uint64(et.Unix()))

	if opt == nil {
		news_desc, err := self.walkNews(c, b_st, b_et)
		if err != nil {
			return nil, err
		}
		return reverse(news_desc), nil
	}
	news_desc, err := self.findNews(c, b_st, b_et, opt)
	if err != nil {
		return nil, err
	}
	return reverse(news_desc), nil
}

func reverse(news_s []*News) []*News {
	l := len(news_s)

	news_asc := make([]*News, l)
	for i, news := range news_s {
		news_asc[(l - 1 - i)] = news
	}
	return news_asc
}

func (self *TimeVortex) walkNews(c context.Context, b_st []byte, b_et []byte) ([]*News, error) {
	iter := self.timeline.NewIterator(&util.Range{Start:b_st, Limit:b_et}, nil)

	news_s := []*News{}
	for iter.Next() {
		b_news := iter.Value()

		news, err := Bytes2News(b_news)
		if err != nil {
			return nil, fmt.Errorf("walkNews: parse failed: %s", err)
		}
		news_s = append(news_s, news)
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return news_s, nil
}

func (self *TimeVortex) findNews(c context.Context, b_st []byte, b_et []byte, opt *Options) ([]*News, error) {
	news_s := []*News{}

	iter := self.category.NewIterator(&util.Range{Start:b_st, Limit:b_et}, nil)

	frs := make(map[string][]bool)
	for iter.Next() {
		k := iter.Key()

		limit_c := 16 + SIZE_LIMIT_CATEGORYNAME
		limit_t := limit_c + SIZE_LIMIT_TOOLNAME
		b_cat := k[16:limit_c]
		b_tool := k[limit_c:limit_t]

		s_id := string(k[0:16])
		fr, ok := frs[s_id]
		if !ok {
			fr = []bool{false, false}
		}

		fr[0] = fr[0] || opt.CheckCategory(b_cat)
		fr[1] = fr[1] || opt.CheckTool(b_tool)
		frs[s_id] = fr
	}

	for id, fr := range frs {
		if !(fr[0] && fr[1]) {
			continue
		}

		b_news, err := self.timeline.Get([]byte(id), nil)
		if err != nil {
			if err != leveldb.ErrNotFound {
				return nil, fmt.Errorf("findNews: can't got value: %s", err)
			}
			continue
		}

		news, err := Bytes2News(b_news)
		if err != nil {
			return nil, fmt.Errorf("findNews: parse failed: %s", err)
		}
		news_s = append(news_s, news)
	}

	return news_s, nil
}

//func (self *TimeVortex) NewNewsIter() //TODO:future

func (self *TimeVortex) lock() {
	self.mtx.Lock()
}

func (self *TimeVortex) unlock() {
	self.mtx.Unlock()
}

func (self *TimeVortex) rlock() {
	self.mtx.RLock()
}

func (self *TimeVortex) runlock() {
	self.mtx.RUnlock()
}

type Options struct {
	tools      []string
	categories []string
}

func NewOptions(tools []string, categories []string) *Options {
	if tools == nil {
		tools = []string{}
	}
	if categories == nil {
		categories = []string{}
	}
	return &Options{tools:tools, categories:categories}
}

func (self *Options) CheckCategory(c []byte) bool {
	if len(self.categories) < 1 {
		return true
	}

	for _, f_category := range self.categories {
		b_c := []byte(f_category)
		c_trunc := SIZE_LIMIT_CATEGORYNAME - len(b_c)
		c_dummy := make([]byte, c_trunc)
		b_c = append(b_c, c_dummy...)

		if string(b_c) == string(c) {
			return true
		}
	}
	return false
}

func (self *Options) CheckTool(t []byte) bool {
	if len(self.tools) < 1 {
		return true
	}

	for _, f_tool := range self.tools {
		b_t := []byte(f_tool)
		t_trunc := SIZE_LIMIT_TOOLNAME - len(b_t)
		t_dummy := make([]byte, t_trunc)
		b_t = append(b_t, t_dummy...)

		if string(b_t) == string(t) {
			return true
		}
	}
	return false
}
