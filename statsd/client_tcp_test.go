package statsd

import (
	"bufio"
	"bytes"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestTCPClient(t *testing.T) {
	l, err := newTCPListener("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	respChan := make(chan []byte, 500)
	fin := make(chan bool)
	done := make(chan bool)

	go func() {
		defer close(done)
		for {
			select {
			case <-fin:
				return
			default:
			}
			c, err := l.Accept()
			if err != nil {
				netErr, ok := err.(net.Error)
				//If this is a timeout, then continue to wait for
				//new connections
				if ok && netErr.Timeout() && netErr.Temporary() {
					continue
				}
				break
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					line, err := br.ReadBytes('\n')
					if err != nil {
						return
					}
					respChan <- line
				}
			}(c)
		}
	}()

	for _, tt := range statsdPacketTests {
		c, err := NewTCPClient(l.Addr().String(), tt.Prefix, true)
		if err != nil {
			t.Fatal(err)
		}
		method := reflect.ValueOf(c).MethodByName(tt.Method)
		e := method.Call([]reflect.Value{
			reflect.ValueOf(tt.Stat),
			reflect.ValueOf(tt.Value),
			reflect.ValueOf(tt.Rate)})[0]
		errInter := e.Interface()
		if errInter != nil {
			t.Fatal(errInter.(error))
		}

		data := <-respChan
		data = bytes.TrimRight(data, "\n")
		if bytes.Equal(data, []byte(tt.Expected)) != true {
			c.Close()
			t.Fatalf("%s got '%s' expected '%s'", tt.Method, data, tt.Expected)
		}
		c.Close()
	}
	close(fin)
	<-done
}

func newTCPListener(addr string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	l.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	return l, nil
}
