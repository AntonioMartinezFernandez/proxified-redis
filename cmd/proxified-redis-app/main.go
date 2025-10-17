package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/redis/go-redis/v9"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

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

	// Pick a free local port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	localPort := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
	listener.Close()

	// Correct port-forward URL
	restClient := clientset.CoreV1().RESTClient()
	req := restClient.Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		panic(err)
	}

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	fw, err := portforward.New(
		dialer,
		[]string{fmt.Sprintf("%s:%s", localPort, remotePort)},
		stopChan,
		readyChan,
		os.Stdout,
		os.Stderr,
	)
	if err != nil {
		panic(err)
	}

	// Run port-forward in background
	go func() {
		if err := fw.ForwardPorts(); err != nil {
			fmt.Fprintf(os.Stderr, "port-forward error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for the tunnel
	<-readyChan
	fmt.Printf("Port-forward established on localhost:%s -> %s:%s\n", localPort, podName, remotePort)

	// Connect Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("127.0.0.1:%s", localPort),
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(err)
	}

	fmt.Println("✅ Connected to Redis through port-forward")

	// Simple Redis operations in loop
	for {
		select {
		case <-ctx.Done():
			close(stopChan)
			fmt.Println("⏹ Port-forward closed")
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
