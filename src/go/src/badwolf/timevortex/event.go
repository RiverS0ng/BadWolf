package timevortex

import (
	"fmt"
	"hash/fnv"
	"time"
	"bytes"
	"encoding/binary"
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

func (self *News) Id() ([]byte) {
	utime := uint64(self.PubDate.Unix())
	h := fnv.New64a()
	h.Write([]byte(self.Title + self.Recorder + self.Summary))
	fnv64 := h.Sum64()

	b_utime := make([]byte, 8)
	b_fnv64 := make([]byte, 8)
	binary.BigEndian.PutUint64(b_utime, utime)
	binary.BigEndian.PutUint64(b_fnv64, fnv64)

	id := append(b_utime, b_fnv64...)
	return id
}

func tExtractNewsId(id []byte) (time.Time, error) {
	if len(id) < 16 {
		return time.Time{}, fmt.Errorf("input value too short.")
	}
	t_base := binary.BigEndian.Uint64(id[0:8])
	return time.Unix(int64(t_base), 0), nil
}
