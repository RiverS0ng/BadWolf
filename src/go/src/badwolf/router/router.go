package router
//MEMO packetとしての扱いが必要かも。どうしても送信元や命令を識別できないと処理が進まない

import (
	"io"
	"os"
	"fmt"
	"net"
	"sync"
	"time"
	"strings"
	"context"
)

import (
	"badwolf/logger"
)

const (
	FLG_PING         uint8 = 0
	FLG_BYE          uint8 = 255
	FLG_SYNC         uint8 = 2
	FLG_DATA         uint8 = 5

	TIMEOUT_LIMIT    int = 4
	BLOCKSIZE        int = 4096
	END_OF_FRAME     string = "\n\n"
)

type Router struct {
	id     uint8
	ports  map[uint8]*port

	recv   chan []byte

	ctx    context.Context
	cancel context.CancelFunc

	mtx    *sync.Mutex
	wg     *sync.WaitGroup
}

func NewRouter(bg_ctx context.Context, id uint8, path string) (*Router, error) {
	r := newRouter(bg_ctx, id)

	if err := r.CreatePort(path); err != nil {
		return nil, err
	}
	return r, nil
}

func Connect(bg_ctx context.Context, id uint8, path string) (*Router, error) {
	r := newRouter(bg_ctx, id)

	if err := r.ConnectPort(path); err != nil {
		return nil, err
	}
	return r, nil
}

func newRouter(bg_ctx context.Context, id uint8) *Router {
	if bg_ctx == nil {
		bg_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(bg_ctx)

	ports := make(map[uint8]*port)

	return &Router{
		id:id,
		ports:ports,
		recv:make(chan []byte),
		ctx:ctx,
		cancel:cancel,
		mtx:new(sync.Mutex),
		wg:new(sync.WaitGroup),
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

func (self *Router) Send(dstid uint8, body []byte) error {
	self.lock()
	defer self.unlock()

	return self.send(dstid, body)
}

func (self *Router) send(dst uint8, body []byte) error {
	if dst == self.id {
		return fmt.Errorf("can't send to same id.")
	}

	port, ok := self.ports[dst]
	if !ok {
		return fmt.Errorf("undefined port table.")
	}

	return port.Send(body)
}

func (self *Router) Recv() (chan []byte, error) {
	self.lock()
	defer self.unlock()

	if !self.havePort() {
		return self.recv, fmt.Errorf("not connect port")
	}
	return self.recv, nil
}

func (self *Router) havePort() bool {
	if len(self.ports) < 1 {
		return false
	}
	return true
}

func (self *Router) listen(path string) error {
	sv, err := net.Listen("unix", path)
	if err != nil {
		return err
	}

	self.wg.Add(1)
	go func() {
		defer self.wg.Done()
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
			case <- self.ctx.Done():
				return
			case rcv := <-rcv_ch:
				if rcv.err != nil {
					logger.PrintErr("Router.listen: %s", rcv.err)
					continue
				}

				c_ctx, _ := context.WithCancel(self.ctx)
				go func(c_ctx context.Context) {
					self.lock()
					defer self.unlock()

					port, err := linkup(c_ctx, self.id, rcv.sess, self.recv)
					if err != nil {
						logger.PrintErr("Router.listen: %s", err)
						return
					}

					self.append(port)
				}(c_ctx)
			}
		}
	}()

	return nil
}

func (self *Router) dial(path string) error {
	c_ctx, _ := context.WithCancel(self.ctx)
	port, err := connect(c_ctx, self.id, path, self.recv)
	if err != nil {
		return err
	}
	return self.append(port)
}

func (self *Router) append(port *port) error {
	if self.ports == nil {
		return fmt.Errorf("undefined port list")
	}

	tgt := port.RightId()
	_, ok := self.ports[tgt]
	if ok {
		return fmt.Errorf("already exsit id")
	}

	self.ports[tgt] = port
	return nil
}

func (self *Router) close() error {
	defer self.wg.Wait()
	self.cancel()

	for _, port := range self.ports {
		port.Close()
	}
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

	send_lock *sync.Mutex
	ctx context.Context
}

func newPort(ctx context.Context, con net.Conn, left uint8) *port {
	self := &port{
		left:left,
		con:con,
		sendq:make(chan *frame),
		recvq:make(chan *frame),
		send_lock:new(sync.Mutex),
		ctx:ctx,
	}

	go self.run_sender()
	go self.run_recver()

	return self
}

func (self *port) RightId() uint8 {
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
			case <- self.ctx.Done():
				return
			case f := <- self.recvq:
				b_ch <- f.Body()
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
	self.send_lock.Lock()
	defer self.send_lock.Unlock()

	bs := f.Bytes()
	l, err := self.con.Write(bs)
	if err != nil {
		return err
	}
	if l != len(bs) {
		return fmt.Errorf("can't send frame")
	}
	self.con.Write([]byte(END_OF_FRAME))

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

	buf := []byte{}
	for {
		select {
		case <-self.ctx.Done():
			return
		case <-timer.C:
			return
		case rcv := <- rcv_ch:
			buf = append(buf, rcv.ret...)

			if rcv.err != nil {
				if rcv.err != io.EOF {
					logger.PrintErr("port.run_recver: %s", rcv.err)
					return
				}
				continue
			}
			if len(buf) < 1 {
				continue
			}

			packets := strings.SplitN(string(buf), END_OF_FRAME, 2)
			if len(packets) < 2 {
				continue
			}
			framebase := []byte(packets[0])
			nbuf := []byte(packets[1])
			buf = nbuf

			f, err := bytes2frame(framebase)
			if err != nil {
				logger.PrintErr("port.run_recver: %s", err)
				continue
			}

			timer.Stop()
			timer.Reset(t_range)

			if f.flag == FLG_PING {
				continue
			}
			go func() {self.recvq <- f}()
		}
	}
}

func (self *port) getRightId() error {
	var right_id uint8

	timer := time.NewTimer(time.Second * time.Duration(TIMEOUT_LIMIT))
	defer timer.Stop()

	select {
		case <- self.ctx.Done():
			return fmt.Errorf("canceled.")
		case <-timer.C:
			return fmt.Errorf("timeout.")
		case f := <- self.recv():
			bs := f.Body()
			if len(bs) < 1 {
				return fmt.Errorf("too short return bytes.")
			}
			if f.flag != FLG_SYNC {
				return fmt.Errorf("not expect flag.")
			}
			right_id = bs[0]
	}
	if self.left == right_id {
		return fmt.Errorf("can't connect to same id.")
	}

	self.right = right_id
	return nil
}

func connect(ctx context.Context, id uint8, path string, b_ch chan []byte) (*port, error) {
	con, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return linkup(ctx, id, con, b_ch)
}

func linkup(ctx context.Context, id uint8, con net.Conn, b_ch chan []byte) (*port, error) {
	port := newPort(ctx, con, id)

	go func() {
		body_from := []byte{byte(id)}
		port.send(newFrame(FLG_SYNC, body_from))
	}()

	if err := port.getRightId(); err != nil {
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
