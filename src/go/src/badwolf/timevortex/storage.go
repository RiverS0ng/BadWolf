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
	"github.com/google/uuid"
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

func (self *TimeVortex) AddNewEvent(tool string, news *News) (*Event, error) {
	self.lock()
	defer self.unlock()

	eid := generateEvtID()
	news_b, err := news.Bytes()
	if err != nil {
		return nil, err
	}

	if err := self.timeline.Put(eid, news_b, nil); err != nil {
		return nil, err
	}

	if err := self.updateCategory(tool, CATEGORY_ORG, [][]byte{eid}); err != nil {
		return nil, err
	}
	return newEvent(eid, news), nil
}

func (self *TimeVortex) DeleteEvent(eid []byte) error {
	self.lock()
	defer self.unlock()

	return self.timeline.Delete(eid, nil)
}

func (self *TimeVortex) UpdateCategory(tool string, category string, eids [][]byte) error {
	self.lock()
	defer self.unlock()

	return self.updateCategory(tool, category, eids)

}

func (self *TimeVortex) updateCategory(tool string, category string, eids [][]byte) error {
	b_tool := []byte(tool)
	if len(b_tool) > SIZE_MAX_TOOLNAME {
		return fmt.Errorf("over the tool value size.")
	}
	b_category := []byte(category)
	if len(b_category) > SIZE_MAX_CATEGORYNAME {
		return fmt.Errorf("over the category value size.")
	}

	cid := make([]byte, 0, 4096)
	utime := uint64(time.Now().Unix())
	b_utime := make([]byte, 8)
	binary.BigEndian.PutUint64(b_utime, utime)
	cid = append(cid, b_utime...)

	flg := make([]byte, 8)
	cid = append(cid, flg...)

	c_trunc := SIZE_LIMIT_CATEGORYNAME - len(b_category)
	c_dummy := make([]byte, c_trunc)
	cid = append(cid, b_category...)
	cid = append(cid, c_dummy...)

	t_trunc := SIZE_LIMIT_TOOLNAME - len(b_tool)
	t_dummy := make([]byte, t_trunc)
	cid = append(cid, b_tool...)
	cid = append(cid, t_dummy...)

	b_eids := []byte{}
	for _, eid := range eids {
		b_eids = append(b_eids, eid...)
	}

	if err := self.category.Put(cid, b_eids, nil); err != nil {
		return err
	}
	return nil
}

func (self *TimeVortex) Find(c context.Context, st time.Time, et time.Time, opt *Options) ([]*Event, error) {
	self.rlock()
	defer self.runlock()

	b_st := make([]byte, 8)
	b_et := make([]byte, 8)
	binary.BigEndian.PutUint64(b_st, uint64(st.Unix()))
	binary.BigEndian.PutUint64(b_et, uint64(et.Unix()))

	if opt == nil {
		return self.walkEvent(c, b_st, b_et)
	}
	return self.findEvent(c, b_st, b_et, opt)
}

func (self *TimeVortex) walkEvent(c context.Context, b_st []byte, b_et []byte) ([]*Event, error) {
	iter := self.timeline.NewIterator(&util.Range{Start:b_st, Limit:b_et}, nil)

	evts := []*Event{}
	for iter.Next() {
		eid := iter.Key()
		b_news := iter.Value()

		news, err := Bytes2News(b_news)
		if err != nil {
			return nil, fmt.Errorf("findEvent: parse failed: %s", err)
		}
		evts = append(evts, newEvent(eid, news))
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return evts, nil
}

func (self *TimeVortex) findEvent(c context.Context, b_st []byte, b_et []byte, opt *Options) ([]*Event, error) {
	eid_ch := make(chan []byte)
	evts := []*Event{}
	wg := new(sync.WaitGroup)

	go func() {
		defer close(eid_ch)
		defer wg.Wait()

		iter := self.category.NewIterator(&util.Range{Start:b_st, Limit:b_et}, nil)

		for iter.Next() {
			k := iter.Key()
			v := iter.Value()

			limit_c := 16 + SIZE_LIMIT_CATEGORYNAME
			limit_t := limit_c + SIZE_LIMIT_TOOLNAME
			b_cat := k[16:limit_c]
			b_tname := k[limit_c:limit_t]

			if !opt.Match(b_cat, b_tname) {
				continue
			}

			wg.Add(1)
			go func() {
				defer wg.Done()

				h := 0
				for h < len(v) {
					l := h + 16

					select {
					case <- c.Done():
						return
					case eid_ch <- v[h:l]:
					}
					h = l
				}
			}()
		}
	}()

	for {
		select {
		case <-c.Done():
			return nil, fmt.Errorf("findEvent: canceled")
		case eid, ok := <-eid_ch:
			if !ok {
				return evts, nil
			}

			b_news, err := self.timeline.Get(eid, nil)
			if err != nil {
				if err != leveldb.ErrNotFound {
					return nil, err
				}
				continue
			}

			news, err := Bytes2News(b_news)
			if err != nil {
				return nil, fmt.Errorf("findEvent: parse failed: %s", err)
			}
			evts = append(evts, newEvent(eid, news))
		}
	}
	return evts, nil
}

//func (self *TimeVortex) NewEventIter() //TODO:future

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
	tool     string
	category string
}

func NewOptions(tool string, category string) *Options {
	return &Options{tool:tool, category:category}
}

func (self *Options) Match(b_category []byte, b_tool []byte) bool {
	if self.category != "" {
		b_c := []byte(self.category)
		c_trunc := SIZE_LIMIT_CATEGORYNAME - len(b_c)
		c_dummy := make([]byte, c_trunc)
		b_c = append(b_c, c_dummy...)

		if string(b_c) != string(b_category) {
			return false
		}
	}

	if self.tool != "" {
		b_t := []byte(self.tool)
		t_trunc := SIZE_LIMIT_TOOLNAME - len(b_t)
		t_dummy := make([]byte, t_trunc)
		b_t = append(b_t, t_dummy...)

		if string(b_t) != string(b_tool) {
			return false
		}
	}

	return true
}

func generateEvtID() []byte {
	utime := uint64(time.Now().Unix())
	uuidv2 := uint64(uuid.New().ID())
	b_utime := make([]byte, 8)
	b_uuidv2 := make([]byte, 8)
	binary.BigEndian.PutUint64(b_utime, utime)
	binary.BigEndian.PutUint64(b_uuidv2, uuidv2)

	evt_id := append(b_utime, b_uuidv2...)
	return evt_id
}
