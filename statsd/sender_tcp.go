package statsd

import (
	"bufio"
	"net"
	"sync"
)

// TCPSender provides a socket send interface.
type TCPSender struct {
	// underlying connection
	c net.Conn
	// resolved udp address
	ra     *net.TCPAddr
	writer *bufio.Writer
	mx     sync.Mutex
}

// Send sends the data to the server endpoint.
func (s *TCPSender) Send(data []byte) (int, error) {
	s.mx.Lock()
	defer s.mx.Unlock()

	n, err := s.writer.Write(data)
	if err != nil {
		return n, err
	}
	if data[len(data)-1] != '\n' {
		err = s.writer.WriteByte('\n')
		if err != nil {
			return 0, err
		}
	}

	err = s.writer.Flush()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Closes TCPSender
func (s *TCPSender) Close() error {
	s.writer.Flush()
	s.writer.Reset(s.c)
	err := s.c.Close()
	return err
}

// Returns a new TCPSender for sending to the supplied addresss.
//
// addr is a string of the format "hostname:port", and must be parsable by
// net.ResolveTCPAddr.
func NewTCPSender(addr string, disableNagle bool) (Sender, error) {
	ra, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}

	c, err := net.DialTCP(addr, nil, ra)
	if err != nil {
		return nil, err
	}

	err = c.SetKeepAlive(true)
	if err != nil {
		c.Close()
		return nil, err
	}

	err = c.SetNoDelay(!disableNagle)
	if err != nil {
		c.Close()
		return nil, err
	}

	sender := &TCPSender{
		c:      c,
		ra:     ra,
		writer: bufio.NewWriter(c),
	}

	return sender, nil
}
