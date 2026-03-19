// This file contain tests specifically targeted towards code in common.go

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gwcapi "github.com/tv2/bifrost-gateway-controller/apis/gateway.tv2.dk/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
)

const commonTestGatewayClassManifest string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: GatewayClass
metadata:
  name: common-test
spec:
  controllerName: "github.com/tv2/bifrost-gateway-controller"
  parametersRef:
    group: gateway.tv2.dk
    kind: GatewayClassBlueprint
    name: common-test
`

const commonTestGatewayManifest string = `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: common-test
  namespace: default
spec:
  gatewayClassName: common-test
  listeners:
  - name: prod-web
    port: 80
    protocol: HTTP
    hostname: example.com
`

const commonTestGatewayClassBlueprintManifest string = `
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayClassBlueprint
metadata:
  name: common-test
spec:
  values:
    override:
      someValue1: blueprint-override1
      nested:
        someValue1: blueprint-nested-override1
    default:
      someValue5: blueprint-default5
      someValue6: blueprint-default6
      someValue7: blueprint-default7
      someValue8: blueprint-default8
      nested:
        someValue2: blueprint-nested-default2
        someValue3: blueprint-nested-default3

  gatewayTemplate:
    resourceTemplates:
      configMapTestDestination: |
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: common-test
          namespace: {{ .Gateway.metadata.namespace }}
        data:
          someValue1: {{ .Values.someValue1 }}
          someValue2: {{ .Values.someValue2 }}
          someValue3: {{ .Values.someValue3 }}
          someValue4: {{ .Values.someValue4 }}
          someValue5: {{ .Values.someValue5 }}
          someValue6: {{ .Values.someValue6 }}
          someValue7: {{ .Values.someValue7 }}
          someValue8: {{ .Values.someValue8 }}
          someValue9: {{ .Values.someValue9 }}
          someValue10: {{ .Values.someValue10 }}
          someNestedValue1: {{ .Values.nested.someValue1 }}
          someNestedValue2: {{ .Values.nested.someValue2 }}
          someNestedValue3: {{ .Values.nested.someValue3 }}
`

const commonTestGlobalPolicy1Manifest string = `
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayClassConfig
metadata:
  name: common-test-global1
  namespace: bifrost-gateway-controller-system
spec:
  override:
    someValue2: global-config1-override2
  default:
    someValue5: global-config1-default5
  targetRef:
    group: gateway.networking.k8s.io
    kind: GatewayClass
    name: common-test
`

const commonTestNsPolicy1Manifest string = `
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayClassConfig
metadata:
  name: common-test-ns1
  namespace: default      # Note, same NS as Gateway
spec:
  override:
    someValue3: global-config2-override3
  default:
    someValue6: global-config2-default6
  targetRef:
    group: gateway.networking.k8s.io
    kind: GatewayClass
    name: common-test
`

const commonTestNsPolicy2Manifest string = `
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayClassConfig
metadata:
  name: common-test-ns2
  namespace: default      # Note, same NS as Gateway
spec:
  override:
    someValue10: global-config3-override10
  targetRef:
    group: ""
    kind: Namespace
    name: default
`

const commonTestPolicy1Manifest string = `
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayConfig
metadata:
  name: common-test-gw1
  namespace: default
spec:
  override:
    someValue2: config1-override2   # Overridden by GatewayClassConfig
    someValue3: config1-override3   # Overridden by GatewayClassConfig
    someValue4: config1-override4
    nested:
      someValue3: config1-nested-override3
  default:
    someValue7: config1-default7
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: common-test
    namespace: default
`

const commonTestNsPolicy3Manifest string = `
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayConfig
metadata:
  name: common-test-ns1
  namespace: default
spec:
  default:
    someValue9: ns-config1-default9
  targetRef:
    group: ""
    kind: Namespace
    name: default
`

var _ = Describe("Attached policies and value precedence", func() {

	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		gwc                 *gatewayapi.GatewayClass
		gwcb                *gwcapi.GatewayClassBlueprint
		gwcc1, gwcc2, gwcc3 *gwcapi.GatewayClassConfig
		gwc1, gwc2          *gwcapi.GatewayConfig
		ctx                 context.Context
	)

	BeforeEach(func() {
		gwc = &gatewayapi.GatewayClass{}
		gwcb = &gwcapi.GatewayClassBlueprint{}
		gwcc1 = &gwcapi.GatewayClassConfig{}
		gwcc2 = &gwcapi.GatewayClassConfig{}
		gwcc3 = &gwcapi.GatewayClassConfig{}
		gwc1 = &gwcapi.GatewayConfig{}
		gwc2 = &gwcapi.GatewayConfig{}
		ctx = context.Background()
		Expect(yaml.Unmarshal([]byte(commonTestGatewayClassManifest), gwc)).To(Succeed())
		Expect(k8sClient.Create(ctx, gwc)).Should(Succeed())
		Expect(yaml.Unmarshal([]byte(commonTestGatewayClassBlueprintManifest), gwcb)).To(Succeed())
		Expect(k8sClient.Create(ctx, gwcb)).Should(Succeed())
		Expect(yaml.Unmarshal([]byte(commonTestGlobalPolicy1Manifest), gwcc1)).To(Succeed())
		Expect(k8sClient.Create(ctx, gwcc1)).Should(Succeed())
		Expect(yaml.Unmarshal([]byte(commonTestNsPolicy1Manifest), gwcc2)).To(Succeed())
		Expect(k8sClient.Create(ctx, gwcc2)).Should(Succeed())
		Expect(yaml.Unmarshal([]byte(commonTestNsPolicy2Manifest), gwcc3)).To(Succeed())
		Expect(k8sClient.Create(ctx, gwcc3)).Should(Succeed())
		Expect(yaml.Unmarshal([]byte(commonTestPolicy1Manifest), gwc1)).To(Succeed())
		Expect(k8sClient.Create(ctx, gwc1)).Should(Succeed())
		Expect(yaml.Unmarshal([]byte(commonTestNsPolicy3Manifest), gwc2)).To(Succeed())
		Expect(k8sClient.Create(ctx, gwc2)).Should(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(ctx, gwc)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, gwcb)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, gwcc1)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, gwcc2)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, gwcc3)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, gwc1)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, gwc2)).Should(Succeed())
	})

	When("Reconciling a parent Gateway", func() {
		var gw *gatewayapi.Gateway
		BeforeEach(func() {
			gw = &gatewayapi.Gateway{}
			Expect(yaml.Unmarshal([]byte(commonTestGatewayManifest), gw)).To(Succeed())
			Expect(k8sClient.Create(ctx, gw)).Should(Succeed())
		})

		It("Should use values correctly", func() {

			cm := corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "common-test", Namespace: "default"}, &cm)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// https://gateway-api.sigs.k8s.io/references/policy-attachment/#hierarchy
			By("Setting the content of the destination configmap according to GEP-713")
			Expect(cm.Data["someValue1"]).To(Equal("blueprint-override1"))
			Expect(cm.Data["someValue2"]).To(Equal("global-config1-override2"))
			Expect(cm.Data["someValue3"]).To(Equal("global-config2-override3"))
			Expect(cm.Data["someValue4"]).To(Equal("config1-override4"))
			Expect(cm.Data["someValue5"]).To(Equal("global-config1-default5"))
			Expect(cm.Data["someValue6"]).To(Equal("global-config2-default6"))
			Expect(cm.Data["someValue7"]).To(Equal("config1-default7"))
			Expect(cm.Data["someValue8"]).To(Equal("blueprint-default8"))
			Expect(cm.Data["someValue9"]).To(Equal("ns-config1-default9"))
			Expect(cm.Data["someValue10"]).To(Equal("global-config3-override10"))
			Expect(cm.Data["someNestedValue1"]).To(Equal("blueprint-nested-override1"))
			Expect(cm.Data["someNestedValue2"]).To(Equal("blueprint-nested-default2"))
			Expect(cm.Data["someNestedValue3"]).To(Equal("config1-nested-override3"))
		})
	})
})
