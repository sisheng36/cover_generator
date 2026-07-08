package cover

import (
	crand "crypto/rand"
	"encoding/binary"
	mrand "math/rand"
	"time"
)

func init() {
	var seed int64
	var buf [8]byte
	if _, err := crand.Read(buf[:]); err == nil {
		seed = int64(binary.LittleEndian.Uint64(buf[:]))
	} else {
		seed = time.Now().UnixNano()
	}
	mrand.Seed(seed)
}
