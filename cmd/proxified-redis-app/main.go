package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// k8sDialer wraps the Kubernetes port-forward into a net.Conn dialer
type k8sDialer struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
	namespace string
	podName   string
	podPort   string
}

func (d *k8sDialer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	// Create the port-forward request
	restClient := d.clientset.CoreV1().RESTClient()
	req := restClient.Post().
		Resource("pods").
		Namespace(d.namespace).
		Name(d.podName).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(d.config)
	if err != nil {
		return nil, err
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	// Use StreamConn to create a direct connection
	streamConn, _, err := dialer.Dial(portforward.PortForwardProtocolV1Name)
	if err != nil {
		return nil, err
	}

	// Create headers for port forwarding
	headers := http.Header{}
	headers.Set("streamType", "error")
	headers.Set("port", d.podPort)
	headers.Set("requestID", "0")

	// Negotiate the port forward - create error stream
	errorStream, err := streamConn.CreateStream(headers)
	if err != nil {
		streamConn.Close()
		return nil, err
	}

	// Create data stream
	headers.Set("streamType", "data")
	dataStream, err := streamConn.CreateStream(headers)
	if err != nil {
		streamConn.Close()
		return nil, err
	}

	// Wrap the streams as a net.Conn
	conn := &streamAdapter{
		dataStream:  dataStream,
		errorStream: errorStream,
		streamConn:  streamConn,
	}

	return conn, nil
}

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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Load kubeconfig
	pwd, _ := os.Getwd()
	kubeconfig := filepath.Join(pwd, "secrets", "kubeconfig.yaml")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	namespace := "default"
	remotePort := "6379"

	// Find a pod by label (e.g., app=redis)
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, v1.ListOptions{
		LabelSelector: "app=redis",
	})
	if err != nil {
		panic(err)
	}
	if len(pods.Items) == 0 {
		panic("no redis pods found")
	}
	podName := pods.Items[0].Name

	// Create custom dialer
	k8sDialer := &k8sDialer{
		config:    config,
		clientset: clientset,
		namespace: namespace,
		podName:   podName,
		podPort:   remotePort,
	}

	// Connect Redis with custom dialer - no local port created!
	rdb := redis.NewClient(&redis.Options{
		Addr: "k8s-pod:6379", // Dummy address, won't be used
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return k8sDialer.Dial(ctx, network, addr)
		},
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(err)
	}

	fmt.Printf("✅ Connected to Redis directly through K8s pod %s (no local port)\n", podName)

	// Simple Redis operations in loop
	for {
		select {
		case <-ctx.Done():
			fmt.Println("⏹ Connection closed")
			return
		default:
			if err := rdb.Set(ctx, "example_key", "hello from k8s", 5*time.Second).Err(); err != nil {
				panic(err)
			}

			val, err := rdb.Get(ctx, "example_key").Result()
			if err != nil {
				panic(err)
			}

			fmt.Printf("example_key: %s\n", val)
			time.Sleep(8 * time.Second)
		}
	}
}
