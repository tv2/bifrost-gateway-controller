/*
Copyright 2023 TV 2 DANMARK A/S

Licensed under the Apache License, Version 2.0 (the "License") with the
following modification to section 6. Trademarks:

Section 6. Trademarks is deleted and replaced by the following wording:

6. Trademarks. This License does not grant permission to use the trademarks and
trade names of TV 2 DANMARK A/S, including but not limited to the TV 2® logo and
word mark, except (a) as required for reasonable and customary use in describing
the origin of the Work, e.g. as described in section 4(c) of the License, and
(b) to reproduce the content of the NOTICE file. Any reference to the Licensor
must be made by making a reference to "TV 2 DANMARK A/S", written in capitalized
letters as in this example, unless the format in which the reference is made,
requires lower case letters.

You may not use this software except in compliance with the License and the
modifications set out above.

You may obtain a copy of the license at:

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This file contain tests that are not e2e per se, however the e2e
// tests assume an external cluster with the controller deployed using
// 'production' means, e.g. a Helm chart and thus we have some basic
// tests here to validate the deployment of the controller on the
// external cluster.

package e2esuite

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"

	gwcapi "github.com/tv2/bifrost-gateway-controller/apis/gateway.tv2.dk/v1alpha1"
	selfapi "github.com/tv2/bifrost-gateway-controller/pkg/api"
)

const gatewayclassManifest string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: GatewayClass
metadata:
  name: cloud-gw
spec:
  controllerName: "github.com/tv2/bifrost-gateway-controller"
  parametersRef:
    group: gateway.tv2.dk
    kind: GatewayClassBlueprint
    name: default-gateway-class`

const gwClassParametersManifest string = `
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayClassBlueprint
metadata:
  name: default-gateway-class
spec:
  gatewayTemplate:
    status:
      template: |
        addresses:
        - type: IPAddress
          value: {{ (index .Resources.configMapTestSource 0).data.testIPAddress }}
    resourceTemplates:
      childGateway: |
        apiVersion: gateway.networking.k8s.io/v1beta1
        kind: Gateway
        metadata:
          name: {{ .Gateway.metadata.name }}-istio
          namespace: {{ .Gateway.metadata.namespace }}
          annotations:
            networking.istio.io/service-type: ClusterIP
        spec:
          gatewayClassName: istio
          listeners:
            {{- toYaml .Gateway.spec.listeners | nindent 6 }}
      configMapTestSource: |
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: source-configmap
          namespace: {{ .Gateway.metadata.namespace }}
        data:
          testIPAddress: 4.5.6.7
  httpRouteTemplate:
    resourceTemplates:
      childHttproute: |
        apiVersion: gateway.networking.k8s.io/v1beta1
        kind: HTTPRoute
        metadata:
          name: {{ .HTTPRoute.metadata.name }}-istio
          namespace: {{ .HTTPRoute.metadata.namespace }}
        spec:
          parentRefs:
          {{ range .HTTPRoute.spec.parentRefs }}
          - kind: {{ .kind }}
            name: {{ .name }}-istio
            {{ if get . "namespace" }}
            namespace: {{ .namespace }}
            {{ end }}
          {{ end }}
          rules:
          {{ toYaml .HTTPRoute.spec.rules | nindent 4 }}`

// example.com does resolve to an IP address so it is not ideal for testing
const gatewayManifest1 string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: foo1-gateway
  namespace: default
  labels:
    external-dns/export: "true"
spec:
  gatewayClassName: cloud-gw
  listeners:
  - name: prod-web
    port: 80
    protocol: HTTP
    hostname: foo1.example-foo4567.com`

const httprouteManifest1 string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: foo1-site
  namespace: default
spec:
  parentRefs:
  - kind: Gateway
    name: foo1-gateway
  rules:
  - backendRefs:
    - name: foo1-site
      port: 80`

// Hostname is on HTTPRoute instead of on Gateway
const gatewayManifest2 string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: foo2-gateway
  namespace: default
  labels:
    external-dns/export: "true"
spec:
  gatewayClassName: cloud-gw
  listeners:
  - name: prod-web
    port: 80
    protocol: HTTP`

const httprouteManifest2 string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: foo2-site
  namespace: default
spec:
  hostnames:
  - foo2.example-foo4567.com
  parentRefs:
  - kind: Gateway
    name: foo2-gateway
  rules:
  - backendRefs:
    - name: foo2-site
      port: 80`

// Wildcard hostname on Gateway, specific hostname on HTTPRoute
const gatewayManifest3 string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: foo3-gateway
  namespace: default
  labels:
    external-dns/export: "true"
spec:
  gatewayClassName: cloud-gw
  listeners:
  - name: prod-web
    port: 80
    protocol: HTTP
    hostname: "*.example-foo4567.com"`

const httprouteManifest3 string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: foo3-site
  namespace: default
spec:
  hostnames:
  - foo3.example-foo4567.com
  parentRefs:
  - kind: Gateway
    name: foo3-gateway
  rules:
  - backendRefs:
    - name: foo3-site
      port: 80`

var _ = Describe("GatewayClass", func() {

	const (
		fixmeExtendedTimeout = time.Second * 20 // This should go away when the normalization refactoring is implemented
		interval             = time.Millisecond * 250
		timeout              = time.Second * 10
	)

	Context("When a GatewayClass we own is created", func() {
		It("Should be marked as accepted", func() {
			By("Setting a condition")
			ctx := context.Background()

			gwc := &gatewayapi.GatewayClass{}
			err := yaml.Unmarshal([]byte(gatewayclassManifest), gwc)
			Expect(err).To(Succeed())
			Expect(k8sClient.Create(ctx, gwc)).To(Succeed())

			// We deliberately sleep here to make the controller initially see the GatewayClass without its corresponding parameters
			time.Sleep(5 * time.Second)

			gwcb := &gwcapi.GatewayClassBlueprint{}
			Expect(yaml.Unmarshal([]byte(gwClassParametersManifest), gwcb)).To(Succeed())
			Expect(k8sClient.Create(ctx, gwcb)).Should(Succeed())

			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, gwcb)).To(Succeed())
				Expect(k8sClient.Delete(ctx, gwc)).To(Succeed())
			})

			lookupKey := types.NamespacedName{Name: gwc.ObjectMeta.Name, Namespace: ""}
			gwcRead := &gatewayapi.GatewayClass{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, gwcRead)
				if err != nil ||
					len(gwcRead.Status.Conditions) == 0 ||
					gwcRead.Status.Conditions[0].Type != string(gatewayapi.GatewayClassConditionStatusAccepted) ||
					gwcRead.Status.Conditions[0].Status != "True" {
					return false
				}
				return true
			}, fixmeExtendedTimeout, interval).Should(BeTrue())
		})
	})
})

var _ = Describe("Gateway addresses", func() {

	const (
		externalDNSTimeout   = time.Second * 120
		interval             = time.Millisecond * 250
		timeout              = time.Second * 10
		fixmeExtendedTimeout = time.Second * 20 // This should go away when the normalization refactoring is implemented
	)
	var (
		ip4AddressRe *regexp.Regexp
		hostnameRe   *regexp.Regexp
	)

	BeforeEach(func() {
		ip4AddressRe = regexp.MustCompile(`^((25[0-5]|(2[0-4]|1\d|[1-9]|)\d)\.?\b){4}$`)
		hostnameRe = regexp.MustCompile(`^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])(\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9]))*$`)
		gwcb := &gwcapi.GatewayClassBlueprint{}
		Expect(yaml.Unmarshal([]byte(gwClassParametersManifest), gwcb)).To(Succeed())
		Expect(k8sClient.Create(ctx, gwcb)).Should(Succeed())

		gwc := &gatewayapi.GatewayClass{}
		err := yaml.Unmarshal([]byte(gatewayclassManifest), gwc)
		ctx = context.Background()
		Expect(err).To(Succeed())
		Expect(k8sClient.Create(ctx, gwc)).To(Succeed())

		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, gwc)).To(Succeed())
			Expect(k8sClient.Delete(ctx, gwcb)).To(Succeed())
		})
	})

	Context("When a Gateway/HTTPRoute is created", func() {
		It("Should be accepted by the controller", func() {
			By("Assigning an address to the Gateway")

			gw := &gatewayapi.Gateway{}
			Expect(yaml.Unmarshal([]byte(gatewayManifest1), gw)).To(Succeed())
			Expect(k8sClient.Create(ctx, gw)).To(Succeed())

			rt := &gatewayapi.HTTPRoute{}
			Expect(yaml.Unmarshal([]byte(httprouteManifest1), rt)).To(Succeed())
			Expect(k8sClient.Create(ctx, rt)).To(Succeed())

			gw2 := &gatewayapi.Gateway{}
			Expect(yaml.Unmarshal([]byte(gatewayManifest2), gw2)).To(Succeed())
			Expect(k8sClient.Create(ctx, gw2)).To(Succeed())

			rt2 := &gatewayapi.HTTPRoute{}
			Expect(yaml.Unmarshal([]byte(httprouteManifest2), rt2)).To(Succeed())
			Expect(k8sClient.Create(ctx, rt2)).To(Succeed())

			gw3 := &gatewayapi.Gateway{}
			Expect(yaml.Unmarshal([]byte(gatewayManifest3), gw3)).To(Succeed())
			Expect(k8sClient.Create(ctx, gw3)).To(Succeed())

			rt3 := &gatewayapi.HTTPRoute{}
			Expect(yaml.Unmarshal([]byte(httprouteManifest3), rt3)).To(Succeed())
			Expect(k8sClient.Create(ctx, rt3)).To(Succeed())

			// Test external-dns integration with hostname via Gateway resource
			lookupKey := types.NamespacedName{Name: gw.ObjectMeta.Name, Namespace: gw.ObjectMeta.Namespace}
			gwRead := &gatewayapi.Gateway{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, gwRead)
				GinkgoT().Logf("gwRead: %+v", gwRead)
				if err != nil ||
					len(gwRead.Status.Addresses) != 1 ||
					(*gwRead.Status.Addresses[0].Type == gatewayapi.IPAddressType && !ip4AddressRe.MatchString(gwRead.Status.Addresses[0].Value)) ||
					(*gwRead.Status.Addresses[0].Type == gatewayapi.HostnameAddressType && !hostnameRe.MatchString(gwRead.Status.Addresses[0].Value)) {
					return false
				}
				return true
			}, fixmeExtendedTimeout, interval).Should(BeTrue())

			By("Assigning status and address such that external-dns accepts and propagates the address (hostname via Gateway)")
			Eventually(func() bool {
				stdout := new(bytes.Buffer)
				err := ExecCmdInPodBySelector(k8sClient, restClient, cfg, client.MatchingLabels{"app": "multitool"}, "default",
					fmt.Sprintf("dig @coredns-test-only-coredns %s +short", *gw.Spec.Listeners[0].Hostname),
					nil, stdout, nil)
				if err != nil {
					return false
				}
				foundDNSLookup := strings.TrimRight(stdout.String(), "\n")
				GinkgoT().Logf("foundDNSLookup: %s, gateway has %s", foundDNSLookup, gwRead.Status.Addresses[0].Value)
				return gwRead.Status.Addresses[0].Value == foundDNSLookup
			}, externalDNSTimeout, interval).Should(BeTrue())

			// Test external-dns integration with hostname via HTTPRoute resource
			lookupKey2 := types.NamespacedName{Name: gw2.ObjectMeta.Name, Namespace: gw2.ObjectMeta.Namespace}
			gw2Read := &gatewayapi.Gateway{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey2, gw2Read)
				GinkgoT().Logf("gw2Read: %+v", gw2Read)
				if err != nil ||
					len(gw2Read.Status.Addresses) != 1 ||
					(*gw2Read.Status.Addresses[0].Type == gatewayapi.IPAddressType && !ip4AddressRe.MatchString(gw2Read.Status.Addresses[0].Value)) ||
					(*gw2Read.Status.Addresses[0].Type == gatewayapi.HostnameAddressType && !hostnameRe.MatchString(gw2Read.Status.Addresses[0].Value)) {
					return false
				}
				return true
			}, fixmeExtendedTimeout, interval).Should(BeTrue())

			By("Assigning status and address such that external-dns accepts and propagates the address (hostname via HTTPRoute)")
			Eventually(func() bool {
				stdout := new(bytes.Buffer)
				err := ExecCmdInPodBySelector(k8sClient, restClient, cfg, client.MatchingLabels{"app": "multitool"}, "default",
					fmt.Sprintf("dig @coredns-test-only-coredns %s +short", rt2.Spec.Hostnames[0]),
					nil, stdout, nil)
				if err != nil {
					return false
				}
				foundDNSLookup := strings.TrimRight(stdout.String(), "\n")
				GinkgoT().Logf("foundDNSLookup: %s, gateway2 has %s", foundDNSLookup, gw2Read.Status.Addresses[0].Value)
				return gw2Read.Status.Addresses[0].Value == foundDNSLookup
			}, externalDNSTimeout, interval).Should(BeTrue())

			// Test external-dns integration with wildcard and hostname with HTTPRoute resource
			lookupKey3 := types.NamespacedName{Name: gw3.ObjectMeta.Name, Namespace: gw3.ObjectMeta.Namespace}
			gw3Read := &gatewayapi.Gateway{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey3, gw3Read)
				GinkgoT().Logf("gw3Read: %+v", gw3Read)
				if err != nil ||
					len(gw3Read.Status.Addresses) != 1 ||
					(*gw3Read.Status.Addresses[0].Type == gatewayapi.IPAddressType && !ip4AddressRe.MatchString(gw3Read.Status.Addresses[0].Value)) ||
					(*gw3Read.Status.Addresses[0].Type == gatewayapi.HostnameAddressType && !hostnameRe.MatchString(gw3Read.Status.Addresses[0].Value)) {
					return false
				}
				return true
			}, fixmeExtendedTimeout, interval).Should(BeTrue())

			By("Assigning status and address such that external-dns accepts and propagates the address (wildcard + hostname via HTTPRoute)")
			Eventually(func() bool {
				stdout := new(bytes.Buffer)
				err := ExecCmdInPodBySelector(k8sClient, restClient, cfg, client.MatchingLabels{"app": "multitool"}, "default",
					fmt.Sprintf("dig @coredns-test-only-coredns %s +short", rt3.Spec.Hostnames[0]),
					nil, stdout, nil)
				if err != nil {
					return false
				}
				foundDNSLookup := strings.TrimRight(stdout.String(), "\n")
				GinkgoT().Logf("foundDNSLookup: %s, gateway3 has %s", foundDNSLookup, gw3Read.Status.Addresses[0].Value)
				return gw3Read.Status.Addresses[0].Value == foundDNSLookup
			}, externalDNSTimeout, interval).Should(BeTrue())

			By("Setting a status condition on the HTTPRoute")
			lookupKey = types.NamespacedName{Name: rt.ObjectMeta.Name, Namespace: rt.ObjectMeta.Namespace}
			rtRead := &gatewayapi.HTTPRoute{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, rtRead)
				if err != nil ||
					len(rtRead.Status.RouteStatus.Parents) != 1 ||
					len(rtRead.Status.RouteStatus.Parents[0].Conditions) != 1 ||
					string(rtRead.Status.RouteStatus.Parents[0].ParentRef.Name) != gw.ObjectMeta.Name ||
					// Namespace is optional, if defined it must match HTTPRoute namespace
					(rtRead.Status.RouteStatus.Parents[0].ParentRef.Namespace != nil && string(*rtRead.Status.RouteStatus.Parents[0].ParentRef.Namespace) != gw.ObjectMeta.Namespace) ||
					rtRead.Status.RouteStatus.Parents[0].ControllerName != selfapi.SelfControllerName ||
					rtRead.Status.RouteStatus.Parents[0].Conditions[0].Type != string(gatewayapi.RouteConditionAccepted) ||
					rtRead.Status.RouteStatus.Parents[0].Conditions[0].Status != "True" {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, rt)).To(Succeed())
			Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
			Expect(k8sClient.Delete(ctx, rt2)).To(Succeed())
			Expect(k8sClient.Delete(ctx, gw2)).To(Succeed())
			Expect(k8sClient.Delete(ctx, rt3)).To(Succeed())
			Expect(k8sClient.Delete(ctx, gw3)).To(Succeed())
		})
	})
})
