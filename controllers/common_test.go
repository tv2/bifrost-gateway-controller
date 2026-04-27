package controllers

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gwcapi "github.com/tv2/bifrost-gateway-controller/apis/gateway.tv2.dk/v1alpha1"
	selfapi "github.com/tv2/bifrost-gateway-controller/pkg/api"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// ------------------------------------
// Test Helpers
// ------------------------------------

// jsonRaw is a helper taking a JSON string and returns a JSON struct pointer.
func jsonRaw(s string) *apiextensionsv1.JSON {
	return &apiextensionsv1.JSON{Raw: []byte(s)}
}

// nsPtr is a helper taking a string and returns a gatewayapi.Namespace pointer.
func nsPtr(s string) *gatewayapi.Namespace {
	ns := gatewayapi.Namespace(s)
	return &ns
}

// fakeControllerClient is a stub that implements the ControllerClient interface
// by embedding a controller-runtime fake client and scheme.
type fakeControllerClient struct {
	cl     client.Client
	scheme *runtime.Scheme
}

// Client() and Scheme() implement ControllerClient interface by returning the
// embedded fake client and scheme
func (f *fakeControllerClient) Client() client.Client   { return f.cl }
func (f *fakeControllerClient) Scheme() *runtime.Scheme { return f.scheme }

// newFakeClient creates a new fakeControllerClient with the given objects. An
// object could be a GatewayClass, GatewayClassBlueprint, GatewayClassConfig, or
// GatewayConfig.
func newFakeClient(objs ...client.Object) *fakeControllerClient {
	s := runtime.NewScheme()
	utilruntime.Must(gwcapi.AddToScheme(s))
	utilruntime.Must(gatewayapi.Install(s))
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
	return &fakeControllerClient{cl: cl, scheme: s}
}

// fakeDynControllerClient extends fakeControllerClient with a dynamic client.
type fakeDynControllerClient struct {
	*fakeControllerClient
	dyn dynamic.Interface
}

// DynamicClient() implements ControllerClient interface by returning the
// embedded dynamic client
func (f *fakeDynControllerClient) DynamicClient() dynamic.Interface { return f.dyn }

// newFakeDynClient creates a new fakeDynControllerClient with the given objects
// with a reactor that allows ApplyPatchType for unstructured objects.
func newFakeDynClient(objs ...client.Object) *fakeDynControllerClient { //nolint:unparam // objs is variadic for convenience
	fc := newFakeClient(objs...)
	dyn := fakedynamic.NewSimpleDynamicClient(fc.scheme)
	// The default fake tracker requires objects to exist before Apply;
	// intercept ApplyPatchType and return the deserialized object directly.
	dyn.PrependReactor("patch", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		pa, _ := action.(ktesting.PatchAction)
		if pa.GetPatchType() != k8stypes.ApplyPatchType {
			return false, nil, nil
		}
		obj := &unstructured.Unstructured{}
		if err := json.Unmarshal(pa.GetPatch(), &obj.Object); err != nil {
			return true, nil, err
		}
		return true, obj, nil
	})
	return &fakeDynControllerClient{fakeControllerClient: fc, dyn: dyn}
}

// newFakeClientWithMapper creates a fake client with a REST mapper that knows
// about Gateway resources. Needed by functions that resolve GVK to GVR.
func newFakeClientWithMapper() *fakeControllerClient {
	s := runtime.NewScheme()
	utilruntime.Must(gwcapi.AddToScheme(s))
	utilruntime.Must(gatewayapi.Install(s))
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Group: "gateway.networking.k8s.io", Version: "v1"},
	})
	mapper.AddSpecific(
		schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "Gateway"},
		schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"},
		schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"},
		apimeta.RESTScopeNamespace,
	)
	cl := fake.NewClientBuilder().WithScheme(s).WithRESTMapper(mapper).Build()
	return &fakeControllerClient{cl: cl, scheme: s}
}

// ------------------------------------
// [isOurGatewayClass] tests
// ------------------------------------

var _ = Describe("isOurGatewayClass", func() {
	It("Should return true for matching controller (matched by ControllerName)", func() {
		gwc := &gatewayapi.GatewayClass{
			Spec: gatewayapi.GatewayClassSpec{ControllerName: selfapi.SelfControllerName},
		}
		Expect(isOurGatewayClass(gwc)).To(BeTrue())
	})

	It("Should return false for different controller (mismatched ControllerName)", func() {
		gwc := &gatewayapi.GatewayClass{
			Spec: gatewayapi.GatewayClassSpec{ControllerName: "example.com/other-controller"},
		}
		Expect(isOurGatewayClass(gwc)).To(BeFalse())
	})

	It("Should return false for empty controller (empty ControllerName)", func() {
		gwc := &gatewayapi.GatewayClass{
			Spec: gatewayapi.GatewayClassSpec{ControllerName: ""},
		}
		Expect(isOurGatewayClass(gwc)).To(BeFalse())
	})
})

// ------------------------------------
// [lookupGatewayClass] tests
// ------------------------------------

var _ = Describe("lookupGatewayClass", func() {
	ctx := context.Background()

	It("Should return the GatewayClass when found", func() {
		gwc := &gatewayapi.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
			Spec:       gatewayapi.GatewayClassSpec{ControllerName: selfapi.SelfControllerName},
		}
		r := newFakeClient(gwc)
		result, err := lookupGatewayClass(ctx, r, "test-class")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Name).To(Equal("test-class"))
	})

	It("Should return an error when not found", func() {
		r := newFakeClient()
		_, err := lookupGatewayClass(ctx, r, "missing")
		Expect(err).To(HaveOccurred())
	})
})

// ------------------------------------
// [lookupGatewayClassBlueprint] tests
// ------------------------------------

var _ = Describe("lookupGatewayClassBlueprint", func() {
	ctx := context.Background()

	It("Should return the blueprint for a valid reference", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "test-bp"},
		}
		r := newFakeClient(gwcb) // preload fake client with a created blueprint
		gwc := &gatewayapi.GatewayClass{
			Spec: gatewayapi.GatewayClassSpec{
				ParametersRef: &gatewayapi.ParametersReference{
					Group: "gateway.tv2.dk",
					Kind:  "GatewayClassBlueprint",
					Name:  "test-bp",
				},
			},
		}
		result, err := lookupGatewayClassBlueprint(ctx, r, gwc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Name).To(Equal("test-bp"))
	})

	It("Should error when ParametersRef is nil", func() {
		r := newFakeClient() // no blueprints created
		gwc := &gatewayapi.GatewayClass{
			Spec: gatewayapi.GatewayClassSpec{},
		}
		_, err := lookupGatewayClassBlueprint(ctx, r, gwc)
		Expect(err).To(HaveOccurred())
	})

	It("Should error for wrong ParametersRef Kind", func() {
		r := newFakeClient() // no blueprints created
		gwc := &gatewayapi.GatewayClass{
			Spec: gatewayapi.GatewayClassSpec{
				ParametersRef: &gatewayapi.ParametersReference{
					Group: "gateway.tv2.dk",
					Kind:  "WrongKind",
					Name:  "banana",
				},
			},
		}
		_, err := lookupGatewayClassBlueprint(ctx, r, gwc)
		Expect(err).To(HaveOccurred())
	})

	It("Should error for wrong ParametersRef Group", func() {
		r := newFakeClient() // no blueprints created
		gwc := &gatewayapi.GatewayClass{
			Spec: gatewayapi.GatewayClassSpec{
				ParametersRef: &gatewayapi.ParametersReference{
					Group: "wrong.group",
					Kind:  "GatewayClassBlueprint",
					Name:  "banana",
				},
			},
		}
		_, err := lookupGatewayClassBlueprint(ctx, r, gwc)
		Expect(err).To(HaveOccurred())
	})

	It("Should error when blueprint is not found", func() {
		r := newFakeClient() // no blueprints created
		gwc := &gatewayapi.GatewayClass{
			Spec: gatewayapi.GatewayClassSpec{
				ParametersRef: &gatewayapi.ParametersReference{
					Group: "gateway.tv2.dk",
					Kind:  "GatewayClassBlueprint",
					Name:  "banana",
				},
			},
		}
		_, err := lookupGatewayClassBlueprint(ctx, r, gwc)
		Expect(err).To(HaveOccurred())
	})
})

// ------------------------------------
// [merge] tests
// ------------------------------------

var _ = Describe("merge", func() {
	It("Should overwrite scalar values from 'b' into 'a'", func() {
		a := map[string]any{"key": "val-a"}
		b := map[string]any{"key": "val-b"}
		result, ok := merge(a, b).(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(result["key"]).To(Equal("val-b")) // Expect value from b to overwrite value from a
	})

	It("Should add new keys from 'b'", func() {
		a := map[string]any{"key1": "val1"}
		b := map[string]any{"key2": "val2"}
		result, ok := merge(a, b).(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(result["key1"]).To(Equal("val1")) // Expect existing key (key1) to remain unchanged
		Expect(result["key2"]).To(Equal("val2")) // Expect new key (key2) from b to be added
	})

	It("Should merge nested maps", func() {
		a := map[string]any{"metadata": map[string]any{"onlyInA": "1", "shared": "2"}}
		b := map[string]any{"metadata": map[string]any{"shared": "3", "onlyInB": "4"}}
		result, ok := merge(a, b).(map[string]any)
		Expect(ok).To(BeTrue())
		metadata, ok := result["metadata"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(metadata["onlyInA"]).To(Equal("1")) // Expect existing key to remain unchanged
		Expect(metadata["shared"]).To(Equal("3"))  // Expect shared key to be overwritten by b
		Expect(metadata["onlyInB"]).To(Equal("4")) // Expect new key from b to be added
	})

	It("Should preserve 'a' on type conflict (map vs scalar)", func() {
		a := map[string]any{"key": map[string]any{"nested": "value"}}
		b := map[string]any{"key": "scalar"}
		result, ok := merge(a, b).(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(result["key"]).To(BeAssignableToTypeOf(map[string]any{})) // Expect 'a' to be preserved as 'b' has a conflicting type
	})
})

// ------------------------------------
// [lookupValues] tests
//
// Specificity (from least specific to most specific):
//  1. Blueprint                 (Blueprint)    - cluster-wide baseline values, applies to all GatewayClasses (least specific)
//  2. Global GatewayClassConfig (GatewayClass) - lives in controller namespace, targets a specific GatewayClass across all namespaces
//  3. Local GatewayClassConfig  (Namespace)    - lives in gateway namespace, targets all GatewayClasses in that namespace
//  4. Local GatewayClassConfig  (GatewayClass) - lives in gateway namespace, targets a specific GatewayClass in that namespace
//  5. GatewayConfig             (Namespace)    - lives in gateway namespace, targets all Gateways in that namespace
//  6. GatewayConfig             (Gateway)      - lives in gateway namespace, targets a specific Gateway by name (most specific)
//
// The default/override distinction flips who wins:
// - Defaults: more specific wins (Gateway > Namespace > GatewayClass > Blueprint)
// - Overrides: less specific wins (Blueprint > GatewayClass > Namespace > Gateway) — lets platform operators enforce constraints
//
// Tests are ordered by topic:
//  1. Individual policy levels (blueprint, global GatewayClassConfig, local GatewayClassConfig, GatewayConfig)
//  2. Filtering (wrong targetRef)
//  3. Specificity between policy levels
//  4. Conflict resolution (multiple policies at same level)
//  5. Nested value merging
//  6. Combined policy levels
//
// See https://gateway-api.sigs.k8s.io/references/policy-attachment/#hierarchy
// See https://gateway-api.sigs.k8s.io/geps/gep-713/#established-and-challenger-policy-specs
var _ = Describe("lookupValues", func() {
	ctx := context.Background()

	BeforeEach(func() {
		ControllerNamespace = "controller-system"
	})

	// --- 1. Individual policy levels ---

	It("Should return blueprint defaults when no policies exist", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
			Spec: gwcapi.GatewayClassBlueprintSpec{
				Values: gwcapi.TemplateValues{
					Default: jsonRaw(`{"key": "bp-default"}`),
				},
			},
		}
		r := newFakeClient() // no policies
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["key"]).To(Equal("bp-default"))
	})

	It("Should let blueprint override always win", func() {
		// GatewayClassBlueprint override should always win
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
			Spec: gwcapi.GatewayClassBlueprintSpec{
				Values: gwcapi.TemplateValues{
					Override: jsonRaw(`{"key": "bp-override"}`),
				},
			},
		}
		// Competing GatewayClassConfig override for the same key and should be ignored
		gwcc := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "global", Namespace: "controller-system"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Override: jsonRaw(`{"key": "gwcc-override"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "test-class",
				},
			},
		}
		r := newFakeClient(gwcc) // preload fake client with a created GatewayClassConfig
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["key"]).To(Equal("bp-override"))
	})

	It("Should let global GatewayClassConfig default overwrite blueprint defaults", func() {
		// GatewayClassBlueprint default should be overwritten by
		// GatewayClassConfig default as it is more specific
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
			Spec: gwcapi.GatewayClassBlueprintSpec{
				Values: gwcapi.TemplateValues{
					Default: jsonRaw(`{"key": "bp-default"}`),
				},
			},
		}
		// Global GatewayClassConfig in controller namespace targeting the
		// GatewayClass should apply to all Gateways of that class, so it should
		// overwrite the blueprint default
		gwcc := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "global", Namespace: "controller-system"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"key": "gwcc-default"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "test-class",
				},
			},
		}
		r := newFakeClient(gwcc) // preload fake client with a created GatewayClassConfig
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["key"]).To(Equal("gwcc-default"))
	})

	It("Should let namespace-scoped GatewayClassConfig default apply to gateways in that namespace", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
		}
		// GatewayClassConfig in Gateway namespace, targeting the Namespace
		gwcc := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "ns-policy", Namespace: "default"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"nsKey": "from-gwcc-ns"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{ // targets Namespace instead of a specific Gateway
					Group: "", Kind: "Namespace", Name: "default",
				},
			},
		}
		r := newFakeClient(gwcc) // preload fake client with a created GatewayClassConfig
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["nsKey"]).To(Equal("from-gwcc-ns"))
	})

	It("Should apply local GatewayClassConfig targeting GatewayClass", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
		}
		// GatewayClassConfig in Gateway namespace, targeting the GatewayClass
		gwcc := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "local-gwc-policy", Namespace: "default"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"localKey": "from-local-gwcc"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "test-class",
				},
			},
		}
		r := newFakeClient(gwcc)
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["localKey"]).To(Equal("from-local-gwcc"))
	})

	It("Should apply GatewayConfig targeting a specific Gateway", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
		}
		gwc := &gwcapi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "gw-policy", Namespace: "default"},
			Spec: gwcapi.GatewayConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"gwKey": "from-gw-config"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "Gateway", Name: "test-gw", Namespace: nsPtr("default"),
				},
			},
		}
		r := newFakeClient(gwc)
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["gwKey"]).To(Equal("from-gw-config"))
	})

	It("Should apply namespace-targeted GatewayConfig", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
		}
		// Targets Namespace instead of a specific Gateway
		gwc := &gwcapi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "ns-policy", Namespace: "default"},
			Spec: gwcapi.GatewayConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"nsKey": "from-ns"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Kind: "Namespace", Name: "default",
				},
			},
		}
		r := newFakeClient(gwc)
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["nsKey"]).To(Equal("from-ns"))
	})

	// --- 2. Filtering ---

	It("Should not apply GatewayConfig targeting a different Gateway", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
		}
		// Targets "other-gw", not our "test-gw"
		gwc := &gwcapi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "gw-policy", Namespace: "default"},
			Spec: gwcapi.GatewayConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"gwKey": "should-not-appear"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "Gateway", Name: "other-gw", Namespace: nsPtr("default"),
				},
			},
		}
		r := newFakeClient(gwc)
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values).NotTo(HaveKey("gwKey"))
	})

	// --- 3. Specificity between levels ---

	It("Should let GatewayConfig default overwrite GatewayClassConfig default, and GatewayClassConfig override beat GatewayConfig override", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
		}
		gwcc := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "global", Namespace: "controller-system"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default:  jsonRaw(`{"defaultKey": "gwcc-default"}`),
					Override: jsonRaw(`{"overrideKey": "gwcc-override"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "test-class",
				},
			},
		}
		gwc := &gwcapi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "gw-policy", Namespace: "default"},
			Spec: gwcapi.GatewayConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default:  jsonRaw(`{"defaultKey": "gwc-default"}`),
					Override: jsonRaw(`{"overrideKey": "gwc-override"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "Gateway", Name: "test-gw", Namespace: nsPtr("default"),
				},
			},
		}
		r := newFakeClient(gwcc, gwc)
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		// GatewayConfig is more specific, so its default wins
		Expect(values["defaultKey"]).To(Equal("gwc-default"))
		// GatewayClassConfig override is less specific, so it wins over GatewayConfig override
		Expect(values["overrideKey"]).To(Equal("gwcc-override"))
	})

	It("Should let local GatewayClassConfig default beat global GatewayClassConfig default", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
		}
		// Global GatewayClassConfig in controller namespace
		globalGwcc := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "global-policy", Namespace: "controller-system"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"key": "global-default"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "test-class",
				},
			},
		}
		// Local GatewayClassConfig in Gateway namespace should overwrite global
		// GatewayClassConfig default as it is more specific
		localGwcc := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "local-policy", Namespace: "default"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"key": "local-default"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: "", Kind: "Namespace", Name: "default",
				},
			},
		}
		r := newFakeClient(globalGwcc, localGwcc)
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["key"]).To(Equal("local-default"))
	})

	// --- 4. Conflict resolution ---

	It("Should resolve same-level conflicts by policy name ordering", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
		}
		// Two GatewayClassConfigs at the same level (both global, targeting GatewayClass)
		gwccFirst := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "aaa-first", Namespace: "controller-system"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default:  jsonRaw(`{"defKey": "first-default"}`),  // Defaults: alphabetically-last policy name wins, so this loses
					Override: jsonRaw(`{"ovrKey": "first-override"}`), // Overrides: alphabetically-first policy name wins, so this wins
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "test-class",
				},
			},
		}
		gwccSecond := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "zzz-second", Namespace: "controller-system"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default:  jsonRaw(`{"defKey": "second-default"}`),  // Defaults: alphabetically-last policy name wins, so this wins
					Override: jsonRaw(`{"ovrKey": "second-override"}`), // Overrides: alphabetically-first policy name wins, so this loses
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "test-class",
				},
			},
		}
		r := newFakeClient(gwccFirst, gwccSecond)
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		Expect(values["defKey"]).To(Equal("second-default")) // Alphabetically-last policy name wins for defaults
		Expect(values["ovrKey"]).To(Equal("first-override")) // Alphabetically-first policy name wins for overrides
	})

	// --- 5. Nested values ---

	It("Should merge nested values with correct precedence", func() {
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "bp"},
			Spec: gwcapi.GatewayClassBlueprintSpec{
				Values: gwcapi.TemplateValues{
					Override: jsonRaw(`{"nested": {"bpOverride": "from-bp"}}`),
					Default:  jsonRaw(`{"nested": {"bpDefault": "from-bp", "shared": "bp"}}`),
				},
			},
		}
		gwc := &gwcapi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "gw-policy", Namespace: "default"},
			Spec: gwcapi.GatewayConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Override: jsonRaw(`{"nested": {"gwOverride": "from-gw", "bpOverride": "from-gw"}}`),
					Default:  jsonRaw(`{"nested": {"shared": "gw"}}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "Gateway", Name: "test-gw", Namespace: nsPtr("default"),
				},
			},
		}
		r := newFakeClient(gwc)
		values, err := lookupValues(ctx, r, "test-class", gwcb, "default", "test-gw")
		Expect(err).NotTo(HaveOccurred())
		nested, ok := values["nested"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(nested["bpOverride"]).To(Equal("from-bp")) // Blueprint override wins
		Expect(nested["bpDefault"]).To(Equal("from-bp"))  // Blueprint default preserved
		Expect(nested["gwOverride"]).To(Equal("from-gw")) // GatewayConfig override applies
		Expect(nested["shared"]).To(Equal("gw"))          // GatewayConfig default overwrites blueprint default
	})

	// --- 6. Combined policy levels ---

	It("Should apply all policy levels with correct precedence (GEP-713)", func() {
		ControllerNamespace = "bifrost-gateway-controller-system"
		gwcb := &gwcapi.GatewayClassBlueprint{
			ObjectMeta: metav1.ObjectMeta{Name: "common-test"},
			Spec: gwcapi.GatewayClassBlueprintSpec{
				Values: gwcapi.TemplateValues{
					Override: jsonRaw(`{"someValue1": "blueprint-override1", "nested": {"someValue1": "blueprint-nested-override1"}}`),
					Default:  jsonRaw(`{"someValue5": "blueprint-default5", "someValue6": "blueprint-default6", "someValue7": "blueprint-default7", "someValue8": "blueprint-default8", "nested": {"someValue2": "blueprint-nested-default2", "someValue3": "blueprint-nested-default3"}}`),
				},
			},
		}

		// Global GatewayClassConfig in controller namespace, targeting GatewayClass
		gwccGlobal := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "common-test-global1", Namespace: "bifrost-gateway-controller-system"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Override: jsonRaw(`{"someValue2": "global-config1-override2"}`),
					Default:  jsonRaw(`{"someValue5": "global-config1-default5"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "common-test",
				},
			},
		}

		// Namespace GatewayClassConfig targeting GatewayClass (same namespace as Gateway)
		gwccNs1 := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "common-test-ns1", Namespace: "default"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Override: jsonRaw(`{"someValue3": "global-config2-override3"}`),
					Default:  jsonRaw(`{"someValue6": "global-config2-default6"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "GatewayClass", Name: "common-test",
				},
			},
		}

		// Namespace GatewayClassConfig targeting Namespace
		gwccNs2 := &gwcapi.GatewayClassConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "common-test-ns2", Namespace: "default"},
			Spec: gwcapi.GatewayClassConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Override: jsonRaw(`{"someValue10": "global-config3-override10"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: "", Kind: "Namespace", Name: "default",
				},
			},
		}

		// GatewayConfig targeting Gateway (with nested override)
		gwcGw := &gwcapi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "common-test-gw1", Namespace: "default"},
			Spec: gwcapi.GatewayConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Override: jsonRaw(`{"someValue2": "config1-override2", "someValue3": "config1-override3", "someValue4": "config1-override4", "nested": {"someValue3": "config1-nested-override3"}}`),
					Default:  jsonRaw(`{"someValue7": "config1-default7"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: gatewayapi.GroupName, Kind: "Gateway", Name: "common-test", Namespace: nsPtr("default"),
				},
			},
		}

		// GatewayConfig targeting Namespace
		gwcNs := &gwcapi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "common-test-ns1", Namespace: "default"},
			Spec: gwcapi.GatewayConfigSpec{
				TemplateValues: gwcapi.TemplateValues{
					Default: jsonRaw(`{"someValue9": "ns-config1-default9"}`),
				},
				TargetRef: gatewayv1a2.NamespacedPolicyTargetReference{
					Group: "", Kind: "Namespace", Name: "default",
				},
			},
		}

		r := newFakeClient(gwccGlobal, gwccNs1, gwccNs2, gwcGw, gwcNs)
		values, err := lookupValues(ctx, r, "common-test", gwcb, "default", "common-test")
		Expect(err).NotTo(HaveOccurred())

		// Blueprint override wins over everything
		By("Blueprint override has highest precedence")
		Expect(values["someValue1"]).To(Equal("blueprint-override1"))

		// GatewayClassConfig override beats GatewayConfig override
		By("Global GatewayClassConfig override beats GatewayConfig override")
		Expect(values["someValue2"]).To(Equal("global-config1-override2"))

		By("Namespace GatewayClassConfig override beats GatewayConfig override")
		Expect(values["someValue3"]).To(Equal("global-config2-override3"))

		// GatewayConfig override when no higher-level override
		By("GatewayConfig override applies when no higher override exists")
		Expect(values["someValue4"]).To(Equal("config1-override4"))

		// GatewayClassConfig default overwrites blueprint default
		By("GatewayClassConfig default overwrites blueprint default")
		Expect(values["someValue5"]).To(Equal("global-config1-default5"))
		Expect(values["someValue6"]).To(Equal("global-config2-default6"))

		// GatewayConfig default overwrites blueprint default
		By("GatewayConfig default overwrites blueprint default")
		Expect(values["someValue7"]).To(Equal("config1-default7"))

		// Blueprint default when nothing overrides it
		By("Blueprint default is used when no policy overrides it")
		Expect(values["someValue8"]).To(Equal("blueprint-default8"))

		// Namespace-targeted GatewayConfig default
		By("Namespace-targeted GatewayConfig default applies")
		Expect(values["someValue9"]).To(Equal("ns-config1-default9"))

		// Namespace-targeted GatewayClassConfig override
		By("Namespace-targeted GatewayClassConfig override applies")
		Expect(values["someValue10"]).To(Equal("global-config3-override10"))

		// Nested values
		By("Nested blueprint override wins")
		nested, ok := values["nested"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(nested["someValue1"]).To(Equal("blueprint-nested-override1"))

		By("Nested blueprint default is preserved")
		Expect(nested["someValue2"]).To(Equal("blueprint-nested-default2"))

		By("Nested GatewayConfig override applies")
		Expect(nested["someValue3"]).To(Equal("config1-nested-override3"))
	})
})

// ------------------------------------
// [lookupGateway] tests
// ------------------------------------

var _ = Describe("lookupGateway", func() {
	ctx := context.Background()

	It("Should return the Gateway when found", func() {
		gw := &gatewayapi.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
			Spec: gatewayapi.GatewaySpec{
				GatewayClassName: "test-class",
			},
		}
		r := newFakeClient(gw)
		result, err := lookupGateway(ctx, r, "test-gw", "default")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Name).To(Equal("test-gw"))
		Expect(result.Namespace).To(Equal("default"))
	})

	It("Should return an error when not found", func() {
		r := newFakeClient()
		_, err := lookupGateway(ctx, r, "missing", "default")
		Expect(err).To(HaveOccurred())
	})
})

// ------------------------------------
// [unstructuredToGVR] tests
// ------------------------------------

var _ = Describe("unstructuredToGVR", func() {
	It("Should return GVR and namespaced=true for a known namespaced resource", func() {
		r := newFakeClientWithMapper()
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("gateway.networking.k8s.io/v1")
		u.SetKind("Gateway")

		gotGVR, isNamespaced, err := unstructuredToGVR(r, u)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotGVR.Resource).To(Equal("gateways"))
		Expect(isNamespaced).To(BeTrue())
	})

	It("Should return error for invalid apiVersion", func() {
		r := newFakeClient()
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("not/a/valid/version")
		u.SetKind("Something")
		_, _, err := unstructuredToGVR(r, u)
		Expect(err).To(HaveOccurred())
	})

	It("Should return error for unknown kind", func() {
		r := newFakeClient()
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("DoesNotExist")
		_, _, err := unstructuredToGVR(r, u)
		Expect(err).To(HaveOccurred())
	})
})

// ------------------------------------
// [patchUnstructured] tests
// ------------------------------------

var _ = Describe("patchUnstructured", func() {
	ctx := context.Background()

	It("Should patch a namespaced resource", func() {
		r := newFakeDynClient()
		us := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "gateway.networking.k8s.io/v1",
				"kind":       "Gateway",
				"metadata":   map[string]any{"name": "test-gw"},
			},
		}
		gvr := &schema.GroupVersionResource{
			Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways",
		}
		ns := "default"
		err := patchUnstructured(ctx, r, us, gvr, &ns)
		Expect(err).NotTo(HaveOccurred()) // TODO: could be great if we return the patched object and assert on it to verify the patch was applied correctly
	})

	It("Should patch a cluster-scoped resource", func() {
		r := newFakeDynClient()
		us := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "gateway.networking.k8s.io/v1",
				"kind":       "GatewayClass",
				"metadata":   map[string]any{"name": "test-class"},
			},
		}
		gvr := &schema.GroupVersionResource{
			Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses",
		}
		err := patchUnstructured(ctx, r, us, gvr, nil)
		Expect(err).NotTo(HaveOccurred()) // TODO: could be great if we return the patched object and assert on it to verify the patch was applied correctly
	})
})

// ------------------------------------
// [PtrTo] tests
// ------------------------------------

var _ = Describe("PtrTo", func() {
	It("Should return pointer to string", func() {
		Expect(*PtrTo("hello")).To(Equal("hello"))
	})

	It("Should return pointer to int", func() {
		Expect(*PtrTo(42)).To(Equal(42))
	})

	It("Should return pointer to bool", func() {
		Expect(*PtrTo(true)).To(BeTrue())
	})
})
