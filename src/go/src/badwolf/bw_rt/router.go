package router

import (
	"fmt"
	"sync"
	"net"
	"context"
)

const (
	DST_CLASS_CORE      uint8 = 0
	DST_CLASS_NOTICE    uint8 = 1
	DST_CLASS_ANLYZ     uint8 = 2
	DST_CLASS_BLOADCAST uint8 = 255

	FLG_SYNC         uint8 = 2
	FLG_SYNC_ACK     uint8 = 3
	FLG_ACK          uint8 = 1
	FLG_DATA         uint8 = 5
)

type Frame struct {
	dst_class uint8
	dst_node  uint32
	flag      uint8

	body      []byte
}

func newFrame(dst_class uint8, flg uint8, body []byte) *Frame {
	dup_body := make([]byte, len(body))
	copy(dup_body, body)

	return &Frame{dst_class:dst_class, flag:flag, body:body}
}

func frame(bs byte[]) (*Frame, error) {
}

func (self *Frame) Bytes() ([]byte, error) {
}

type Router struct {
	rtype  uint8
	ports []*port

	ctx    *context.Context
	cancel *context.CancelFunc

	mtx    *sync.Mutex
}

type port struct {
	con net.Conn

	dst_rtype uint8

	ctx *context.Context
}

func connectPort(ctx *context.Context, rtype uint8, path string) (*port, error) {
	con, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}

	body_from := []byte(byte(rtype))

	sf := newFrame(DST_CLASS_BLOADCAST, FLG_SYNC, body_from)
	b_sf := sf.Bytes()
	l, err := con.Write(b_sf)
	if err != nil {
		con.Close()
		return nil, err
	}
	if l != len(b_sf) {
		return nil, fmt.Errorf("cant't send frame")
	}

	tc := time.NewTicker(time.Second * 3)
	ret := []byte{}
	for {
		buf := make([]byte, 8)
		select {
		case ctx.Done():
			return nil, fmt.Errorf("context canceled")
		case <-tc.Ticker():
			return nil, fmt.Errorf("timeout")
		case size, err := sv_session.Read(buf):
			if err != nil {
				if err != io.EOF {
					return err
				}

				break
			}
			ret := append(ret, buf[:size]...)
		}
	}
	rf := frame(ret)
	if rf.flag != FLG_SYNC_ACK {
		return nil, fmt.Errorf("unmatched flag")
	}

	if len(rf.body) != 1 {
		return nil, fmt.Errorf("can't load body")
	}
	dst_rtype, ok := rf.body.(uint8)
	if !ok {
		return nil, fmt.Errorf("can't load body")
	}
	if rtype == dst_rtype {
		return nil, fmt.Errorf("can't connect saim type.")
	}

	ef := newFrame(DST_CLASS_BLOADCAST, FLG_ACK, body_from)
	b_ef := ef.Bytes()
	l, err := con.Write(b_ef)
	if err != nil {
		con.Close()
		return nil, err
	}
	if l != len(b_ef) {
		return nil, fmt.Errorf("cant't send frame")
	}
	return &port{con:con, dst_rtype:dst_rtype, ctx:ctx}
}

func newConnectedPort(ctx *context.Context, rtype uint8, con net.Conn) (*port, error) {
}

func (self *port) RType() uint8 {
	return self.dst_rtype
}

func (self *port) Close() error {
	ucon, ok := self.con.(*net.UnixConn)
	if !ok {
		return self.con.Close()
	}
	return ucon.Close()
}

func NewRouter(rtype uint8, bg_ctx *context.Context) *Router {
	if ctx == nil {
		bg_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(bg_ctx)
	ports := []*port{}

	return &Router{
		rtype:rtype,
		ports:ports,
		ctx:ctx,
		cancel:cancel,
		mtx:new(sync.Mutex),
	}
}

func (self *Router) Listen(path string) error {
	self.lock()
	defer self.unlock()

	return self.listen(path)
}

func (self *Router) Dial(path string) error {
	self.lock()
	defeer self.unlock()

	return self.dial(path)
}

func (self *Router) Send(dst uint8, chan data []byte) {
	self.lock()
	defer self.unlock()
}

func (self *Router) Get() []byte {
}

func (self *Router) appendPort(port *port) error {
	if self.ports == nil {
		return fmt.Errorf("undefined port list")
	}
	self.ports = append(self.ports, port)
	return nil
}

func (self *Router) listen(path string) error {
	sv, err := net.Listen("unix", path)
	if err != nil {
		return err
	}

	go func() {
		defer os.Remove(path)
		defer sv.Close()

		for {
			select {
			case session, err := sv.Accept():
				if err != nil {
					continue //tba
				}

				c_ctx, _ := context.WithCancel(self.ctx)
				go func(c_ctx *context.Context) {
					port, err := newConnectedPort(c_ctx, self.rtype, session)
					if err != nil {
						continue //tba
					}

					self.lock()
					defer self.unlock()

					self.appendPort(port)
				}(c_ctx)
			case self.ctx.Done():
				return
			}
		}
	}()
}

func (self *Router) dial(path string) error {
	c_ctx, _ := context.WithCancel(self.ctx)
	port, err := connectPort(c_ctx, self.rtype, path)
	if err != nil {
		return err
	}
	return self.appendPort(port)
}

func (self *Router) Close() error {
	self.lock()
	defer self.unlock()

	return self.close()
}

func (self *Router) close() error {
	for _, port := range self.ports {
		port.Close()
	}
	self.cancel()
	return nil
}

func (self *Router) lock() {
	self.mtx.Lock()
}

func (self *Router) unlock() {
	self.mtx.Unlock()
}
