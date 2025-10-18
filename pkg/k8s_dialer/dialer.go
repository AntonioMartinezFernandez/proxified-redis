package k8sdialer

import (
	"context"
	"net"
	"net/http"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

func NewK8sDialer(config *rest.Config, clientset *kubernetes.Clientset, namespace, podName, podPort string) *k8sDialer {
	return &k8sDialer{
		config:    config,
		clientset: clientset,
		namespace: namespace,
		podName:   podName,
		podPort:   podPort,
	}
}

func (d *k8sDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.Dial(ctx, network, addr)
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
