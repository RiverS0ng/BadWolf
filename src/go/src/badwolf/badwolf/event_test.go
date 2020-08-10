package badwolf

import (
	"testing"
)

func TestEvent2Byte(t *testing.T) {
	n := testDummyNews()
	b, err := n.Bytes()
	if err != nil {
		t.Fatal("Failed convert from struct. :", err)
	}
	if len(b) < 1 {
		t.Fatal("Haven't data.")
	}

	n2, err := Bytes2News(b)
	if err != nil {
		t.Fatal("Failed convert from bytes. :", err)
	}

	if n.Title != n2.Title {
		t.Fatal("Does not match title.")
	}
	if n.Link != n2.Link {
		t.Fatal("Does not match link.")
	}
	if n.Summary != n2.Summary {
		t.Fatal("Does not match summary.")
	}
	if n.PubDate != n2.PubDate {
		t.Fatal("Does not match publish date.")
	}
	if n.Source != n2.Source {
		t.Fatal("Does not match source.")
	}
	if n.Recorder != n2.Recorder {
		t.Fatal("Does not match recorder.")
	}
}

func testDummyNews() *News {
	return &News{
		Title:"Dummy news",
		Link:"https://www.dummy.example.com",
		Summary: "This is a dummy news.",
		PubDate: 0,
		Source: "The name of RSS Channel.",
		Recorder: "The name of bw_getter",
	}
}
