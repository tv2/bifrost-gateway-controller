#! /bin/bash

SCOPE=${1:-""}

if [ -z "$SCOPE" ] || [ "$SCOPE" == "blueprints" ]; then
    echo ""
    echo "-------------------------------------------------------------------"
    read -p "Press enter to deploy GatewayClassBlueprint + GatewayClass'es"
    kubectl apply -f blueprints/gatewayclassblueprint-aws-alb-crossplane.yaml
    kubectl apply -f blueprints/gatewayclass-aws-alb-crossplane.yaml
fi

if [ -z "$SCOPE" ] || [ "$SCOPE" == "configs" ]; then
    echo ""
    echo "-------------------------------------------------------------------"
    read -p "Press enter to deploy GatewayClassConfig's"
    kubectl apply -f hack/demo/gatewayclassconfig-public.yaml
    kubectl apply -f hack/demo/gatewayclassconfig-internal.yaml
fi

if [ -z "$SCOPE" ] || [ "$SCOPE" == "tenantconfig" ]; then
    echo ""
    echo "-------------------------------------------------------------------"
    read -p "Press enter to deploy namespace-default GatewayClassConfig's"
    kubectl apply -f hack/demo/foo-namespaces.yaml
    kubectl apply -f hack/demo/namespace-gatewayclassconfig.yaml
fi

if [ -z "$SCOPE" ] || [ "$SCOPE" == "gateway" ]; then
    echo ""
    echo "-------------------------------------------------------------------"
    read -p "Press enter to deploy getting-started usecase Gateway"
    kubectl -n foo-infra apply -f hack/demo/foo-namespaces.yaml -f hack/demo/foo-gateway.yaml
fi

if [ -z "$SCOPE" ] || [ "$SCOPE" == "acl" ]; then
    echo ""
    echo "-------------------------------------------------------------------"
    read -p "Press enter to show user GatewayConfig with ACL CIDR"
    hack/demo/test-add-user-acl.sh
fi

if [ -z "$SCOPE" ] || [ "$SCOPE" == "app" ]; then
    echo ""
    echo "-------------------------------------------------------------------"
    read -p "Press enter to deploy getting-started usecase application"
    kubectl -n foo-site apply -f test-data/getting-started/app-foo-site.yaml
    kubectl -n foo-site  apply -f test-data/getting-started/foo-site-httproute.yaml
    kubectl -n foo-store apply -f test-data/getting-started/app-foo-store-v1.yaml
    kubectl -n foo-store apply -f test-data/getting-started/app-foo-store-v2.yaml
    kubectl -n foo-store apply -f test-data/getting-started/foo-store-httproute.yaml
fi

if [ -z "$SCOPE" ] || [ "$SCOPE" == "bifrost" ]; then
    echo ""
    echo "-------------------------------------------------------------------"
    read -p "Press enter to deploy bifrost-gateway-controller"
	helm repo add tv2 https://tv2.github.io/bifrost-gateway-controller 2>/dev/null
	helm upgrade -i bifrost-gateway-controller tv2/bifrost-gateway-controller --version 0.1.4 --values charts/bifrost-gateway-controller/ci/gatewayclassblueprint-crossplane-aws-alb-values.yaml -n bifrost-gateway-controller-system 2>/dev/null
fi
