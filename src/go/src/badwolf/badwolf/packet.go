package badwolf

import (
	"fmt"
)

const (
	flg_SYSTEM_TYPE uint8 = 129
	flg_S_NEWS      uint8 = 1
	flg_R_NEWS      uint8 = 65
	flg_S_ANLYZ     uint8 = 2
	flg_R_ANLYZ     uint8 = 66
	flg_S_SEARCH    uint8 = 3
	flg_R_SEARCH    uint8 = 67
)

type Packet struct {
	flg  uint8
	//session_id
	body []byte
}

func CreateBytesPacket(flg uint8, body []byte) []byte {
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
