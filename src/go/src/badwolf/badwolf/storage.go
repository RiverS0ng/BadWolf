package badwolf

import (
	"os"
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
	mtx    *sync.Mutex
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
	timeline, err := leveldb.OpenFile(tl_path, &opt.Options{ ErrorIfMissing: true })
	if err != nil {
		return nil, err
	}
	ct_path := filepath.Join(c_path, FNAME_CATEGORY)
	category, err := leveldb.OpenFile(ct_path, &opt.Options{ ErrorIfMissing: true })
	if err != nil {
		return nil, err
	}

	self := &TimeVortex{
		timeline:timeline,
		category:category,

		ctx:ctx,
		cancel:cancel,
		wg:new(sync.WaitGroup),
		mtx:new(sync.Mutex),
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

func (self *TimeVortex) AddNewEvent(t_name string, news *News) (*Event, error) {
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

	if err := self.updateCategory(t_name, CATEGORY_ORG, []string{string(eid)}); err != nil {
		return nil, err
	}
	return newEvent(eid, news), nil
}

func (self *TimeVortex) DeleteEvent(eid string) error {
	self.lock()
	defer self.unlock()

	return self.timeline.Delete([]byte(eid), nil)
}

func (self *TimeVortex) UpdateCategory(t_name string, category string, eids []string) error {
	self.lock()
	defer self.unlock()

	return self.updateCategory(t_name, category, eids)

}

func (self *TimeVortex) updateCategory(t_name string, category string, eids []string) error {
	cid := make([]byte, 0, 4096)

	utime := uint64(time.Now().Unix())
	b_utime := make([]byte, 8)
	binary.LittleEndian.PutUint64(b_utime, utime)
	cid = append(cid, b_utime...)

	flg := make([]byte, 8)
	cid = append(cid, flg...)

	b_category := []byte(category)
	c_trunc := SIZE_LIMIT_CATEGORYNAME - len(b_category)
	c_dummy := make([]byte, c_trunc)
	cid = append(cid, b_category...)
	cid = append(cid, c_dummy...)

	b_t_name := []byte(t_name)
	t_trunc := SIZE_LIMIT_TOOLNAME - len(b_t_name)
	t_dummy := make([]byte, t_trunc)
	cid = append(cid, b_t_name...)
	cid = append(cid, t_dummy...)

	b_eids := make([]byte, 0, len(eids) * 16)
	for _, eid := range eids {
		b_eids = append(b_eids, []byte(eid)...)
	}

	if err := self.category.Put(cid, b_eids, nil); err != nil {
		return err
	}
	return nil
}

//func (self *TimeVortex) Find(t1, t2, opt{[]t_name, []cat}) ([][]byte, error) {}

//func (self *TimeVortex) NewEventIter()

func (self *TimeVortex) lock() {
	self.mtx.Lock()
}

func (self *TimeVortex) unlock() {
	self.mtx.Unlock()
}

func generateEvtID() []byte {
	utime := uint64(time.Now().Unix())
	uuidv2 := uint64(uuid.New().ID())
	b_utime := make([]byte, 8)
	b_uuidv2 := make([]byte, 8)
	binary.LittleEndian.PutUint64(b_utime, utime)
	binary.LittleEndian.PutUint64(b_uuidv2, uuidv2)

	evt_id := append(b_utime, b_uuidv2...)
	return evt_id
}
