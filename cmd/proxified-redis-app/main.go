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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/rest"
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

	// Pod and namespace where Redis is running
	serviceName := "redis"
	namespace := "default"
	remotePort := "6379"

	// Find an available local port dynamically
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	localPort := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
	listener.Close()

	// Get the pod name for the service (assuming label app=redis)
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=redis",
	})
	if err != nil {
		panic(err)
	}
	if len(pods.Items) == 0 {
		panic("no redis pods found")
	}
	podName := pods.Items[0].Name

	// Create the port-forward URL
	restClient := clientset.CoreV1().RESTClient()
	req := restClient.Post().
		Resource("services").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	// Create SPDY transport and upgrader
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		panic(err)
	}

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})
	out := os.Stdout
	errOut := os.Stderr

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%s:%s", localPort, remotePort)}, stopChan, readyChan, out, errOut)
	if err != nil {
		panic(err)
	}

	// Start the port-forward in background
	go func() {
		if err := fw.ForwardPorts(); err != nil {
			fmt.Fprintf(os.Stderr, "port-forward error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for the tunnel to be ready
	<-readyChan
	fmt.Printf("Port-forward established on localhost:%s -> %s:%s\n", localPort, serviceName, remotePort)

	// Connect Redis client to the forwarded port
	rdb := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("127.0.0.1:%s", localPort),
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(err)
	}

	fmt.Println("✅ Connected to Redis through port-forward")

	// Set a key with TTL
	if err := rdb.Set(ctx, "example_key", "hello from k8s", 30*time.Second).Err(); err != nil {
		panic(err)
	}

	val, err := rdb.Get(ctx, "example_key").Result()
	if err != nil {
		panic(err)
	}

	fmt.Printf("example_key: %s\n", val)

	// Keep alive until Ctrl+C
	<-ctx.Done()
	close(stopChan)
	fmt.Println("⏹ Port-forward closed")
}
