package router

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
	NOTARGET_RID     uint8 = 0
	CORE_RID         uint8 = 1
	BLOADCAST_RID    uint8 = 255

	FLG_PING         uint8 = 6
	FLG_BYE          uint8 = 255
	FLG_SYN          uint8 = 1
	FLG_ACK          uint8 = 2
	FLG_SYNACK       uint8 = 3
	FLG_DATA         uint8 = 5

	TIMEOUT_LIMIT    int = 4
	BLOCKSIZE        int = 4096
)

var (
	END_OF_FRAME     string = string([]byte{0, 0, 0, 0})

	ErrClosedPort error = fmt.Errorf("closed port.")
	ErrUnconnectPort error = fmt.Errorf("unconnect port.")
)

type Router struct {
	id     uint8
	ports  map[uint8]*port
	stock  map[uint8]interface{}

	recv   chan *Frame

	ctx    context.Context
	cancel context.CancelFunc

	mtx    *sync.Mutex
	wg     *sync.WaitGroup
}

func NewRouter(bg_ctx context.Context, path string) (*Router, error) {
	r := newRouter(bg_ctx, CORE_RID)

	if err := r.CreatePort(path); err != nil {
		return nil, err
	}
	return r, nil
}

func Connect(bg_ctx context.Context, path string) (*Router, error) {
	r := newRouter(bg_ctx, NOTARGET_RID)

	if err := r.connectPort(path); err != nil {
		return nil, err
	}
	logger.PrintMsg("Connected. my id is (%v)", r.id)
	return r, nil
}

func newRouter(bg_ctx context.Context, id uint8) *Router {
	if bg_ctx == nil {
		bg_ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(bg_ctx)

	ports := make(map[uint8]*port)
	stock := make(map[uint8]interface{})
	for i := 1; i < 255; i++ {
		if id == uint8(i) {
			continue
		}
		stock[uint8(i)] = nil
	}

	return &Router{
		id:id,
		ports:ports,
		stock:stock,
		recv:make(chan *Frame),
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

func (self *Router) connectPort(path string) error {
	self.lock()
	defer self.unlock()

	c_ctx, _ := context.WithCancel(self.ctx)
	port, err := connect(c_ctx, path, self.recv)
	if err != nil {
		return err
	}
	self.id = port.LeftId()
	return self.append(port)
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
	if dst == BLOADCAST_RID {
		for _, port := range self.ports {
			go func() {
				if port.IsClosed() {
					return
				}
				port.Send(body)
			}()
		}
		return nil
	}

	port, ok := self.ports[dst]
	if !ok {
		return ErrUnconnectPort
	}
	if port.IsClosed() {
		return ErrClosedPort
	}
	return port.Send(body)
}

func (self *Router) Recv() chan *Frame {
	self.lock()
	defer self.unlock()

	return self.recv
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

					rid, err := self.requestRightId()
					if err != nil {
						logger.PrintErr("Router.listen: %s", err)
						return
					}
					port, err := linkup(c_ctx, self.id, rid, rcv.sess, self.recv)
					if err != nil {
						self.releaseRightId(rid)
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

func (self *Router) requestRightId() (uint8, error) {
	for id, _ := range self.stock {
		use_id := id
		delete(self.stock, id)
		return use_id, nil
	}
	return 0, fmt.Errorf("not in stock.")
}

func (self *Router) releaseRightId(id uint8) {
	self.stock[id] = nil
}

func (self *Router) append(port *port) error {
	if self.ports == nil {
		return fmt.Errorf("Router.append: undefined port list")
	}

	tgt := port.RightId()
	_, ok := self.ports[tgt]
	if ok {
		return fmt.Errorf("Router.append: already exsit id")
	}

	self.ports[tgt] = port
	logger.PrintMsg("Connected new client : %v", tgt)
	self.run_portAutoRemover(port)
	return nil
}

func (self *Router) run_portAutoRemover(port *port) {
	go func() {
		tgt := port.RightId()

		select {
		case <- self.ctx.Done():
			return
		case <- port.RecvClosed():
			logger.PrintMsg("Detect the client(id:%v) closed. removing the client port.", tgt)
			func() {
				self.lock()
				defer self.unlock()

				delete(self.ports, tgt)
				self.releaseRightId(tgt)
			}()
		}
	}()
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
	closed chan interface{}

	left  uint8
	right uint8

	ext_sendq chan *Frame
	sendq chan *Frame
	recvq chan *Frame

	send_lock *sync.Mutex
	ctx context.Context
	cancel context.CancelFunc
}

func newPort(ctx context.Context, con net.Conn) *port {
	p_ctx, cancel := context.WithCancel(ctx)
	self := &port{
		con:con,
		closed:make(chan interface{}),
		ext_sendq:make(chan *Frame),
		sendq:make(chan *Frame),
		recvq:make(chan *Frame),
		send_lock:new(sync.Mutex),
		ctx:p_ctx,
		cancel:cancel,
	}

	go self.run_sender()
	go self.run_recver()

	return self
}

func (self *port) SetLeftId(id uint8) {
	self.left = id
}

func (self *port) RightId() uint8 {
	return self.right
}

func (self *port) LeftId() uint8 {
	return self.left
}

func (self *port) Send(bs []byte) error {
	select {
	case <- self.ctx.Done():
		return ErrClosedPort
	case self.ext_sendq <- newFrame(self.left, self.right, FLG_DATA, bs):
	}
	return nil
}

func (self *port) connectSendPipe() {
	go func() {
		for {
			select {
			case <- self.ctx.Done():
				return
			case f := <- self.ext_sendq:
				self.send(f)
			}

		}
	}()
}

func (self *port) connectRecvPipe(b_ch chan *Frame) {
	go func() {
		for {
			select {
			case <- self.ctx.Done():
				return
			case f := <- self.recvq:
				b_ch <- f
			}

		}
	}()
}

func (self *port) send(f *Frame) {
	self.sendq <- f
}

func (self *port) recv() chan *Frame {
	return self.recvq
}

func (self *port) run_sender() {
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

			f_ping := newFrame(self.left, self.right, FLG_PING, []byte{255})
			if err := self.write(f_ping); err != nil {
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

func (self *port) write(f *Frame) error {
	self.send_lock.Lock()
	defer self.send_lock.Unlock()

	bs := f.Bytes()
	l, err := self.con.Write(bs)
	if err != nil {
		return err
	}
	if l != len(bs) {
		return fmt.Errorf("can't send Frame")
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
		for {
			buf := make([]byte, BLOCKSIZE)
			s, e := self.con.Read(buf)

			select {
			case <-self.ctx.Done():
				return
			case rcv_ch <- &rcv_data{ret:buf[:s], err:e}:
			}
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
			if rcv.err != nil {
				if rcv.err != io.EOF {
					logger.PrintErr("port.run_recver: %s", rcv.err)
					return
				}
				continue
			}

			buf = append(buf, rcv.ret...)
			if len(buf) < 1 {
				continue
			}

			frames := strings.SplitN(string(buf), END_OF_FRAME, 2)
			if len(frames) < 2 {
				continue
			}
			framebase := []byte(frames[0])
			nbuf := []byte(frames[1])
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

			go func() {
				select {
				case <- self.ctx.Done():
					return
				case self.recvq <- f:
					return
				}
			}()
		}
	}
}

func connect(ctx context.Context, path string, b_ch chan *Frame) (*port, error) {
	con, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}

	port := newPort(ctx, con)
	if err := port.recvSyn(); err != nil {
		port.close()
		return nil, err
	}
	port.sendSynAck()
	if err := port.recvAck(); err != nil {
		port.close()
		return nil, err
	}

	port.connectRecvPipe(b_ch)
	port.connectSendPipe()
	return port, nil
}

func linkup(ctx context.Context, lid uint8, rid uint8, con net.Conn, b_ch chan *Frame) (*port, error) {
	port := newPort(ctx, con)
	port.SetLeftId(lid)

	port.sendSyn(rid)
	if err := port.recvSynAck(); err != nil {
		port.close()
		return nil, err
	}
	port.sendAck()

	port.connectRecvPipe(b_ch)
	port.connectSendPipe()
	return port, nil
}

func (self *port) sendSyn(rid uint8) {
	self.right = rid
	go func() {
		body_from := []byte{byte(rid)}
		self.send(newFrame(NOTARGET_RID, NOTARGET_RID, FLG_SYN, body_from))
	}()
}

func (self *port) sendSynAck() {
	go func() {
		body_from := []byte{byte(self.left)}
		self.send(newFrame(NOTARGET_RID, NOTARGET_RID, FLG_SYNACK, body_from))
	}()
}

func (self *port) sendAck() {
	go func() {
		body_from := []byte{byte(self.left)}
		self.send(newFrame(NOTARGET_RID, NOTARGET_RID, FLG_ACK, body_from))
	}()
}

func (self *port) recvSyn() error {
	left_id, err := self.handshakeRecver(FLG_SYN)
	if err != nil {
		return fmt.Errorf("recvSyn: %v", err)
	}

	self.left = left_id
	return nil
}

func (self *port) recvSynAck() error {
	right_id, err := self.handshakeRecver(FLG_SYNACK)
	if err != nil {
		return fmt.Errorf("recvSynAck: %v", err)
	}
	if self.right != right_id {
		return fmt.Errorf("recvSynAck: can't connect to same id.")
	}
	return nil
}

func (self *port) recvAck() error {
	right_id, err := self.handshakeRecver(FLG_ACK)
	if err != nil {
		return fmt.Errorf("recvSyn: %v", err)
	}
	if self.left == right_id {
		return fmt.Errorf("recvAck: can't connect to same id.")
	}

	self.right = right_id
	return nil
}

func (self *port) handshakeRecver(flg uint8) (uint8, error) {
	timer := time.NewTimer(time.Second * time.Duration(TIMEOUT_LIMIT))
	defer timer.Stop()

	var router_id uint8
	select {
		case <- self.ctx.Done():
			return 0, fmt.Errorf("handshakeRecver: canceled.")
		case <-timer.C:
			return 0, fmt.Errorf("handshakeRecver: timeout.")
		case f := <- self.recv():
			bs := f.Body()
			if len(bs) < 1 {
				return 0, fmt.Errorf("handshakeRecver: too short return bytes.")
			}
			if f.flag != flg {
				return 0, fmt.Errorf("handshakeRecver: not expect flag.")
			}
			router_id = bs[0]
	}
	return router_id, nil
}

func (self *port) RecvClosed() chan interface{} {
	return self.recvClosed()
}

func (self *port) recvClosed() chan interface{} {
	return self.closed
}

func (self *port) IsClosed() bool {
	select {
	case <- self.recvClosed():
		return true
	default:
	}
	return false
}

func (self *port) Close() error {
	return self.close()
}

func (self *port) close() error {
	if self.IsClosed() {
		return nil
	}
	close(self.closed)

	self.cancel()

	ucon, ok := self.con.(*net.UnixConn)
	if !ok {
		return self.con.Close()
	}
	return ucon.Close()
}

type Frame struct {
	src  uint8
	dst  uint8
	flag uint8
	body []byte
}

func newFrame(src uint8, dst uint8, flg uint8, body []byte) *Frame {
	dup_body := make([]byte, len(body))
	copy(dup_body, body)

	return &Frame{src:src, dst:dst, flag:flg, body:body}
}

func bytes2frame(bs []byte) (*Frame, error) {
	if len(bs) < 4 {
		return nil, fmt.Errorf("too short frame size.")
	}
	return &Frame{src:bs[0], dst:bs[1], flag:bs[2], body:bs[3:]}, nil
}

func (self *Frame) Bytes() []byte {
	var bs []byte
	bs = append(bs, self.src)
	bs = append(bs, self.dst)
	bs = append(bs, self.flag)
	bs = append(bs, self.body...)
	return bs
}

func (self *Frame) SrcId() uint8 {
	return self.src
}

func (self *Frame) DstId() uint8 {
	return self.dst
}

func (self *Frame) Body() []byte {
	dup_body := make([]byte, len(self.body))
	copy(dup_body, self.body)
	return dup_body
}

type RouteTable struct {
	route map[uint8]map[uint8]interface{}
	mtx   *sync.Mutex
}

func NewRouteTable() *RouteTable {
	return &RouteTable{
		route: make(map[uint8]map[uint8]interface{}),
		mtx: new(sync.Mutex),
	}
}

func (self *RouteTable) Add(type_id uint8, node_id uint8) {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	t_map, ok := self.route[type_id]
	if !ok {
		t_map = make(map[uint8]interface{})
	}
	t_map[node_id] = nil
	self.route[type_id] = t_map
}

func (self *RouteTable) Remove(node_id uint8) {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	for _, t_map := range self.route {
		delete(t_map, node_id)
	}
}

func (self *RouteTable) Find(type_id uint8) ([]uint8, error) {
	self.mtx.Lock()
	defer self.mtx.Unlock()

	t_map, ok := self.route[type_id]
	if !ok {
		return nil, fmt.Errorf("undefined route")
	}

	route := make([]uint8, 0, len(t_map))
	for node_id, _ := range t_map {
		route = append(route, node_id)
	}
	return route, nil
}
