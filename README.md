# Proxified Redis

Instead of using a traditional port-forwarding approach to access Redis services running inside a Kubernetes cluster, this project shows how to create a SPDY stream directly to the Redis pod.

This allows for more efficient and reliable communication with a remote Redis instance running in a Kubernetes cluster, especially in scenarios where the security and performance of the connection are critical.

Each time the Redis client needs a connection, it calls the custom dialer which establishes a direct stream to the pod using the Kubernetes API. No local ports are opened or bound on the host machine.

## Getting Started

To run this Proxified Redis locally using Kind and Skaffold, follow these steps:

```bash
make run-cluster
make skaffold

# Ctrl+C to stop skaffold when redis is ready

cp $HOME/.kube/config secrets/kubeconfig.yaml
make run
```
