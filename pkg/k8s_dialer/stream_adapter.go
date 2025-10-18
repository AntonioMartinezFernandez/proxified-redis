package k8sdialer

import (
	"io"
	"net"
	"sync"
	"time"
)

// streamAdapter adapts SPDY streams to net.Conn interface
type streamAdapter struct {
	dataStream  io.ReadWriteCloser
	errorStream io.ReadWriteCloser
	streamConn  io.Closer
	readMu      sync.Mutex
	writeMu     sync.Mutex
}

func (s *streamAdapter) Read(b []byte) (n int, err error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	return s.dataStream.Read(b)
}

func (s *streamAdapter) Write(b []byte) (n int, err error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.dataStream.Write(b)
}

func (s *streamAdapter) Close() error {
	s.dataStream.Close()
	s.errorStream.Close()
	return s.streamConn.Close()
}

func (s *streamAdapter) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}

func (s *streamAdapter) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6379}
}

func (s *streamAdapter) SetDeadline(t time.Time) error {
	return nil
}

func (s *streamAdapter) SetReadDeadline(t time.Time) error {
	return nil
}

func (s *streamAdapter) SetWriteDeadline(t time.Time) error {
	return nil
}
