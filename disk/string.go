package disk

const (
	padByte = 0xA0 // space in unshifted PETSCII
)

// TODO: some fancy PETSCII encoding/decoding?

func PadString(str string, n int) []byte {
	if len(str) > n {
		panic("overflow")
	}
	buf := make([]byte, n)
	for i := copy(buf, str); i < n; i++ {
		buf[i] = padByte
	}
	return buf
}

func UnpadBytes(buf []byte) string {
	for i := len(buf) ; i > 0; i-- {
		if buf[i-1] != padByte {
			return string(buf[:i])
		}
	}
	return ""
}
