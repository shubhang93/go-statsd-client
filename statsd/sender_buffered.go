// Copyright (c) 2012-2016 Eli Janssen
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package statsd

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"
)

var senderPool = newBufferPool()

// BufferedSender provides a buffered statsd udp, sending multiple
// metrics, where possible.
type BufferedSender struct {
	sender        Sender
	flushBytes    int
	flushInterval time.Duration
	// buffers
	bufmx  sync.Mutex
	buffer *bytes.Buffer
	bufs   chan *bytes.Buffer
	// lifecycle
	runmx   sync.RWMutex
	cancel  context.CancelFunc
	errchan chan error
	running bool
}

// Send bytes.
func (s *BufferedSender) Send(data []byte) (int, error) {
	s.runmx.RLock()
	if !s.running {
		s.runmx.RUnlock()
		return 0, fmt.Errorf("BufferedSender is not running")
	}

	s.withBufferLock(func() {
		blen := s.buffer.Len()
		if blen > 0 && blen+len(data)+1 >= s.flushBytes {
			s.swapnqueue()
		}

		s.buffer.Write(data)
		s.buffer.WriteByte('\n')

		if s.buffer.Len() >= s.flushBytes {
			s.swapnqueue()
		}
	})
	s.runmx.RUnlock()
	return len(data), nil
}

// Close closes the Buffered Sender and cleans up.
func (s *BufferedSender) Close() error {
	// since we are running, write lock during cleanup
	s.runmx.Lock()
	defer s.runmx.Unlock()
	if !s.running {
		return nil
	}

	s.cancel()
	s.running = false
	return <-s.errchan
}

// Start Buffered Sender
// Begins ticker and read loop
func (s *BufferedSender) Start() {
	// write lock to start running
	s.runmx.Lock()
	defer s.runmx.Unlock()
	if s.running {
		return
	}

	s.running = true
	s.bufs = make(chan *bytes.Buffer, 32)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.run(ctx)
}

func (s *BufferedSender) withBufferLock(fn func()) {
	s.bufmx.Lock()
	fn()
	s.bufmx.Unlock()
}

func (s *BufferedSender) swapnqueue() {
	if s.buffer.Len() == 0 {
		return
	}
	ob := s.buffer
	nb := senderPool.Get()
	s.buffer = nb
	s.bufs <- ob
}

func (s *BufferedSender) run(ctx context.Context) {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	// ctx to wait until buffer empties
	flushCtx, flushDone := context.WithCancel(context.Background())
	go func() {
		for buf := range s.bufs {
			s.flush(buf)
			senderPool.Put(buf)
		}
		flushDone()
	}()

	for {
		select {
		case <-ctx.Done():
			s.withBufferLock(func() {
				s.swapnqueue()
			})
			close(s.bufs)
			// wait for buffer to empty
			<-flushCtx.Done()
			// propagate any subsender closes
			s.errchan <- s.sender.Close()
			return
		case <-ticker.C:
			s.withBufferLock(func() {
				s.swapnqueue()
			})
		}
	}
}

// send to remove endpoint and truncate buffer
func (s *BufferedSender) flush(b *bytes.Buffer) (int, error) {
	bb := b.Bytes()
	bbl := len(bb)
	if bb[bbl-1] == '\n' {
		bb = bb[:bbl-1]
	}
	//n, err := s.sender.Send(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
	n, err := s.sender.Send(bb)
	b.Truncate(0) // clear the buffer
	return n, err
}

// NewBufferedSender returns a new BufferedSender
//
// addr is a string of the format "hostname:port", and must be parsable by
// net.ResolveUDPAddr.
//
// flushInterval is a time.Duration, and specifies the maximum interval for
// packet sending. Note that if you send lots of metrics, you will send more
// often. This is just a maximal threshold.
//
// flushBytes specifies the maximum udp packet size you wish to send. If adding
// a metric would result in a larger packet than flushBytes, the packet will
// first be send, then the new data will be added to the next packet.
func NewBufferedSender(addr string, flushInterval time.Duration, flushBytes int) (Sender, error) {
	simpleSender, err := NewSimpleSender(addr)
	if err != nil {
		return nil, err
	}
	return newBufferedSenderWithSender(simpleSender, flushInterval, flushBytes)
}

// newBufferedSender returns a new BufferedSender
//
// sender is an instance of a statsd.Sender interface. Sender is required.
//
// flushInterval is a time.Duration, and specifies the maximum interval for
// packet sending. Note that if you send lots of metrics, you will send more
// often. This is just a maximal threshold.
//
// flushBytes specifies the maximum udp packet size you wish to send. If adding
// a metric would result in a larger packet than flushBytes, the packet will
// first be send, then the new data will be added to the next packet.
func newBufferedSenderWithSender(sender Sender, flushInterval time.Duration, flushBytes int) (Sender, error) {
	if sender == nil {
		return nil, fmt.Errorf("sender may not be nil")
	}

	bufSender := &BufferedSender{
		flushBytes:    flushBytes,
		flushInterval: flushInterval,
		sender:        sender,
		buffer:        senderPool.Get(),
		errchan:       make(chan error),
	}

	bufSender.Start()
	return bufSender, nil
}
