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

	FLG_PING         uint8 = 0
	FLG_SYNC         uint8 = 2
	FLG_SYNC_ACK     uint8 = 3
	FLG_ACK          uint8 = 1
	FLG_DATA         uint8 = 5
)

type Router struct {
	rtype  uint8
	ports  map[uint8][]*port

	recv    chan []byte

	ctx    *context.Context
	cancel *context.CancelFunc

	mtx    *sync.Mutex
}

func NewRouter(ctx *context.Context, rtype uint8) *Router {
	if ctx == nil {
		bg_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(bg_ctx)
	ports := []*port{}

	return &Router{
		rtype:rtype,
		ports:make(map[uint8][]*port),
		recv:make(chan []byte),
		ctx:ctx,
		cancel:cancel,
		mtx:new(sync.Mutex),
	}
}

func (self *Router) ConnectPort(path string) error {
	self.lock()
	defer self.unlock()

	self.dial(path)
}

func (self *Router) CreatePort(path string) error {
	self.lock()
	defer self.unlock()

	self.listen(path)
}

func (self *Router) Send(dst uint8, body []byte) error {
}

func (self *Router) Recv() chan []byte {
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
					port, err := linkup(c_ctx, self.rtype, session, self.recv)
					if err != nil {
						continue //tba
					}

					self.append(port)
				}(c_ctx)
			case self.ctx.Done():
				return
			}
		}
	}()
}

func (self *Router) dial(path string) error {
	c_ctx, _ := context.WithCancel(self.ctx)
	port, err := connect(c_ctx, self.rtype, path, self.recv)
	if err != nil {
		return err
	}
	return self.append(port)
}

func (self *Router) append(port *port) error {
	if self.ports == nil {
		return fmt.Errorf("undefined port list")
	}
	self.ports = append(self.ports, port)
	return nil
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

type port struct {
	con net.Conn

	send chan *frame
	recv chan *frame
	ctx *context.Context
}

func newPort(ctx *context.Context, con net.Conn) *port {
	self := &port{
		con:con,
		send:make(chan *frame),
		recv:make(chan *frame),
		ctx:ctx,
	}

	go self.run_recver()
	go self.run_sender()

	return self
}

func (self *port) Send(bs []byte) error {
	return self.send(newFrame(FLG_DATA, bs))
}

func (self *port) RecvPipeline(b_ch chan []byte) {
	go func() {
		for {
			select {
			case f := <- self.recv:
				b_ch <- f.Bytes()
			case self.ctx.Done():
				return
			}
		}
	}()
}

func (self *port) send(f *frame) error {
	bs := f.Bytes()
	l, err := self.con.Write(bs)
	if err != nil {
		return err
	}
	if l != len(bs) {
		return fmt.Errorf("can't send frame")
	}
	return nil
}

func (self *port) recv() chan *frame {
	return self.recv
}

func (self *port) run_sender() {
	f_ping := newFrame(FLG_PING, []byte{0})

	tc := time.NewTicker(time.Second * 3)
	for {
		select {
		case self.ctx.Done():
			return
		case <-tc.C:
			self.send(f_ping)
			continue
		}
	}
}

func (self *port) run_recver() {
	defer self.close()

	tc := time.NewTicker(time.Second * 4)
	ret := []byte{}
	for {
		buf := make([]byte, 4096)
		select {
		case ctx.Done():
			return
		case <-tc.C:
			return
		case size, err := self.con.Read(buf):
			if err != nil {
				if err != io.EOF {
					return err
				}

				f :=  frame(ret)
				ret = []byte{}

				if f.flag == FLG_PING {
					tc.Reset()
					continue
				}
				go func() {self.recv <- f}()
			}
			ret := append(ret, buf[:size]...)
		}
	}
}

func connect(ctx *context.Context, rtype uint8, path string, b_ch chan []byte) (uint8, *port, error) {
	con, err := net.Dial("unix", path)
	if err != nil {
		return 0, nil, err
	}
	port := newPort(ctx, con)

	body_from := []byte(byte(rtype))

	var dst_rtype uint8
	if err := port.send(newFrame(FLG_SYNC, body_from)); err != nil {
		port.close()
		return 0, nil, err
	}
	select {
		case ctx.Done():
			port.close()
			return
		case f := <- port.recv():
			bs := f.Bytes()
			if len(bs) < 1 {
				port.close()
				return 0, nil, fmt.Errorf("too short")
			}
			if f.flag != FLG_SYNC_ACK {
				port.close()
				return 0, nil, fmt.Errorf("not expect flag")
			}
			dst_rtype = bs[0]
	}
	if rtype == dst_rtype {
		port.close()
		return 0, nil, fmt.Errorf("can't connect type")
	}
	if err := port.send(newFrame(FLG_ACK, body_from)); err != nil {
		port.close()
		return 0, nil, err
	}

	port.RecvPipeline(b_ch)
	return dst_rtype, port, nil
}

func linkup(ctx *context.Context, rtype uint8, con net.Conn, b_ch chan []byte) (uint8, *port, error) {
	port := newPort(ctx, con)

	body_from := []byte(byte(rtype))//tba
	con.Read(buf)//tba
	con.Write(buf)//tba
}

func (self *port) close() error {
	ucon, ok := self.con.(*net.UnixConn)
	if !ok {
		return self.con.Close()
	}
	return ucon.Close()
}

type frame struct {
	flag      uint8
	body      []byte
}

func newFrame(flg uint8, body []byte) *frame {
	dup_body := make([]byte, len(body))
	copy(dup_body, body)

	return &frame{flag:flag, body:body}
}

func frame(bs byte[]) (*frame, error) {
	if len(bs) < 2 {
		return nil, fmt.Errorf("too short frame size.")
	}
	return &frame{flag:bs[0], body:bs[1:]}
}

func (self *frame) Bytes() []byte {
	var bs []byte
	bs = append(bs, self.flag)
	bs = append(bs, self.body...)
	return bs
}
