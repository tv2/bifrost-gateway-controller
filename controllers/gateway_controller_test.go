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
	"regexp"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gwcapi "github.com/tv2/bifrost-gateway-controller/apis/gateway.tv2.dk/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
)

// ------------------------------------
// Test Helpers
// ------------------------------------

// conditionStateIs is a helper function to check if a Gateway has a condition of a given type, status, reason and message pattern
func conditionStateIs(gw *gatewayapi.Gateway, condType string, status *metav1.ConditionStatus, reason, messageRegEx *string) bool {
	var msgMatch *regexp.Regexp
	if messageRegEx != nil {
		msgMatch, _ = regexp.Compile(*messageRegEx)
	}
	for _, cond := range gw.Status.Conditions {
		if cond.Type == condType &&
			(status == nil || cond.Status == *status) &&
			(reason == nil || cond.Reason == *reason) &&
			(messageRegEx == nil || msgMatch.MatchString(cond.Message)) {
			return true
		}
	}
	return false
}

// setGatewayStatus is a helper function to update the status of a Gateway in the test environment
func setGatewayStatus(nn types.NamespacedName, newCondition *metav1.Condition, address *gatewayapi.GatewayStatusAddress) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		gw := &gatewayapi.Gateway{}

		if err := k8sClient.Get(context.TODO(), nn, gw); err != nil {
			return err
		}

		if newCondition != nil {
			newCondition.ObservedGeneration = gw.ObjectMeta.Generation
			meta.SetStatusCondition(&gw.Status.Conditions, *newCondition)
			GinkgoT().Logf("update gw: %+v conditions: %+v\n", gw, newCondition)
		}
		if address != nil {
			gw.Status.Addresses = []gatewayapi.GatewayStatusAddress{}
			gw.Status.Addresses = append(gw.Status.Addresses, *address)
		}

		return k8sClient.Status().Update(context.TODO(), gw)
	})
}

// Shared resource template used by both ready and non-ready blueprints
const tmplConfigMapSource = `apiVersion: v1
kind: ConfigMap
metadata:
  name: source-configmap
  namespace: {{ .Gateway.metadata.namespace }}
data:
  valueToRead1: Hello
  valueToRead2: World
`

// newTestGatewayClass returns a GatewayClass with a reference to the default test GatewayClassBlueprint
func newTestGatewayClass() *gatewayapi.GatewayClass {
	return &gatewayapi.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: gatewayapi.GatewayClassSpec{
			ControllerName: "github.com/tv2/bifrost-gateway-controller",
			ParametersRef: &gatewayapi.ParametersReference{
				Group: "gateway.tv2.dk",
				Kind:  "GatewayClassBlueprint",
				Name:  "default-gateway-class",
			},
		},
	}
}

// newTestGateway returns a Gateway with a reference to the default test GatewayClass
func newTestGateway() *gatewayapi.Gateway {
	hostname := gatewayapi.Hostname("example.com")
	return &gatewayapi.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "foo-gateway", Namespace: "default"},
		Spec: gatewayapi.GatewaySpec{
			GatewayClassName: "default",
			Listeners: []gatewayapi.Listener{{
				Name:     "prod-web",
				Port:     80,
				Protocol: gatewayapi.HTTPProtocolType,
				Hostname: &hostname,
			}},
		},
	}
}

// newTestBlueprint returns a GatewayClassBlueprint with templates that
// reference each other to test inter-resource referencing and value inheritance
func newTestBlueprint() *gwcapi.GatewayClassBlueprint {
	return &gwcapi.GatewayClassBlueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "default-gateway-class"},
		Spec: gwcapi.GatewayClassBlueprintSpec{
			Values: gwcapi.TemplateValues{
				Default: jsonRaw(`{"configmap2SuffixData":["one","two","three"]}`),
			},
			GatewayTemplate: gwcapi.ResourceSpec{
				ResourceStatusSpec: gwcapi.ResourceStatusSpec{
					Status: map[string]string{
						"template": "addresses:\n  {{ toYaml (index .Resources.childGateway 0).status.addresses | nindent 2}}\n",
					},
				},
				ResourceTemplate: gwcapi.ResourceTemplate{
					ResourceTemplates: map[string]string{
						"childGateway": `apiVersion: gateway.networking.k8s.io/v1beta1
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
`,
						"configMapTestSource": tmplConfigMapSource,
						"configMapTestIntermediate1": `apiVersion: v1
kind: ConfigMap
metadata:
  name: intermediate1-configmap
  namespace: {{ .Gateway.metadata.namespace }}
data:
  valueIntermediate: {{ (index .Resources.configMapTestSource 0).data.valueToRead1 }}
`,
						"configMapTestIntermediate2": `{{ range $idx,$suffix := .Values.configmap2SuffixData }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: intermediate2-configmap-{{ $idx }}
  namespace: {{ $.Gateway.metadata.namespace }}
data:
  valueIntermediate: {{ (index $.Resources.configMapTestSource 0).data.valueToRead1 }}-{{ $suffix }}
---
{{ end }}
`,
						"configMapTestDestination": `apiVersion: v1
kind: ConfigMap
metadata:
  name: dst-configmap
  namespace: {{ .Gateway.metadata.namespace }}
data:
  valueRead: {{ printf "%s, %s" (index .Resources.configMapTestIntermediate1 0).data.valueIntermediate (index .Resources.configMapTestSource 0).data.valueToRead2 | upper }}
  valueRead2: {{ printf "Testing, one two %s" (index .Resources.configMapTestIntermediate2 2).data.valueIntermediate | upper }}
`,
					},
				},
			},
			HTTPRouteTemplate: gwcapi.ResourceSpec{
				ResourceTemplate: gwcapi.ResourceTemplate{
					ResourceTemplates: map[string]string{
						"shadowHttproute": `apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: {{ .HTTPRoute.metadata.name }}-istio
  namespace: {{ .HTTPRoute.metadata.namespace }}
spec:
  parentRefs:
  {{ range .HTTPRoute.spec.parentRefs }}
  - kind: {{ .kind }}
    name: {{ .name }}-istio
    namespace: {{ .namespace }}
  {{ end }}
  rules:
  {{ toYaml .HTTPRoute.spec.rules | nindent 4 }}
`,
					},
				},
			},
		},
	}
}

// newTestBlueprintNonReady returns a GatewayClassBlueprint with templates that
// reference each other but have a missing value reference to simulate a
// non-ready child resource
func newTestBlueprintNonReady() *gwcapi.GatewayClassBlueprint {
	return &gwcapi.GatewayClassBlueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "default-gateway-class"},
		Spec: gwcapi.GatewayClassBlueprintSpec{
			GatewayTemplate: gwcapi.ResourceSpec{
				ResourceTemplate: gwcapi.ResourceTemplate{
					ResourceTemplates: map[string]string{
						"configMapTestSource": tmplConfigMapSource,
						"configMapTestIntermediate1": `apiVersion: v1
kind: ConfigMap
metadata:
  name: intermediate1-configmap
  namespace: {{ .Gateway.metadata.namespace }}
data:
  valueIntermediate: {{ (index .Resources.configMapTestSource 0).data.valueToRead1NonExisting }}
`,
					},
				},
			},
		},
	}
}

// ------------------------------------
// [combineHostnames] tests
// ------------------------------------

var _ = Describe("combineHostnames", func() {
	// gwWith is a helper to create a Gateway with listeners having the given hostnames
	gwWith := func(hostnames ...string) *gatewayapi.Gateway {
		gw := &gatewayapi.Gateway{}
		for _, h := range hostnames {
			hn := gatewayapi.Hostname(h)
			gw.Spec.Listeners = append(gw.Spec.Listeners, gatewayapi.Listener{
				Name:     gatewayapi.SectionName("l"),
				Hostname: &hn,
			})
		}
		return gw
	}

	// rtWith is a helper to create an HTTPRoute with the given hostnames
	rtWith := func(ns string, hostnames ...string) *gatewayapi.HTTPRoute { //nolint:unparam // ns is always "default" in tests but kept for clarity
		rt := &gatewayapi.HTTPRoute{}
		rt.Namespace = ns
		for _, h := range hostnames {
			rt.Spec.Hostnames = append(rt.Spec.Hostnames, gatewayapi.Hostname(h))
		}
		return rt
	}

	It("Should return empty slices (nil) when no hostnames", func() {
		gw := &gatewayapi.Gateway{
			Spec: gatewayapi.GatewaySpec{
				Listeners: []gatewayapi.Listener{{Name: "l"}},
			},
		}
		union, isect := combineHostnames(gw, nil)
		Expect(union).To(BeNil())
		Expect(isect).To(BeNil())
	})

	It("Should return a single hostname in both union and intersection", func() {
		gw := gwWith("example.com")
		union, isect := combineHostnames(gw, nil)
		Expect(union).To(ConsistOf("example.com"))
		Expect(isect).To(ConsistOf("example.com"))
	})

	It("Should deduplicate identical hostnames from listener and route", func() {
		gw := gwWith("example.com")
		rt := rtWith("default", "example.com")
		union, isect := combineHostnames(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(union).To(ConsistOf("example.com"))
		Expect(isect).To(ConsistOf("example.com"))
	})

	It("Should exclude route hostname from intersection when covered by wildcard", func() {
		gw := gwWith("*.example.com")
		rt := rtWith("default", "foo.example.com")
		union, isect := combineHostnames(gw, []*gatewayapi.HTTPRoute{rt})
		sort.Strings(union)
		Expect(union).To(ConsistOf("*.example.com", "foo.example.com"))
		Expect(isect).To(ConsistOf("*.example.com"))
	})

	It("Should keep non-covered hostname in intersection", func() {
		gw := gwWith("*.example.com")
		rt := rtWith("default", "other.org")
		union, isect := combineHostnames(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(union).To(ConsistOf("*.example.com", "other.org"))
		Expect(isect).To(ConsistOf("*.example.com", "other.org"))
	})

	It("Should handle multiple wildcards and mixed hostnames", func() {
		gw := gwWith("*.example.com", "*.test.io")
		rt := rtWith("default", "foo.example.com", "bar.test.io", "other.org")
		union, isect := combineHostnames(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(union).To(ConsistOf("*.example.com", "*.test.io", "foo.example.com", "bar.test.io", "other.org"))
		Expect(isect).To(ConsistOf("*.example.com", "*.test.io", "other.org"))
	})

	It("Should include route hostnames from multiple routes", func() {
		gw := gwWith("example.com")
		rt1 := rtWith("default", "a.example.com")
		rt2 := rtWith("default", "b.example.com")
		union, isect := combineHostnames(gw, []*gatewayapi.HTTPRoute{rt1, rt2})
		Expect(union).To(ConsistOf("example.com", "a.example.com", "b.example.com"))
		Expect(isect).To(ConsistOf("example.com", "a.example.com", "b.example.com"))
	})

	It("Should handle listeners without hostname", func() {
		gw := &gatewayapi.Gateway{
			Spec: gatewayapi.GatewaySpec{
				Listeners: []gatewayapi.Listener{{Name: "l"}},
			},
		}
		rt := rtWith("default", "example.com")
		union, isect := combineHostnames(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(union).To(ConsistOf("example.com"))
		Expect(isect).To(ConsistOf("example.com"))
	})
})

// ------------------------------------
// [filterHTTPRoutesForGateway] tests
// ------------------------------------

var _ = Describe("filterHTTPRoutesForGateway", func() {
	// rtWithRef is a helper to create an HTTPRoute with a parent ref to a gateway with the given name and namespace
	rtWithRef := func(name, ns string, parentName gatewayapi.ObjectName, parentNs *gatewayapi.Namespace, parentGroup *gatewayapi.Group, parentKind *gatewayapi.Kind) *gatewayapi.HTTPRoute {
		return &gatewayapi.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: gatewayapi.HTTPRouteSpec{
				CommonRouteSpec: gatewayapi.CommonRouteSpec{
					ParentRefs: []gatewayapi.ParentReference{{
						Group:     parentGroup,
						Kind:      parentKind,
						Name:      parentName,
						Namespace: parentNs,
					}},
				},
			},
		}
	}

	gw := &gatewayapi.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "my-gw", Namespace: "default"},
	}

	It("Should match route with matching name and same namespace", func() {
		rt := rtWithRef("rt1", "default", "my-gw", nil, nil, nil)
		result := filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(result[0].Name).To(Equal("rt1"))
	})

	It("Should match route with explicit matching namespace", func() {
		ns := gatewayapi.Namespace("default")
		rt := rtWithRef("rt1", "default", "my-gw", &ns, nil, nil)
		result := filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(result).To(HaveLen(1))
	})

	It("Should not match route targeting different gateway name", func() {
		rt := rtWithRef("rt1", "default", "other-gw", nil, nil, nil)
		result := filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(result).To(BeEmpty())
	})

	It("Should not match route in different namespace (implicit)", func() {
		rt := rtWithRef("rt1", "other-ns", "my-gw", nil, nil, nil)
		result := filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(result).To(BeEmpty())
	})

	It("Should not match route with explicit wrong namespace", func() {
		ns := gatewayapi.Namespace("other-ns")
		rt := rtWithRef("rt1", "default", "my-gw", &ns, nil, nil)
		result := filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(result).To(BeEmpty())
	})

	It("Should match correct group and reject wrong group", func() {
		wrongGroup := gatewayapi.Group("wrong.group")
		rt := rtWithRef("rt1", "default", "my-gw", nil, &wrongGroup, nil)
		Expect(filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt})).To(BeEmpty())

		correctGroup := gatewayapi.Group(gatewayapi.GroupName)
		rt2 := rtWithRef("rt2", "default", "my-gw", nil, &correctGroup, nil)
		Expect(filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt2})).To(HaveLen(1))
	})

	It("Should match correct kind and reject wrong kind", func() {
		wrongKind := gatewayapi.Kind("Service")
		rt := rtWithRef("rt1", "default", "my-gw", nil, nil, &wrongKind)
		Expect(filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt})).To(BeEmpty())

		correctKind := gatewayapi.Kind("Gateway")
		rt2 := rtWithRef("rt2", "default", "my-gw", nil, nil, &correctKind)
		Expect(filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt2})).To(HaveLen(1))
	})

	It("Should filter from multiple routes keeping only matches", func() {
		rt1 := rtWithRef("match1", "default", "my-gw", nil, nil, nil)
		rt2 := rtWithRef("nomatch", "default", "other-gw", nil, nil, nil)
		rt3 := rtWithRef("match2", "default", "my-gw", nil, nil, nil)
		result := filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt1, rt2, rt3})
		Expect(result).To(HaveLen(2))
		Expect(result[0].Name).To(Equal("match1"))
		Expect(result[1].Name).To(Equal("match2"))
	})

	It("Should return empty slice for empty input", func() {
		result := filterHTTPRoutesForGateway(gw, nil)
		Expect(result).To(BeEmpty())
	})

	It("Should handle route with no parent refs", func() {
		rt := &gatewayapi.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "rt1", Namespace: "default"},
		}
		result := filterHTTPRoutesForGateway(gw, []*gatewayapi.HTTPRoute{rt})
		Expect(result).To(BeEmpty())
	})
})

// ------------------------------------
// [lookupHTTPRoutes] tests
// ------------------------------------

var _ = Describe("lookupHTTPRoutes", func() {

	It("Should return empty slice when no routes exist", func() {
		r := newFakeClient()
		routes, err := lookupHTTPRoutes(context.Background(), r)
		Expect(err).NotTo(HaveOccurred())
		Expect(routes).To(BeEmpty())
	})

	It("Should return all routes from the cluster", func() {
		rt1 := &gatewayapi.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "rt1", Namespace: "default"}}
		rt2 := &gatewayapi.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "rt2", Namespace: "other"}}
		r := newFakeClient(rt1, rt2)
		routes, err := lookupHTTPRoutes(context.Background(), r)
		Expect(err).NotTo(HaveOccurred())
		Expect(routes[0].Name).To(Equal("rt1"))
		Expect(routes[1].Name).To(Equal("rt2"))
	})
})

// ------------------------------------
// Integration tests
// ------------------------------------

var _ = Describe("Gateway controller", func() {

	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		gwc  *gatewayapi.GatewayClass
		gwcb *gwcapi.GatewayClassBlueprint
		ctx  context.Context
	)

	BeforeEach(func() {
		// Before each test, create a shared default GatewayClass and a shared
		// GatewayClassBlueprint that the test Gateways will reference.
		ctx = context.Background()
		gwc = newTestGatewayClass()
		Expect(k8sClient.Create(ctx, gwc)).Should(Succeed())
		gwcb = newTestBlueprint()
		Expect(k8sClient.Create(ctx, gwcb)).Should(Succeed())
	})

	AfterEach(func() {
		// After each test, clean up the created GatewayClass and
		// GatewayClassBlueprint to avoid interference with other tests.
		Expect(k8sClient.Delete(ctx, gwc)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, gwcb)).Should(Succeed())
	})

	When("Reconciling a parent Gateway", func() {
		var gw *gatewayapi.Gateway

		BeforeEach(func() {
			// Create a Gateway that will be used in the "Reconciling a parent
			// Gateway" tests. This Gateway references the shared default
			// GatewayClass and should trigger the creation of a child Gateway
			// and the ConfigMaps defined in the blueprint.
			gw = newTestGateway()
			Expect(k8sClient.Create(ctx, gw)).Should(Succeed()) // create the parent Gateway
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, gw)).Should(Succeed()) // when test finishes, delete the parent Gateway and expect the child and configmaps to be garbage collected
			})
		})

		It("Should set owner reference on child gateway for garbage collection", func() {
			childGateway := &gatewayapi.Gateway{}
			gwChildNN := types.NamespacedName{Name: gw.ObjectMeta.Name + "-istio", Namespace: gw.ObjectMeta.Namespace}

			// Wait for child gateway to exist and have the correct owner
			// reference pointing to the parent gateway for garbage collection
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, gwChildNN, childGateway); err != nil {
					return false
				}
				for _, ref := range childGateway.ObjectMeta.OwnerReferences {
					if ref.UID == gw.ObjectMeta.GetUID() {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			t := true
			expectedOwnerReference := metav1.OwnerReference{
				Kind:               "Gateway",
				APIVersion:         "gateway.networking.k8s.io/v1",
				UID:                gw.ObjectMeta.GetUID(),
				Name:               gw.ObjectMeta.Name,
				Controller:         &t,
				BlockOwnerDeletion: &t,
			}
			Expect(childGateway.ObjectMeta.OwnerReferences).To(ContainElement(expectedOwnerReference))
		})

		It("Should set Ready=false when child gateway is not ready", func() {
			gwNN := types.NamespacedName{Name: gw.ObjectMeta.Name, Namespace: gw.ObjectMeta.Namespace}
			gwChildNN := types.NamespacedName{Name: gw.ObjectMeta.Name + "-istio", Namespace: gw.ObjectMeta.Namespace}

			// Wait for child to exist
			Eventually(func() bool {
				return k8sClient.Get(ctx, gwChildNN, &gatewayapi.Gateway{}) == nil
			}, timeout, interval).Should(BeTrue())

			Expect(setGatewayStatus(gwChildNN, &metav1.Condition{
				Type:   string(gatewayapi.GatewayConditionReady),
				Status: metav1.ConditionFalse,
				//nolint:staticcheck // GatewayReasonReady is deprecated but still used by the upstream API
				Reason: string(gatewayapi.GatewayReasonReady)}, nil)).Should(Succeed())
			time.Sleep(5 * time.Second)

			gwRead := &gatewayapi.Gateway{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, gwNN, gwRead)
				if err != nil {
					return false
				}
				if kubernetes.ConditionsHaveLatestObservedGeneration(gwRead, gwRead.Status.Conditions) != nil {
					return false
				}
				return conditionStateIs(gwRead, "Ready", PtrTo(metav1.ConditionFalse), nil, nil) &&
					conditionStateIs(gwRead, "Programmed", PtrTo(metav1.ConditionTrue), nil, nil)
			}, 5*time.Second, interval).Should(BeTrue())
		})

		It("Should set Ready=true and propagate address when child gateway is ready", func() {
			gwNN := types.NamespacedName{Name: gw.ObjectMeta.Name, Namespace: gw.ObjectMeta.Namespace}
			gwChildNN := types.NamespacedName{Name: gw.ObjectMeta.Name + "-istio", Namespace: gw.ObjectMeta.Namespace}

			// Wait for child to exist
			Eventually(func() bool {
				return k8sClient.Get(ctx, gwChildNN, &gatewayapi.Gateway{}) == nil
			}, timeout, interval).Should(BeTrue())

			addrType := gatewayapi.IPAddressType
			Expect(setGatewayStatus(gwChildNN, &metav1.Condition{
				Type:   string(gatewayapi.GatewayConditionReady),
				Status: metav1.ConditionTrue,
				//nolint:staticcheck // GatewayReasonReady is deprecated but still used by the upstream API
				Reason: string(gatewayapi.GatewayReasonReady)},
				&gatewayapi.GatewayStatusAddress{Type: &addrType, Value: "4.5.6.7"})).Should(Succeed())

			gwRead := &gatewayapi.Gateway{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, gwNN, gwRead)
				if err != nil {
					return false
				}
				if kubernetes.ConditionsHaveLatestObservedGeneration(gwRead, gwRead.Status.Conditions) != nil {
					return false
				}
				return conditionStateIs(gwRead, "Ready", PtrTo(metav1.ConditionTrue), nil, nil) &&
					conditionStateIs(gwRead, "Programmed", PtrTo(metav1.ConditionTrue), nil, nil)
			}, timeout, interval).Should(BeTrue())
		})

		It("Should resolve inter resource-references in ConfigMap chain", func() {
			cm := corev1.ConfigMap{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "dst-configmap", Namespace: "default"}, &cm)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(cm.Data["valueRead"]).To(Equal("HELLO, WORLD"))
			Expect(cm.Data["valueRead2"]).To(Equal("TESTING, ONE TWO HELLO-THREE"))
		})

		It("Should set Accepted=true on the parent gateway", func() {
			gwNN := types.NamespacedName{Name: gw.ObjectMeta.Name, Namespace: gw.ObjectMeta.Namespace}
			gwRead := &gatewayapi.Gateway{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, gwNN, gwRead)
				if err != nil {
					return false
				}
				return conditionStateIs(gwRead, "Accepted", PtrTo(metav1.ConditionTrue), nil, nil)
			}, timeout, interval).Should(BeTrue())
		})
	})

	When("Blueprint produces a resource that cannot render", func() {
		BeforeEach(func() {
			// Override the blueprint with the non-ready variant
			Expect(k8sClient.Delete(ctx, gwcb)).Should(Succeed())
			gwcb = newTestBlueprintNonReady()
			Expect(k8sClient.Create(ctx, gwcb)).Should(Succeed())
		})

		It("Should report Programmed=false with missing resource message", func() {
			gw := newTestGateway()
			Expect(k8sClient.Create(ctx, gw)).Should(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, gw)).Should(Succeed())
			})

			gwNN := types.NamespacedName{Name: gw.ObjectMeta.Name, Namespace: gw.ObjectMeta.Namespace}
			gwRead := &gatewayapi.Gateway{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, gwNN, gwRead)
				if err != nil {
					return false
				}
				if kubernetes.ConditionsHaveLatestObservedGeneration(gwRead, gwRead.Status.Conditions) != nil {
					return false
				}
				return conditionStateIs(gwRead, "Accepted", PtrTo(metav1.ConditionTrue), nil, nil) &&
					conditionStateIs(gwRead, "Ready", PtrTo(metav1.ConditionFalse), nil, nil) &&
					conditionStateIs(gwRead, "Programmed", PtrTo(metav1.ConditionFalse), PtrTo("Pending"), PtrTo("missing 1 resources: configMapTestIntermediate1\\[\\]"))
			}, 5*time.Second, interval).Should(BeTrue())
		})
	})
})
