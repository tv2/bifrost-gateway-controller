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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
)

// ------------------------------------
// Test Helpers
// ------------------------------------

// newGatewayClassWithBlueprint returns a GatewayClass named "default-gateway-class".
// This GatewayClass has a ParametersRef that points to a GatewayClassBlueprint named
func newGatewayClassWithBlueprint() *gatewayapi.GatewayClass {
	return &gatewayapi.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "cloud-gw"},
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

// newGatewayClassNoParams returns a GatewayClass named "cloud-gw-invalid" that
// has no ParametersRef. This is invalid because our controller requires a
// ParametersRef to a GatewayClassBlueprint in order to function, so we expect
// the controller to mark this GatewayClass as invalid when it processes it.
func newGatewayClassNoParams() *gatewayapi.GatewayClass {
	return &gatewayapi.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "cloud-gw-invalid"},
		Spec: gatewayapi.GatewayClassSpec{
			ControllerName: "github.com/tv2/bifrost-gateway-controller",
		},
	}
}

// newGatewayClassNotOurs returns a GatewayClass named "not-our-gatewayclass"
// that specifies a controller name that does not match our controller. This is
// used to test that our controller correctly ignores GatewayClasses that it
// does not own, and does not mark them as accepted or invalid.
func newGatewayClassNotOurs() *gatewayapi.GatewayClass {
	return &gatewayapi.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "not-our-gatewayclass"},
		Spec: gatewayapi.GatewayClassSpec{
			ControllerName: "github.com/acme/bifrost-gateway-controller",
		},
	}
}

// newMinimalBlueprint returns a minimal valid GatewayClassBlueprint that can be
// used in tests.
func newMinimalBlueprint() *gwcapi.GatewayClassBlueprint {
	return &gwcapi.GatewayClassBlueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "default-gateway-class"},
		Spec: gwcapi.GatewayClassBlueprintSpec{
			GatewayTemplate: gwcapi.ResourceSpec{
				ResourceTemplate: gwcapi.ResourceTemplate{
					ResourceTemplates: map[string]string{
						"istioShadowGw": `apiVersion: gateway.networking.k8s.io/v1beta1
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
`,
					},
				},
			},
		},
	}
}

// conditionByType returns the first condition matching the given type, or nil.
func conditionByType(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// ------------------------------------
// [GatewayClassReconciler] accessor tests
// ------------------------------------

var _ = Describe("GatewayClassReconciler", func() {

	It("Should return the client it was constructed with", func() {
		cl := newFakeClient().Client()
		r := &GatewayClassReconciler{client: cl}
		Expect(r.Client()).To(Equal(cl))
	})

	It("Should return the scheme it was constructed with", func() {
		s := runtime.NewScheme()
		r := &GatewayClassReconciler{scheme: s}
		Expect(r.Scheme()).To(Equal(s))
	})
})

// ------------------------------------
// Integration tests
// ------------------------------------

var _ = Describe("GatewayClass controller", func() {

	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	ctx := context.Background()

	When("A gatewayclass we own is created", func() {
		var gwc *gatewayapi.GatewayClass
		var gwcb *gwcapi.GatewayClassBlueprint

		BeforeEach(func() {
			gwcb = newMinimalBlueprint()
			Expect(k8sClient.Create(ctx, gwcb)).Should(Succeed())
			gwc = newGatewayClassWithBlueprint()
			Expect(k8sClient.Create(ctx, gwc)).Should(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, gwc)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, gwcb)).Should(Succeed())
			})
		})

		It("Should be marked as accepted", func() {
			nn := types.NamespacedName{Name: gwc.Name}
			fetched := &gatewayapi.GatewayClass{}

			// Use Eventually here to wait for the controller to process the
			// newly created GatewayClass and update its status with the
			// Accepted condition. This is necessary because the controller
			// processes resources asynchronously.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
				cond := conditionByType(fetched.Status.Conditions, string(gatewayapi.GatewayClassConditionStatusAccepted))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(string(gatewayapi.GatewayClassReasonAccepted)))
			}, timeout, interval).Should(Succeed())
		})
	})

	When("An invalid gatewayclass we own is created", func() {
		var gwc *gatewayapi.GatewayClass

		BeforeEach(func() {
			gwc = newGatewayClassNoParams()
			Expect(k8sClient.Create(ctx, gwc)).Should(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, gwc)).Should(Succeed())
			})
		})

		It("Should be marked as invalid", func() {
			nn := types.NamespacedName{Name: gwc.Name}
			fetched := &gatewayapi.GatewayClass{}

			// Use Eventually here to wait for the controller to process the
			// newly created invalid GatewayClass and update its status with the
			// Accepted=false condition and the appropriate reason. This is
			// necessary because the controller processes resources
			// asynchronously.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
				cond := conditionByType(fetched.Status.Conditions, string(gatewayapi.GatewayClassConditionStatusAccepted))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(gatewayapi.GatewayClassReasonInvalidParameters)))
			}, timeout, interval).Should(Succeed())
		})
	})

	When("A gatewayclass we do not own is created", func() {
		var gwc *gatewayapi.GatewayClass

		BeforeEach(func() {
			gwc = newGatewayClassNotOurs()
			Expect(k8sClient.Create(ctx, gwc)).Should(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, gwc)).Should(Succeed())
			})
		})

		It("Should not be marked as accepted", func() {
			nn := types.NamespacedName{Name: gwc.Name}
			fetched := &gatewayapi.GatewayClass{}

			// Use Eventually here to wait for the controller to process the
			// newly created GatewayClass that we do not own and check that it
			// is not marked as accepted. Since the controller should ignore
			// GatewayClasses that do not specify our controller name, we expect
			// that either there will be no Accepted condition at all, or if the
			// controller does set an Accepted condition for some reason, it
			// will remain in the Unknown state since the controller is not
			// actively processing it. This test ensures that the controller is
			// correctly ignoring resources that it does not own.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
				cond := conditionByType(fetched.Status.Conditions, string(gatewayapi.GatewayClassConditionStatusAccepted))
				// Either no condition at all, or still Unknown
				if cond != nil {
					g.Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
				}
			}, timeout, interval).Should(Succeed())
		})
	})
})
