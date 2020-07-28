package router

import (
	"io"
	"os"
	"fmt"
	"net"
	"sync"
	"time"
	"context"
)

import (
	"badwolf/logger"
)

const (
	TYPE_CORE      uint8 = 0
	TYPE_NOTICE    uint8 = 1
	TYPE_ANLYZ     uint8 = 2
	TYPE_BLOADCAST uint8 = 255

	FLG_PING         uint8 = 0
	FLG_SYNC         uint8 = 2
	FLG_DATA         uint8 = 5

	TIMEOUT_LIMIT    int = 4
	BLOCKSIZE        int = 4096
)

type Router struct {
	rtype  uint8
	plists  map[uint8][]*port

	recv    chan []byte

	ctx    context.Context
	cancel context.CancelFunc

	mtx    *sync.Mutex
}

func NewRouter(bg_ctx context.Context, rtype uint8) *Router {
	if bg_ctx == nil {
		bg_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(bg_ctx)

	plists := make(map[uint8][]*port)
	plists[TYPE_CORE] = []*port{}
	plists[TYPE_BLOADCAST] = []*port{}
	plists[TYPE_ANLYZ] = []*port{}
	plists[TYPE_NOTICE] = []*port{}

	return &Router{
		rtype:rtype,
		plists:plists,
		recv:make(chan []byte),
		ctx:ctx,
		cancel:cancel,
		mtx:new(sync.Mutex),
	}
}

func (self *Router) Close() error {
	self.lock()
	defer self.unlock()

	return self.close()
}

func (self *Router) ConnectPort(path string) error {
	self.lock()
	defer self.unlock()

	return self.dial(path)
}

func (self *Router) CreatePort(path string) error {
	self.lock()
	defer self.unlock()

	return self.listen(path)
}

func (self *Router) Send(dst uint8, body []byte) error {
	self.lock()
	defer self.unlock()

	return self.send(dst, body)
}

func (self *Router) send(dst uint8, body []byte) error {
	if dst == self.rtype {
		return fmt.Errorf("can't send to same type.")
	}

	plist, ok := self.plists[dst]
	if !ok {
		return fmt.Errorf("unkown route.")
	}

	for _, port := range plist {
		go port.Send(body)
	}
	return nil
}

func (self *Router) Recv() (chan []byte, error) {
	self.lock()
	defer self.unlock()

	if self.havePort() {
		return self.recv, fmt.Errorf("not connect port")
	}
	return self.recv, nil
}

func (self *Router) havePort() bool {
	for _, plist := range self.plists {
		if len(plist) > 0 {
			return true
		}
	}
	return false
}

func (self *Router) listen(path string) error {
	sv, err := net.Listen("unix", path)
	if err != nil {
		return err
	}

	go func() {
		defer os.Remove(path)
		defer sv.Close()

		type rcv_sess struct {
			sess net.Conn
			err  error
		}
		rcv_ch := make(chan *rcv_sess)
		go func() {
			for {
				sess, err := sv.Accept()
				rcv_ch <- &rcv_sess{sess:sess, err:err}
			}
		}()

		for {
			select {
			case rcv := <-rcv_ch:
				if rcv.err != nil {
					logger.PrintErr("Router.listen: %s", rcv.err)
					continue
				}

				c_ctx, _ := context.WithCancel(self.ctx)
				go func(c_ctx context.Context) {
					port, err := linkup(c_ctx, self.rtype, rcv.sess, self.recv)

					if err != nil {
						logger.PrintErr("Router.listen: %s", err)
						return
					}

					self.append(port)
				}(c_ctx)
			case <- self.ctx.Done():
				return
			}
		}
	}()

	return nil
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
	if self.plists == nil {
		return fmt.Errorf("undefined port list")
	}

	tgt := port.RightType()
	plist, ok := self.plists[tgt]
	if !ok {
		return fmt.Errorf("undefined port list")
	}

	plist = append(plist, port)
	self.plists[tgt] = plist
	return nil
}

func (self *Router) close() error {
	for _, plist := range self.plists {
		for _, port := range plist {
			port.Close()
		}
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

	left  uint8
	right uint8

	sendq chan *frame
	recvq chan *frame
	ctx context.Context
}

func newPort(ctx context.Context, con net.Conn, left uint8) *port {
	self := &port{
		left:left,
		con:con,
		sendq:make(chan *frame),
		recvq:make(chan *frame),
		ctx:ctx,
	}

	go self.run_recver()
	go self.run_sender()

	return self
}

func (self *port) RightType() uint8 {
	return self.right
}

func (self *port) Send(bs []byte) error {
	self.send(newFrame(FLG_DATA, bs))
	return nil
}

func (self *port) connectRecvPipe(b_ch chan []byte) {
	go func() {
		for {
			select {
			case f := <- self.recvq:
				b_ch <- f.Body()
			case <- self.ctx.Done():
				return
			}
		}
	}()
}

func (self *port) send(f *frame) {
	self.sendq <- f
}

func (self *port) recv() chan *frame {
	return self.recvq
}

func (self *port) run_sender() {
	f_ping := newFrame(FLG_PING, []byte{0})

	t_range := time.Second * time.Duration(TIMEOUT_LIMIT - 1)
	timer := time.NewTimer(t_range)
	defer timer.Stop()

	for {
		select {
		case <- self.ctx.Done():
			return
		case <-timer.C:
			timer.Stop()
			timer.Reset(t_range)

			if err := self.write(f_ping); err != nil {
				logger.PrintErr("port.run_sender: %s", err)
				self.close()
				return
			}
		case f := <- self.sendq:
			timer.Stop()
			timer.Reset(t_range)

			if err := self.write(f); err != nil {
				logger.PrintErr("port.run_sender: %s", err)
			}
			continue
		}
	}
}

func (self *port) write(f *frame) error {
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

func (self *port) run_recver() {
	defer self.close()

	t_range := time.Second * time.Duration(TIMEOUT_LIMIT)
	timer := time.NewTimer(t_range)
	defer timer.Stop()

	type rcv_data struct {
		ret []byte
		err error
	}
	rcv_ch := make(chan *rcv_data)
	go func() {
		buf := make([]byte, BLOCKSIZE)

		for {
			s, e := self.con.Read(buf)
			rcv_ch <- &rcv_data{ret:buf[:s], err:e}
		}
	}()

	ret := []byte{}
	for {
		select {
		case <-self.ctx.Done():
			return
		case <-timer.C:
			return
		case rcv := <- rcv_ch:
			if rcv.err != nil {
				if rcv.err != io.EOF {
					logger.PrintErr("port.run_recver: %s", rcv.err)
					continue
				}

				f, err := bytes2frame(ret)
				if err != nil {
					logger.PrintErr("port.run_recver: %s", err)
					ret = []byte{}
					continue
				}

				timer.Stop()
				timer.Reset(t_range)
				ret = []byte{}

				if f.flag == FLG_PING {
					continue
				}
				go func() {self.recvq <- f}()
			}
			ret = append(ret, rcv.ret...)
		}
	}
}

func (self *port) getRightType() error {
	var right_rtype uint8

	timer := time.NewTimer(time.Second * time.Duration(TIMEOUT_LIMIT))
	defer timer.Stop()

	select {
		case <- self.ctx.Done():
			return fmt.Errorf("canceled.")
		case <-timer.C:
			return fmt.Errorf("timeout.")
		case f := <- self.recv():
			bs := f.Bytes()
			if len(bs) < 1 {
				return fmt.Errorf("too short return bytes.")
			}
			if f.flag != FLG_SYNC {
				return fmt.Errorf("not expect flag.")
			}
			right_rtype = bs[0]
	}
	if self.left == right_rtype {
		return fmt.Errorf("can't connect type.")
	}

	self.right = right_rtype
	return nil
}

func connect(ctx context.Context, rtype uint8, path string, b_ch chan []byte) (*port, error) {
	con, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return linkup(ctx, rtype, con, b_ch)
}

func linkup(ctx context.Context, rtype uint8, con net.Conn, b_ch chan []byte) (*port, error) {
	port := newPort(ctx, con, rtype)

	go func() {
		body_from := []byte{byte(rtype)}
		port.send(newFrame(FLG_SYNC, body_from))
	}()

	if err := port.getRightType(); err != nil {
		port.close()
		return nil, err
	}

	port.connectRecvPipe(b_ch)
	return port, nil
}

func (self *port) Close() error {
	return self.close()
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

	return &frame{flag:flg, body:body}
}

func bytes2frame(bs []byte) (*frame, error) {
	if len(bs) < 2 {
		return nil, fmt.Errorf("too short frame size.")
	}
	return &frame{flag:bs[0], body:bs[1:]}, nil
}

func (self *frame) Bytes() []byte {
	var bs []byte
	bs = append(bs, self.flag)
	bs = append(bs, self.body...)
	return bs
}

func (self *frame) Body() []byte {
	dup_body := make([]byte, len(self.body))
	copy(dup_body, self.body)
	return dup_body
}
