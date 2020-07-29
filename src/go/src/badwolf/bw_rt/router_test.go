package router

import (
	"testing"

	"sync"
//	"time"
)

const TestSockPath = "/var/tmp/testsock"

func TestCreate(t *testing.T) {
	rt := NewRouter(nil, TYPE_CORE)
	if rt == nil {
		t.Fatal("return address is nil")
	}
	defer rt.Close()
	return
}

func TestCreatePort(t *testing.T) {
	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()

	if err := rt1.CreatePort(TestSockPath); err != nil {
		t.Fatal("can't create port : ", err)
	}
	return
}

func TestUndefinedSend(t *testing.T) {
	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()

	msg := "test"

	err := rt1.Send(TYPE_NOTICE, []byte(msg))
	if err == nil {
		t.Fatal("sended undefined port")
	}
	return
}

func TestUndefinedRecv(t *testing.T) {
	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()

	_, err := rt1.Recv()
	if err == nil {
		t.Fatal("started recv undefined port")
	}
	return
}

func TestConnectPort(t *testing.T) {
	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()
	rt2 := NewRouter(nil, TYPE_NOTICE)
	defer rt2.Close()

	if err := rt1.CreatePort(TestSockPath); err != nil {
		t.Fatal("can't create port : ", err)
	}
	if err := rt2.ConnectPort(TestSockPath); err != nil {
		t.Fatal("can't connect port : ", err)
	}
	return
}

func TestConnectUndefinedPort(t *testing.T) {
	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()

	err := rt1.ConnectPort(TestSockPath)
	if err == nil {
		t.Fatal("connected undefined port")
	}
	return
}

func TestLeft2Right(t *testing.T) {
	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()
	rt2 := NewRouter(nil, TYPE_NOTICE)
	defer rt2.Close()

	if err := rt1.CreatePort(TestSockPath); err != nil {
		t.Fatal("can't create port : ", err)
	}
	if err := rt2.ConnectPort(TestSockPath); err != nil {
		t.Fatal("can't connect port : ", err)
	}

	msg := "test"

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer wg.Done()

		ch, err := rt2.Recv()
		if err != nil {
			t.Fatal("can't recv payload :", err)
		}

		buf := <- ch
		if string(buf) != msg {
			t.Fatal("can't recv payload :", buf)
		}
	}()

	if err := rt1.Send(TYPE_NOTICE, []byte(msg)); err != nil {
		t.Fatal("can't send packet : ", err)
	}

	wg.Wait()
	return
}

func TestRight2Left(t *testing.T) {
	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()
	rt2 := NewRouter(nil, TYPE_NOTICE)
	defer rt2.Close()

	if err := rt1.CreatePort(TestSockPath); err != nil {
		t.Fatal("can't create port : ", err)
	}
	if err := rt2.ConnectPort(TestSockPath); err != nil {
		t.Fatal("can't connect port : ", err)
	}

	msg := "test"

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer wg.Done()

		ch, err := rt1.Recv()
		if err != nil {
			t.Fatal("can't recv :", err)
		}

		buf := <- ch
		if string(buf) != msg {
			t.Fatal("can't recv payload :", buf)
		}
	}()

	if err := rt2.Send(TYPE_NOTICE, []byte(msg)); err != nil {
		t.Fatal("can't send packet : ", err)
	}

	wg.Wait()
	return
}

func TestSendToUnconnectType(t *testing.T) {
	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()
	rt2 := NewRouter(nil, TYPE_NOTICE)
	defer rt2.Close()

	if err := rt1.CreatePort(TestSockPath); err != nil {
		t.Fatal("can't create port : ", err)
	}
	if err := rt2.ConnectPort(TestSockPath); err != nil {
		t.Fatal("can't connect port : ", err)
	}

	msg := "test"

	if err := rt1.Send(TYPE_ANLYZ, []byte(msg)); err == nil {
		t.Fatal("does not return error")
	}
	return
}
/*

func TestWaitKeepalive(t *testing.T) {
	TestSockPath := "testsock"

	rt1 := NewRouter(nil, TYPE_CORE)
	defer rt1.Close()
	rt2 := NewRouter(nil, TYPE_NOTICE)
	defer rt2.Close()

	if err := rt1.CreatePort(TestSockPath); err != nil {
		t.Fatal("can't create port : ", err)
	}
	if err := rt2.ConnectPort(TestSockPath); err != nil {
		t.Fatal("can't connect port : ", err)
	}

	msg := "test"

	time.Sleep(time.Second * 5)

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer wg.Done()

		ch, err := rt1.Recv()
		if err != nil {
			t.Fatal("can't recv :",err)
		}

		buf := <- ch
		if string(buf) != msg {
			t.Fatal("can't recv payload :", buf)
		}
	}()

	if err := rt2.Send(TYPE_NOTICE, []byte(msg)); err != nil {
		t.Fatal("can't send packet : ", err)
	}

	wg.Wait()
	return
}
*/
