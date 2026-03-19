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

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gwcapi "github.com/tv2/bifrost-gateway-controller/apis/gateway.tv2.dk/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
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

const gatewayclassManifestInvalid string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: GatewayClass
metadata:
  name: cloud-gw-invalid
spec:
  controllerName: "github.com/tv2/bifrost-gateway-controller"`

const gatewayclassManifestNotOurs string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: GatewayClass
metadata:
  name: not-our-gatewayclass
spec:
  controllerName: "github.com/acme/bifrost-gateway-controller"`

const gwClassBlueprintManifest string = `
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayClassBlueprint
metadata:
  name: default-gateway-class
spec:
  gatewayTemplate:
    resourceTemplates:
      istioShadowGw: |
        apiVersion: gateway.networking.k8s.io/v1beta1
        kind: Gateway
        metadata:
          name: {{ .Gateway.ObjectMeta.Name }}-istio
          namespace: {{ .Gateway.metadata.namespace }}
          annotations:
            networking.istio.io/service-type: ClusterIP
        spec:
          gatewayClassName: istio
          listeners:
            {{- toYaml .Gateway.spec.listeners | nindent 6 }}
  httpRouteTemplate:
    resourceTemplates:
      shadowHttproute: |
        apiVersion: gateway.networking.k8s.io/v1beta1
        kind: HTTPRoute
        metadata:
          name: {{ .HTTPRoute.ObjectMeta.Name }}-istio
          namespace: {{ .HTTPRoute.metadata.namespace }}
        spec:
          parentRefs:
          {{ range .HTTPRoute.spec.parentRefs }}
          - kind: {{ .kind }}
            name: {{ .name }}-istio
            namespace: {{ .namespace }}
          {{ end }}
          rules:
          {{ toYaml .HTTPRoute.spec.rules | nindent 4 }}`

var _ = Describe("GatewayClass controller", func() {

	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		gwcIn, gwc *gatewayapi.GatewayClass
		gwcb       *gwcapi.GatewayClassBlueprint
		ctx        context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		gwcIn = &gatewayapi.GatewayClass{}
		gwc = &gatewayapi.GatewayClass{}
		gwcb = &gwcapi.GatewayClassBlueprint{}
	})

	When("A gatewayclass we own is created", func() {

		It("Should be marked as accepted", func() {

			err := yaml.Unmarshal([]byte(gatewayclassManifest), gwcIn)
			Expect(err).Should(Succeed())
			Expect(k8sClient.Create(ctx, gwcIn)).Should(Succeed())

			Expect(yaml.Unmarshal([]byte(gwClassBlueprintManifest), gwcb)).To(Succeed())
			Expect(k8sClient.Create(ctx, gwcb)).Should(Succeed())

			lookupKey := types.NamespacedName{Name: gwcIn.ObjectMeta.Name, Namespace: ""}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, gwc)
				if err != nil ||
					gwc.Status.Conditions[0].Type != string(gatewayapi.GatewayClassConditionStatusAccepted) ||
					gwc.Status.Conditions[0].Status != "True" {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, gwcIn)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, gwcb)).Should(Succeed())
		})
	})

	When("An invalid gatewayclass we own is created", func() {
		It("Should be marked as invalid", func() {

			err := yaml.Unmarshal([]byte(gatewayclassManifestInvalid), gwcIn)
			Expect(err).Should(Succeed())
			Expect(k8sClient.Create(ctx, gwcIn)).Should(Succeed())

			lookupKey := types.NamespacedName{Name: gwcIn.ObjectMeta.Name, Namespace: ""}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, gwc)
				if err != nil ||
					gwc.Status.Conditions[0].Type != string(gatewayapi.GatewayClassConditionStatusAccepted) ||
					gwc.Status.Conditions[0].Status != "False" ||
					gwc.Status.Conditions[0].Reason != string(gatewayapi.GatewayClassReasonInvalidParameters) {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, gwcIn)).Should(Succeed())
		})
	})

	When("A gatewayclass we do not own is created", func() {
		It("Should not be marked as accepted", func() {

			err := yaml.Unmarshal([]byte(gatewayclassManifestNotOurs), gwcIn)
			Expect(err).Should(Succeed())
			Expect(k8sClient.Create(ctx, gwcIn)).Should(Succeed())

			lookupKey := types.NamespacedName{Name: gwcIn.ObjectMeta.Name, Namespace: ""}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, gwc)
				if err != nil ||
					gwc.Status.Conditions[0].Status != "Unknown" {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, gwcIn)).Should(Succeed())
		})
	})
})
