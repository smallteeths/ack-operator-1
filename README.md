# cnrancher/ack-operator

Operator for managing ACK clusters in Rancher.

## Building

```sh
make
```
You can find the binary in `bin/`,its name is `ack-operator`.

## Running

With a kubeconfig set in your shell, run the binary:

```sh
./ack-operator
```

Apply the CRD:

```sh
kubectl apply -f templates/ackclusterconfig.yaml
```

Create a new cluster

```sh
kubectl apply -f examples/create-examples.yaml
```

Import a exist cluster

```sh
kubectl apply -f examples/import-examples.yaml
```

Create a `Opaque` type Secret

```sh
apiVersion: v1
kind: Secret
metadata:
  name: "<name>"
  namespace: "<namespace>"
type: Opaque
data:
  aliyunecscredentialConfig-accessKeyId: "<accessKeyId-based64-encoded>"
  aliyunecscredentialConfig-accessKeySecret: "<accessKeySecret-based64-encoded>"
```

You can get base64 encoded content with the command below
```sh
echo -n "content" | base64
```