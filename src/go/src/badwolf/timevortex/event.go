package timevortex

import (
	"time"
	"bytes"
)

import (
	"github.com/ugorji/go/codec"
)

var (
	MSGPACK_HANDLE *codec.MsgpackHandle = &codec.MsgpackHandle{}
)

func init() {
	MSGPACK_HANDLE.RawToString = true
}

type News struct {
	Title    string
	Link     string
	Summary  string
	PubDate  time.Time
	Source   string
	Recorder string
}

func Bytes2News(bs []byte) (*News, error) {
	buf := make([]byte, len(bs))
	copy(buf, bs)
	r := bytes.NewReader(buf)

	var news *News
	if err := codec.NewDecoder(r, MSGPACK_HANDLE).Decode(&news); err != nil {
		return nil, err
	}
	return news, nil
}

func (self *News) Bytes() ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := codec.NewEncoder(buf, MSGPACK_HANDLE).Encode(self); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type Event struct {
	id    []byte
	data  *News
}

func newEvent(id []byte, news *News) *Event {
	cp_id := make([]byte, 16)
	copy(cp_id, id)
	return &Event{id:cp_id, data:news}
}

func (self *Event) Title() string {
	return self.data.Title
}

func (self *Event) Link() string {
	return self.data.Link
}

func (self *Event) Summary() string {
	return self.data.Summary
}

func (self *Event) Recorder() string {
	return self.data.Recorder
}

func (self *Event) Source() string {
	return self.data.Source
}

func (self *Event) Time() time.Time {
	return self.data.PubDate
}

func (self *Event) Id() []byte {
	cp_id := make([]byte, 16)
	copy(cp_id, self.id)
	return cp_id
}
