package badwolf

import (
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
	Title   string
	Link    string
	Summary string
	PubDate uint64
	Source  string
	Recorder  string
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
	id    string
	data  *News
}

func newEvent(id string, news *News) *Event {
	return &Event{id:id, data:news}
}

func (self *Event) Id() string {
	return self.id
}