package badwolf

import (
	"testing"
)

const (
	TestPath = "/var/tmp/mytest.tv"
	TestToolName = "mytoool"
)

func TestCreate(t *testing.T) {
	if err := CreateTimeVortex(TestPath); err != nil {
		t.Fatal("Create Failed. :", err)
	}
}

func TestExistCreate(t *testing.T) {
	if err := CreateTimeVortex(TestPath); err == nil {
		t.Fatal("Create Successed.")
	}
	return
}

func TestOpen(t *testing.T) {
	tv, err := OpenTimeVortex(nil, TestPath)
	if err != nil {
		t.Fatal("Open Failed. :", err)
	}
	tv.Close()
}

func TestAddData(t *testing.T) {
	tv, err := OpenTimeVortex(nil, TestPath)
	if err != nil {
		t.Fatal("Open Failed. :", err)
	}
	defer tv.Close()

	ns := testDummyNews()
	if _, err := tv.AddNewEvent(TestToolName, ns); err != nil {
		t.Fatal("Failed add news. :", err)
	}
}

func TestDelete(t *testing.T) {
	if err := DeleteTimeVortex(TestPath); err != nil {
		t.Fatal("Delete Failed. :", err)
	}
}

func TestNotExistOpen(t *testing.T) {
	tv, err := OpenTimeVortex(nil, TestPath)
	if err == nil {
		tv.Close()
		t.Fatal("Open Successed.")
	}
	return
}

