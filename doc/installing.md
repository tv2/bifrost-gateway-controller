# Installing *bifrost-gateway-controller*

## Validating Artifact Signatures

Container and Helm chart artifacts are signed using [cosign](https://github.com/sigstore/cosign).

Signature validation should always be done using artifact digest. You will find digests on the [container package page](https://github.com/tv2/bifrost-gateway-controller/pkgs/container/bifrost-gateway-controller).

Container signature validation:

```bash
export IMAGE_DIGEST=sha256:406122a8226ded74417856178d58e63a8265dc64f74ee6cedd88d2a44bbf41c2
cosign verify --certificate-identity-regexp https://github.com/tv2/bifrost-gateway-controller/.github/workflows/build-release.yaml@refs/.* \
              --certificate-oidc-issuer https://token.actions.githubusercontent.com \
			  ghcr.io/tv2/bifrost-gateway-controller@$IMAGE_DIGEST | jq .
```

Helm chart signature validation:

```bash
export CHART_DIGEST=sha256:cf638567ae1954b6c345a911768dbf17d002f24487ca128332830a5640f9b72d
cosign verify --certificate-identity-regexp https://github.com/tv2/bifrost-gateway-controller/.github/workflows/chart-publish.yaml@refs/.* \
              --certificate-oidc-issuer https://token.actions.githubusercontent.com \
			  ghcr.io/tv2/bifrost-gateway-controller-helm@$CHART_DIGEST | jq .
```

## Installing the Controller Using Helm

Deploying the controller using Helm as follows. Note, that the Helm
chart only contain RBAC settings for the core Gateway API resources,
and any additional resources included in blueprints should be added as
shown below with `gatewayclassblueprint-contour-istio-values.yaml`.

```
helm upgrade -i bifrost-gateway-controller-helm oci://ghcr.io/tv2/bifrost-gateway-controller-helm --version 0.1.6 --values charts/bifrost-gateway-controller/ci/gatewayclassblueprint-contour-istio-values.yaml -n bifrost-gateway-controller-system --create-namespace
```

> Note: Helm currently does not support specifying a digest as chart version.

In addition to the *bifrost-gateway-controller*, you will need
blueprints defining datapath implementations. See [Example
GatewayClassBlueprints](../blueprints/README.md).

## Metrics and Observability

The controller provides the following Prometheus/OpenMetrics metrics:

| Metric | Type | Description |
| ------ | ---- | ----------- |
| `bifrost_patchapply_total` | Counter | Number of server-side patch operations |
| `bifrost_patchapply_errors_total` | Counter | Number of server-side patch errors |
| `bifrost_template_errors_total` | Counter | Number of template render errors |
| `bifrost_template_parse_errors_total` | Counter | Number of template parse errors |
| `bifrost_resource_get_total` | Counter | Number of resources fetched to use as dependency in templates |

Additionally the controller provides [standard controller
metrics](https://book.kubebuilder.io/reference/metrics-reference.html)
and the Helm chart provides a
[ServiceMonitor](https://github.com/prometheus-operator/prometheus-operator/blob/main/Documentation/api.md#monitoring.coreos.com/v1.ServiceMonitor)
for integration with systems that understand this CRD.

Observability of Gateway-API resources is possible through [Custom
Resource State
Metrics](https://github.com/kubernetes/kube-state-metrics/blob/main/docs/customresourcestate-metrics.md)
through kube-state-metrics. An example configuration for deploying
kube-state-metrics is [provided
here](test-data/kube-state-metrics-values.yaml). Generally, this
configuration provides condition status for `GatewayClass`, `Gateway`
and `HTTPRoute` resources.

An SLI for e.g. `Gateway` resources can be created from the metric
`gateway_conditions` and watching for `Gateway`s without the
`Programmed` condition.
