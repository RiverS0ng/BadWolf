package router

import (
	"testing"

	"sync"
	"time"
)

const (
	TestSockPath string = "/var/tmp/testsock"
	Bloadcast uint8 = BLOADCAST_RID
)

func TestCreatePort(t *testing.T) {
	rt1, err := NewRouter(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt1.Close()

	return
}

func TestUndefinedSend(t *testing.T) {
	rt1, err := NewRouter(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt1.Close()

	var unknown_target uint8 = 111
	if err := rt1.Send(unknown_target, []byte("test message")); err == nil {
		t.Fatal("sended undefined port")
	}
	return
}

func TestConnectPort(t *testing.T) {
	rt1, err := NewRouter(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt1.Close()

	rt2, err := Connect(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt2.Close()
	return
}

func TestConnectUndefinedPort(t *testing.T) {
	rt1, err := Connect(nil, TestSockPath)
	if err == nil {
		defer rt1.Close()
		t.Fatal("connected undefined port")
	}
	return
}

func TestLeft2Right(t *testing.T) {
	rt1, err := NewRouter(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt1.Close()

	rt2, err := Connect(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt2.Close()

	msg := "test"

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer wg.Done()

		ch := rt2.Recv()
		f := <- ch
		if string(f.Body()) != msg {
			t.Fatal("can't recv payload :", f.Body())
		}
	}()

	if err := rt1.Send(Bloadcast, []byte(msg)); err != nil {
		t.Fatal("can't send packet : ", err)
	}

	wg.Wait()
	return
}

func TestRight2Left(t *testing.T) {
	rt1, err := NewRouter(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt1.Close()

	rt2, err := Connect(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt2.Close()

	msg := "test"

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer wg.Done()

		ch := rt1.Recv()
		f := <- ch
		if string(f.Body()) != msg {
			t.Fatal("can't recv payload :", f.Body())
		}
	}()

	if err := rt2.Send(Bloadcast, []byte(msg)); err != nil {
		t.Fatal("can't send packet : ", err)
	}

	wg.Wait()
	return
}

func TestSendToUnconnectType(t *testing.T) {
	rt1, err := NewRouter(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt1.Close()

	rt2, err := Connect(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt2.Close()

	msg := "test"
	var unkown_id uint8 = 10

	if err := rt1.Send(unkown_id, []byte(msg)); err == nil {
		t.Fatal("does not return error")
	}
	return
}

func TestWaitKeepalive(t *testing.T) {
	rt1, err := NewRouter(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt1.Close()

	rt2, err := Connect(nil, TestSockPath)
	if err != nil {
		t.Fatal("can't create port : ", err)
	}
	defer rt2.Close()

	msg := "test"

	time.Sleep(time.Second * 5)

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer wg.Done()

		ch := rt2.Recv()
		f := <- ch
		if string(f.Body()) != msg {
			t.Fatal("can't recv payload :", f.Body())
		}
	}()

	if err := rt1.Send(Bloadcast, []byte(msg)); err != nil {
		t.Fatal("can't send packet : ", err)
	}

	wg.Wait()
	return
}
