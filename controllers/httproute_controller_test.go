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
	selfapi "github.com/tv2/bifrost-gateway-controller/pkg/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
)

// ------------------------------------
// [derefCmp] tests
// ------------------------------------

var _ = Describe("derefCmp", func() {

	It("Should return true when both arguments are nil", func() {
		Expect(derefCmp[string](nil, nil)).To(BeTrue())
	})

	It("Should return false when only first argument is nil", func() {
		b := PtrTo("x")
		Expect(derefCmp(nil, b)).To(BeFalse())
	})

	It("Should return false when only second argument is nil", func() {
		a := PtrTo("x")
		Expect(derefCmp(a, nil)).To(BeFalse())
	})

	It("Should return true when both values are equal", func() {
		Expect(derefCmp(PtrTo(42), PtrTo(42))).To(BeTrue())
	})

	It("Should return false when both values differ", func() {
		Expect(derefCmp(PtrTo(1), PtrTo(2))).To(BeFalse())
	})
})

// ------------------------------------
// [parentRefCmp] tests
// ------------------------------------

var _ = Describe("parentRefCmp", func() {

	// baseRef returns a sample ParentReference with all fields set. We can use
	// this as a starting point for our tests, and modify individual fields to
	// test how parentRefCmp handles differences in each field.
	baseRef := func() gatewayapi.ParentReference {
		return gatewayapi.ParentReference{
			Group:       PtrTo(gatewayapi.Group(gatewayapi.GroupName)),
			Kind:        PtrTo(gatewayapi.Kind("Gateway")),
			Namespace:   PtrTo(gatewayapi.Namespace("default")),
			Name:        "my-gw",
			SectionName: PtrTo(gatewayapi.SectionName("https")),
			Port:        PtrTo(gatewayapi.PortNumber(443)),
		}
	}

	It("Should return true for identical refs", func() {
		a, b := baseRef(), baseRef()
		Expect(parentRefCmp(a, b)).To(BeTrue())
	})

	It("Should return false when names differ", func() {
		a, b := baseRef(), baseRef()
		b.Name = "other-gw"
		Expect(parentRefCmp(a, b)).To(BeFalse())
	})

	It("Should return false when groups differ", func() {
		a, b := baseRef(), baseRef()
		b.Group = PtrTo(gatewayapi.Group("wrong"))
		Expect(parentRefCmp(a, b)).To(BeFalse())
	})

	It("Should return false when kinds differ", func() {
		a, b := baseRef(), baseRef()
		b.Kind = PtrTo(gatewayapi.Kind("Service"))
		Expect(parentRefCmp(a, b)).To(BeFalse())
	})

	It("Should return false when namespaces differ", func() {
		a, b := baseRef(), baseRef()
		b.Namespace = PtrTo(gatewayapi.Namespace("other"))
		Expect(parentRefCmp(a, b)).To(BeFalse())
	})

	It("Should return false when sectionNames differ", func() {
		a, b := baseRef(), baseRef()
		b.SectionName = PtrTo(gatewayapi.SectionName("http"))
		Expect(parentRefCmp(a, b)).To(BeFalse())
	})

	It("Should return false when ports differ", func() {
		a, b := baseRef(), baseRef()
		b.Port = PtrTo(gatewayapi.PortNumber(80))
		Expect(parentRefCmp(a, b)).To(BeFalse())
	})

	It("Should match when both optional fields are nil", func() {
		a := gatewayapi.ParentReference{Name: "gw"}
		b := gatewayapi.ParentReference{Name: "gw"}
		Expect(parentRefCmp(a, b)).To(BeTrue())
	})

	It("Should not match when one optional field is nil and other is set", func() {
		a := gatewayapi.ParentReference{Name: "gw"}
		b := gatewayapi.ParentReference{Name: "gw", Port: PtrTo(gatewayapi.PortNumber(80))}
		Expect(parentRefCmp(a, b)).To(BeFalse())
	})
})

// ------------------------------------
// [lookupParent] tests
// ------------------------------------

var _ = Describe("lookupParent", func() {

	It("Should use HTTPRoute namespace when parentRef has no namespace", func() {
		gw := &gatewayapi.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "my-gw", Namespace: "route-ns"},
		}
		rt := &gatewayapi.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "rt1", Namespace: "route-ns"},
		}
		r := newFakeClient(gw)
		parent := gatewayapi.ParentReference{Name: "my-gw"}

		result, err := lookupParent(context.Background(), r, rt, parent)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Name).To(Equal("my-gw"))
		Expect(result.Namespace).To(Equal("route-ns"))
	})

	It("Should use explicit namespace from parentRef", func() {
		gw := &gatewayapi.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "my-gw", Namespace: "explicit-ns"},
		}
		rt := &gatewayapi.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "rt1", Namespace: "route-ns"},
		}
		r := newFakeClient(gw)
		parent := gatewayapi.ParentReference{
			Name:      "my-gw",
			Namespace: PtrTo(gatewayapi.Namespace("explicit-ns")),
		}

		result, err := lookupParent(context.Background(), r, rt, parent)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Namespace).To(Equal("explicit-ns"))
	})

	It("Should error when gateway not found", func() {
		rt := &gatewayapi.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "rt1", Namespace: "default"},
		}
		r := newFakeClient()
		parent := gatewayapi.ParentReference{Name: "nonexistent"}

		_, err := lookupParent(context.Background(), r, rt, parent)
		Expect(err).To(HaveOccurred())
	})
})

// ------------------------------------
// [findParentRouteStatus] tests
// ------------------------------------

var _ = Describe("findParentRouteStatus", func() {

	parent := gatewayapi.ParentReference{
		Kind: PtrTo(gatewayapi.Kind("Gateway")),
		Name: "my-gw",
	}

	It("Should return nil when no parents in status", func() {
		status := &gatewayapi.RouteStatus{}
		Expect(findParentRouteStatus(status, parent)).To(BeNil())
	})

	It("Should return nil when parent matches but controller differs", func() {
		status := &gatewayapi.RouteStatus{
			Parents: []gatewayapi.RouteParentStatus{{
				ParentRef:      parent,
				ControllerName: "other-controller",
			}},
		}
		Expect(findParentRouteStatus(status, parent)).To(BeNil())
	})

	It("Should return nil when controller matches but parent differs", func() {
		otherParent := gatewayapi.ParentReference{
			Kind: PtrTo(gatewayapi.Kind("Gateway")),
			Name: "other-gw",
		}
		status := &gatewayapi.RouteStatus{
			Parents: []gatewayapi.RouteParentStatus{{
				ParentRef:      otherParent,
				ControllerName: selfapi.SelfControllerName,
			}},
		}
		Expect(findParentRouteStatus(status, parent)).To(BeNil())
	})

	It("Should return matching entry", func() {
		status := &gatewayapi.RouteStatus{
			Parents: []gatewayapi.RouteParentStatus{{
				ParentRef:      parent,
				ControllerName: selfapi.SelfControllerName,
				Conditions: []metav1.Condition{{
					Type:   "Accepted",
					Status: metav1.ConditionTrue,
				}},
			}},
		}
		result := findParentRouteStatus(status, parent)
		Expect(result).NotTo(BeNil())
		Expect(result.Conditions[0].Type).To(Equal("Accepted"))
	})
})

// ------------------------------------
// [setRouteStatusCondition] tests
// ------------------------------------

var _ = Describe("setRouteStatusCondition", func() {

	parent := gatewayapi.ParentReference{
		Kind: PtrTo(gatewayapi.Kind("Gateway")),
		Name: "my-gw",
	}

	It("Should append new parent status when none exists", func() {
		status := &gatewayapi.RouteStatus{}
		setRouteStatusCondition(status, parent, &metav1.Condition{
			Type:   "Accepted",
			Status: metav1.ConditionTrue,
			Reason: "Accepted",
		})

		Expect(status.Parents[0].ControllerName).To(Equal(selfapi.SelfControllerName))
		Expect(status.Parents[0].Conditions[0].Type).To(Equal("Accepted"))
		Expect(status.Parents[0].Conditions[0].LastTransitionTime.IsZero()).To(BeFalse())
	})

	It("Should update existing condition for same parent", func() {
		status := &gatewayapi.RouteStatus{
			Parents: []gatewayapi.RouteParentStatus{{
				ParentRef:      parent,
				ControllerName: selfapi.SelfControllerName,
				Conditions: []metav1.Condition{{
					Type:   "Accepted",
					Status: metav1.ConditionFalse,
					Reason: "Pending",
				}},
			}},
		}
		setRouteStatusCondition(status, parent, &metav1.Condition{
			Type:   "Accepted",
			Status: metav1.ConditionTrue,
			Reason: "Accepted",
		})

		Expect(status.Parents[0].Conditions[0].Status).To(Equal(metav1.ConditionTrue))
	})

	It("Should add condition for different parent without affecting existing", func() {
		otherParent := gatewayapi.ParentReference{
			Kind: PtrTo(gatewayapi.Kind("Gateway")),
			Name: "other-gw",
		}
		status := &gatewayapi.RouteStatus{
			Parents: []gatewayapi.RouteParentStatus{{
				ParentRef:      otherParent,
				ControllerName: selfapi.SelfControllerName,
				Conditions: []metav1.Condition{{
					Type:   "Accepted",
					Status: metav1.ConditionTrue,
					Reason: "Accepted",
				}},
			}},
		}
		setRouteStatusCondition(status, parent, &metav1.Condition{
			Type:   "Accepted",
			Status: metav1.ConditionTrue,
			Reason: "Accepted",
		})

		Expect(status.Parents).To(HaveLen(2))
	})

	It("Should preserve provided LastTransitionTime", func() {
		status := &gatewayapi.RouteStatus{}
		fixedTime := metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		setRouteStatusCondition(status, parent, &metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionTrue,
			Reason:             "Accepted",
			LastTransitionTime: fixedTime,
		})

		Expect(status.Parents[0].Conditions[0].LastTransitionTime).To(Equal(fixedTime))
	})
})

// ====================================================================
// Integration tests
// ====================================================================

var _ = Describe("HTTPRoute controller", func() {

	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		gwc  *gatewayapi.GatewayClass
		gwcb *gwcapi.GatewayClassBlueprint
		gw   *gatewayapi.Gateway
		ctx  context.Context
	)

	BeforeEach(func() {
		// Before each test, we create a GatewayClass and Gateway that the
		// HTTPRoute will reference. This setup is necessary for the HTTPRoute
		// controller to process the route and update its status based on the
		// parent gateway's status.
		ctx = context.Background()
		gwc = newTestGatewayClass()
		Expect(k8sClient.Create(ctx, gwc)).Should(Succeed())
		gwcb = newTestBlueprint()
		Expect(k8sClient.Create(ctx, gwcb)).Should(Succeed())
		gw = newTestGateway()
		Expect(k8sClient.Create(ctx, gw)).Should(Succeed())
		DeferCleanup(func() { // Clean up the created resources after each test to ensure isolation between tests
			Expect(k8sClient.Delete(ctx, gw)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, gwcb)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, gwc)).Should(Succeed())
		})
	})

	When("An HTTPRoute targeting our Gateway is created", func() {
		var rt *gatewayapi.HTTPRoute

		// Before each test in this block, we create an HTTPRoute that
		// references the Gateway we set up in the outer BeforeEach. This allows
		// us to test how the HTTPRoute controller processes a route that
		// targets a gateway it manages, and how it updates the route's status
		// based on the parent gateway's status.
		BeforeEach(func() {
			gwKind := gatewayapi.Kind("Gateway")
			gwGroup := gatewayapi.Group(gatewayapi.GroupName)
			gwNs := gatewayapi.Namespace(gw.Namespace)
			rt = &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{{
							Group:     &gwGroup,
							Kind:      &gwKind,
							Name:      gatewayapi.ObjectName(gw.Name),
							Namespace: &gwNs,
						}},
					},
					Rules: []gatewayapi.HTTPRouteRule{{
						BackendRefs: []gatewayapi.HTTPBackendRef{{
							BackendRef: gatewayapi.BackendRef{
								BackendObjectReference: gatewayapi.BackendObjectReference{
									Name: "my-service",
									Port: PtrTo(gatewayapi.PortNumber(8080)),
								},
							},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, rt)).Should(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, rt)).Should(Succeed())
			})
		})

		It("Should set Accepted=true status for the parent gateway", func() {
			nn := types.NamespacedName{Name: rt.Name, Namespace: rt.Namespace}
			fetched := &gatewayapi.HTTPRoute{}

			// Use Eventually here to wait for the controller to process the
			// newly created HTTPRoute and update its status with the parent
			// status indicating that it is accepted by the gateway. This is
			// necessary because the controller processes resources
			// asynchronously.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
				g.Expect(fetched.Status.Parents).NotTo(BeEmpty())
				found := false
				for _, ps := range fetched.Status.Parents {
					if ps.ControllerName == selfapi.SelfControllerName &&
						ps.ParentRef.Name == gatewayapi.ObjectName(gw.Name) {
						cond := conditionByType(ps.Conditions, string(gatewayapi.RouteConditionAccepted))
						g.Expect(cond).NotTo(BeNil())
						g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
						found = true
					}
				}
				g.Expect(found).To(BeTrue(), "expected parent status for our controller")
			}, timeout, interval).Should(Succeed())
		})

		It("Should create shadow HTTPRoute from blueprint template", func() {
			shadowNN := types.NamespacedName{
				Name:      rt.Name + "-istio",
				Namespace: rt.Namespace,
			}
			shadowRT := &gatewayapi.HTTPRoute{}

			// Use Eventually here to wait for the controller to process the
			// newly created HTTPRoute and create the corresponding shadow route
			// based on the blueprint template. This is necessary because the
			// controller processes resources asynchronously.
			Eventually(func() error {
				return k8sClient.Get(ctx, shadowNN, shadowRT)
			}, timeout, interval).Should(Succeed())

			// Verify the shadow route's parentRef points to the shadow gateway
			Expect(shadowRT.Spec.ParentRefs).NotTo(BeEmpty())
		})
	})

	When("An HTTPRoute targets a non-Gateway kind", func() {
		It("Should not set any parent status for that ref", func() {
			svcKind := gatewayapi.Kind("Service")
			rt := &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-route",
					Namespace: "default",
				},
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{{
							Kind: &svcKind,
							Name: "some-service",
						}},
					},
				},
			}
			// Expect the route to be created successfully, but since the
			// controller only processes parentRefs that point to Gateways, it
			// should not set any status for this route.
			Expect(k8sClient.Create(ctx, rt)).Should(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, rt)).Should(Succeed())
			})

			nn := types.NamespacedName{Name: rt.Name, Namespace: rt.Namespace}
			fetched := &gatewayapi.HTTPRoute{}

			// Give the controller time to process, then verify no status was
			// set. We use Consistently here to check that the status remains
			// unchanged over a period of time, confirming that the controller
			// is not updating it.
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
				for _, ps := range fetched.Status.Parents {
					g.Expect(ps.ControllerName).NotTo(Equal(selfapi.SelfControllerName))
				}
			}, 3*time.Second, interval).Should(Succeed())
		})
	})
})
