# Getting Started using a KIND Cluster

This guide describes how to exercise the _bifrost-gateway-controller_
using a KIND cluster, i.e. this guide does not require a cloud
account. Instead, some resources that typically are allocated from a
cloud provider are instead simulated inside the KIND
cluster. Specifically:

- An `ingress` resource is used to simulate load-balancer allocation. [Contour](https://projectcontour.io) is used to implement the `ingress` resources.

- [MetalLB](metallb.io) is used to assign IP addresses to the `type=LoadBalancer` `service` created by Contour.
  **NOTE, The IP address pool used is taken from the Docker kind network and this require network access to the docker network from where we run our tests. I.e. this setup most likely only work on Linux, not Mac or Windows. In the demonstration below we test using a container-based curl, which will run in the docker network and thus this will work also on e.g. Mac**

- Cert-manager is used together with a cluster-internal CA to issue certificates.

- External-dns is used with a cluster-internal CoreDNS deployment to handle DNS.

The cluster-internal datapath is implemented using Istio.

## Prerequisites

See [devbox.json](https://www.jetify.com/docs/devbox/) definition in the root of the repo for tooling needed.

## Deploy KIND Cluster and Dependencies

Deploy the KIND cluster and add gateway-API with:

```shell
make create-cluster deploy-gateway-api
```

Deploy MetalLB (for load-balancers) Istio control-plane, Contour and
external-dns+local CoreDNS and cert-manager with:

```shell
make deploy-metallb
make deploy-istio
make deploy-contour deploy-contour-provisioner
make setup-external-dns-test
make deploy-cert-manager
```

Alternatively the following make target wraps all of above commands:

```shell
make setup-getting-started-cluster
```

## Deploy bifrost-gateway-controller

There are three alternative ways to deploy the controller as described
by the following sections.

### Deploy with Helm (recommended)

Deploying the controller using Helm can be done as follows:

```shell
helm upgrade -i bifrost-gateway-controller-helm oci://ghcr.io/tv2/bifrost-gateway-controller-helm -n bifrost-gateway-controller-system --create-namespace --values charts/bifrost-gateway-controller/ci/gatewayclassblueprint-contour-istio-values.yaml
```

### Deploy from Local-build and YAML Artifacts (recommended for end-to-end tests)

Deploying the controller using the generated manifests will
additionally test these, hence this is recommended for end-to-end
testing:

```shell
make build docker-build             # Build controller+container
make install                        # Install CRDs
make cluster-load-controller-image  # Load container into KIND
make deploy                         # Deploy from YAML manifests
```

### Run Controller Locally (recommended for development of the controller)

Running the controller locally is useful during development of the
controller:

```shell
make install   # Install CRDs
make run
```

### Deploy GatewayClass for KIND Datapath

```shell
kubectl apply -f blueprints/contour-istio/gatewayclass-contour-istio-cert.yaml
kubectl apply -f blueprints/contour-istio/gatewayclassblueprint-contour-istio-cert.yaml
```

## Create Datapath and Deploy Test Applications

As an example, we will implement the following usecase:

![Gateway-API example](images/gateway-api-multi-namespace.png)
(source: https://gateway-api.sigs.k8s.io/)

In this example, three different personas are used. A cluster
operator/SRE that manages a gateway in a `foo-infra` namespace and two
developer personas that manage two applications in `foo-store` and
`foo-site` namespaces.

To watch the progress and resources created, it can be convenient to watch for
resources with the following command:

```shell
watch kubectl get gateway,httproute,ingress,certificate,po,gatewayclass -A
```

### Cluster Operator/SRE

The cluster operator persona creates namespaces (and RBAC etc, but
that is out out-of-scope for this guide):

```shell
kubectl apply -f test-data/getting-started/foo-namespaces.yaml
```

The cluster-operator/SRE also creates the common `Gateway` using the
`GatewayClass` created previously:

```shell
cat test-data/getting-started/foo-gateway.yaml | GATEWAY_CLASS_NAME=contour-istio-cert DOMAIN=foo.example.com envsubst | kubectl apply -f -
```

### Developer of 'Site' Application

As a 'site' developer we deploy the 'foo-site' application and associated
routing into the `foo-site` namespace:

```shell
kubectl -n foo-site apply -f test-data/getting-started/app-foo-site.yaml
kubectl -n foo-site apply -f test-data/getting-started/foo-site-httproute.yaml
```

### Developer of 'Store' Application

As a 'store' developer we deploy the v1 and v2 versions of the 'foo-store'
application and associated routing into the `foo-store` namespace:

```shell
kubectl -n foo-store apply -f test-data/getting-started/app-foo-store-v1.yaml
kubectl -n foo-store apply -f test-data/getting-started/app-foo-store-v2.yaml
kubectl -n foo-store apply -f test-data/getting-started/foo-store-httproute.yaml
```

### Observations

In response to the `foo-gateway` `Gateway` resource, expect to see a
shadow `Gateway` called `foo-gateway-istio`. Also, expect to see Istio
respond to the `foo-gateway-istio` `Gateway` by creating an
ingress-gateway deployment. The PODs created for the Istio
ingress-gateway names will start with `foo-gateway-istio-`.

### Testing the Datapath

Test access to foo-site and foo-store applications.
First look IP of load-balancer:

```shell
export GATEWAY_IP=$(kubectl -n projectcontour get svc contour-envoy -o=jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

```shell
scripts/curl.sh -s --resolve foo.example.com:80:$GATEWAY_IP http://foo.example.com/site
scripts/curl.sh -s --resolve foo.example.com:80:$GATEWAY_IP http://foo.example.com/store
```

Expect to see a `Welcome-to-foo-site`, `Welcome-to-foo-store-v1` and
`Welcome-to-foo-store-v2` being echoed.

Similarly, but using HTTPS through the cert-manager issued
certificate:

```shell
scripts/curl.sh -s --cacert foo-example-com.crt --resolve foo.example.com:443:$GATEWAY_IP https://foo.example.com/site
scripts/curl.sh -s --cacert foo-example-com.crt --resolve foo.example.com:443:$GATEWAY_IP https://foo.example.com/store
```

### Testing Integration with DNS

The KIND cluster for the getting-started example includes a deployment
of [external-dns](https://github.com/kubernetes-sigs/external-dns),
configured to update a test-only CoreDNS instance. This CoreDNS
instance can be quieried using the tooling container also deployed as
part of the KIND cluster setup:

```shell
kubectl exec -it deploy/multitool -- dig @coredns-test-only-coredns example.com +short
```

The output should be the same IP address as reported from `kubectl get gateway -n foo-infra` in the `ADDRESS` column.

## Cleanup

```shell
make delete-cluster
```
