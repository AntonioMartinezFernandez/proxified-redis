package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	k8sdialer "proxified-redis/pkg/k8s_dialer"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	pwd, _ := os.Getwd()

	var (
		namespace          = "default"
		podPort            = "6379"
		redisLabelSelector = "app=redis"
		counter            = 0
		kubeConfigPath     = filepath.Join(pwd, "secrets", "kubeconfig.yaml")
	)

	ctx, ctxCancelFunc := signal.NotifyContext(context.Background(), os.Interrupt)
	defer ctxCancelFunc()

	// Load kubeconfig
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		fmt.Println("fatal error: ", err)
		os.Exit(1)
	}

	// Create Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		fmt.Println("fatal error: ", err)
		os.Exit(1)
	}

	// Find Redis pod
	redisPods, err := clientset.CoreV1().Pods(namespace).List(ctx, v1.ListOptions{
		LabelSelector: redisLabelSelector,
	})
	if err != nil {
		fmt.Println("fatal error: ", err)
		os.Exit(1)
	}
	if len(redisPods.Items) == 0 {
		panic("no redis pods found")
	}
	podName := redisPods.Items[0].Name

	// Create Redis client with k8s dialer
	redisClient := NewRedisClient(ctx, kubeConfig, clientset, namespace, podName, podPort)

	// Execute SET/GET Redis operations in loop
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("connection closed")
			return

		case <-ticker.C:
			if err := redisClient.Set(
				ctx, "key_"+strconv.Itoa(counter),
				"Hello Pepito!",
				10*time.Second,
			).Err(); err != nil {
				fmt.Println("fatal error: ", err)
				os.Exit(1)
			}

			val, err := redisClient.Get(ctx, "key_"+strconv.Itoa(counter)).Result()
			if err != nil {
				fmt.Println("fatal error: ", err)
				os.Exit(1)
			}

			fmt.Printf("key_%d: %s\n", counter, val)
			counter++
		}
	}
}

func NewRedisClient(ctx context.Context, config *rest.Config, clientset *kubernetes.Clientset, namespace string, podName string, podPort string) *redis.Client {
	// Create custom dialer
	k8sDialer := k8sdialer.NewK8sDialer(config, clientset, namespace, podName, podPort)

	// Create Redis client with the custom dialer
	redisClient := redis.NewClient(&redis.Options{
		Addr:   "dummy_address_not_used:6379",
		Dialer: k8sDialer.DialContext,
	})

	if err := redisClient.Ping(ctx).Err(); err != nil {
		fmt.Println("fatal error: ", err)
		os.Exit(1)
	}

	fmt.Printf("✅ connected to redis directly through K8s pod %s (no local port)\n", podName)

	return redisClient
}
