package packet

import (
	"fmt"
)

const (
	F_S_NEW_NEWS  uint8 = 1
	F_R_NEW_NEWS  uint8 = 129
//	F_S_ANLYZ     uint8 = 2
//	F_R_ANLYZ     uint8 = 130
//	F_S_SEARCH    uint8 = 3
//	F_R_SEARCH    uint8 = 131
//	F_S_REANLYZ   uint8 = 4
//	F_R_REANLYZ   uint8 = 132
)

type Packet struct {
	flg  uint8
	//session_id
	body []byte

}

func CreateBytes(flg uint8, body []byte) []byte {
	packet := []byte{flg}
	packet = append(packet, body...)
	return packet
}

func Bytes2Packet(b []byte) (*Packet, error) {
	if len(b) < 2 {
		return nil, fmt.Errorf("can't convert packet")
	}
	return &Packet{flg:b[0], body:b[1:]}, nil
}

func (self *Packet) Body() []byte {
	return self.body
}

func (self *Packet) Flg() uint8 {
	return self.flg
}
