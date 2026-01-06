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
	"errors"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"

	gwcapi "github.com/tv2/bifrost-gateway-controller/apis/gateway.tv2.dk/v1alpha1"
	selfapi "github.com/tv2/bifrost-gateway-controller/pkg/api"
)

var (
	// The namespace the controller will watch for global policies
	ControllerNamespace string
)

type ControllerClient interface {
	Client() client.Client
	Scheme() *runtime.Scheme
}

type ControllerDynClient interface {
	ControllerClient
	DynamicClient() dynamic.Interface
}

func isOurGatewayClass(gwc *gatewayapi.GatewayClass) bool {
	return gwc.Spec.ControllerName == selfapi.SelfControllerName
}

func lookupGatewayClass(ctx context.Context, r ControllerClient, name gatewayapi.ObjectName) (*gatewayapi.GatewayClass, error) {
	var gwc gatewayapi.GatewayClass
	if err := r.Client().Get(ctx, types.NamespacedName{Name: string(name)}, &gwc); err != nil {
		return nil, err
	}

	return &gwc, nil
}

func lookupGatewayClassBlueprint(ctx context.Context, r ControllerClient, gwc *gatewayapi.GatewayClass) (*gwcapi.GatewayClassBlueprint, error) {
	if gwc.Spec.ParametersRef == nil {
		return nil, errors.New("GatewayClass without parameters")
	}

	if gwc.Spec.ParametersRef.Kind != "GatewayClassBlueprint" || gwc.Spec.ParametersRef.Group != "gateway.tv2.dk" {
		return nil, errors.New("parameter kind/group is not a valid GatewayClassBlueprint")
	}

	var gwcb gwcapi.GatewayClassBlueprint
	if err := r.Client().Get(ctx, types.NamespacedName{Name: gwc.Spec.ParametersRef.Name}, &gwcb); err != nil {
		return nil, err
	}

	return &gwcb, nil
}

// Deep map merge, with 'b' overwriting values in 'a'.  On type conflicts precedence is given to 'a' i.e. no overwrite
// x and y are concrete versions of a and b
func merge(a, b any) any {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return a
		}
		for k, vy := range y { // Copy from 'b' (represented by 'y') into 'a'
			if va, ok := x[k]; ok {
				x[k] = merge(va, vy)
			} else {
				x[k] = vy
			}
		}
	default:
		return b
	}
	return a
}

// Lookup values from GatewayClassConfig/GatewayConfig CRDs and combine using precedence rules:
// - Values from GatewayClassBlueprint
// - Values from GatewayClassConfig in controller namespace (aka. global policies)
// - Values from GatewayClassConfig in Gateway/HTTPRoute local namespace targeting namespace
// - Values from GatewayClassConfig in Gateway/HTTPRoute local namespace targeting GatewayClass
// - Values from GatewayConfig in Gateway/HTTPRoute local namespace, targeting namespace
// - Values from GatewayConfig in Gateway/HTTPRoute local namespace, targeting Gateway/HTTPRoute resource
// Note, defaults are processed top-to-bottom (i.e. later defaults overwrites earlier defaults), while overrides are bottom-to-top (see GEP-713)
//
// See also doc/extended-configuration-w-policy-attachments.md
//
// FIXME: Fully implement conflict resolution: https://gateway-api.sigs.k8s.io/references/policy-attachment/#conflict-resolution
//
//nolint:gocyclo // This function have a repeating character and this not as complex as the number of ifs may indicate
func lookupValues(ctx context.Context, r ControllerClient, gatewayClassName string, gwcb *gwcapi.GatewayClassBlueprint,
	gwNamespace string, gwName string) (map[string]any, error) {
	values := map[string]any{}
	var err error

	// Helper to parse and merge-overwrite values. IMPORTANT: All
	// values from GatewayClassConfig and GatewayConfigs are
	// Unmarshalled and hence we will not be modifying original
	// K8s resources
	mergeValues := func(src *apiextensionsv1.JSON, existing map[string]any) (map[string]any, error) {
		if src != nil {
			newvals := map[string]any{}
			var ok bool
			if err = json.Unmarshal(src.Raw, &newvals); err != nil {
				return nil, fmt.Errorf("cannot unmarshal values: %w", err)
			}
			existing, ok = merge(existing, newvals).(map[string]any)
			if !ok {
				return nil, fmt.Errorf("cannot merge values: %w", err)
			}
		}
		return existing, nil
	}

	var gwccGlobal gwcapi.GatewayClassConfigList
	err = r.Client().List(ctx, &gwccGlobal, client.InNamespace(ControllerNamespace))
	if err != nil {
		return nil, err
	}

	// GatewayClassConfig and GatewayConfig in same namespace as parent resource (e.g. a Gateway resource)
	var gwccLocal gwcapi.GatewayClassConfigList
	err = r.Client().List(ctx, &gwccLocal, client.InNamespace(gwNamespace))
	if err != nil {
		return nil, err
	}
	var gwcLocal gwcapi.GatewayConfigList
	err = r.Client().List(ctx, &gwcLocal, client.InNamespace(gwNamespace))
	if err != nil {
		return nil, err
	}

	// Select policies that target GatewayClass, parent resource or namespace of parent resource
	// Note, policies are kept ordered!
	var gwccFiltered []*gwcapi.GatewayClassConfig
	var gwcFiltered []*gwcapi.GatewayConfig

	// Global GatewayClassConfig first
	for idx := range gwccGlobal.Items {
		gwcc := &gwccGlobal.Items[idx]
		if gwcc.Spec.TargetRef.Kind == "GatewayClass" &&
			gwcc.Spec.TargetRef.Group == gatewayapi.GroupName &&
			string(gwcc.Spec.TargetRef.Name) == gatewayClassName {
			gwccFiltered = append(gwccFiltered, gwcc) // gwcc targets GatewayClass
		}
	}
	// Namespace GatewayClassConfig targeting namespace second
	for idx := range gwccLocal.Items {
		gwcc := &gwccLocal.Items[idx]
		if gwcc.Spec.TargetRef.Kind == "Namespace" &&
			gwcc.Spec.TargetRef.Group == "" &&
			string(gwcc.Spec.TargetRef.Name) == gwNamespace {
			gwccFiltered = append(gwccFiltered, gwcc) // gwcc targets namespace
		}
	}
	// Namespace GatewayClassConfig targeting GatewayClass third
	for idx := range gwccLocal.Items {
		gwcc := &gwccLocal.Items[idx]
		if gwcc.Spec.TargetRef.Kind == "GatewayClass" &&
			gwcc.Spec.TargetRef.Group == gatewayapi.GroupName &&
			string(gwcc.Spec.TargetRef.Name) == gatewayClassName {
			gwccFiltered = append(gwccFiltered, gwcc) // gwcc targets GatewayClass
		}
	}
	// Namespace GatewayConfig first
	for idx := range gwcLocal.Items {
		gwc := &gwcLocal.Items[idx]
		if gwc.Spec.TargetRef.Kind == "Namespace" &&
			gwc.Spec.TargetRef.Group == "" &&
			string(gwc.Spec.TargetRef.Name) == gwNamespace {
			gwcFiltered = append(gwcFiltered, gwc) // gwcc targets namespace of Gateway
		}
	}
	// Parent resource GatewayConfig second
	for idx := range gwcLocal.Items {
		gwc := &gwcLocal.Items[idx]
		if gwc.Spec.TargetRef.Kind == "Gateway" &&
			gwc.Spec.TargetRef.Group == gatewayapi.GroupName &&
			(gwc.Spec.TargetRef.Namespace == nil || string(*gwc.Spec.TargetRef.Namespace) == gwNamespace) &&
			string(gwc.Spec.TargetRef.Name) == gwName {
			gwcFiltered = append(gwcFiltered, gwc) // gwcc targets Gateway
		}
	}

	// Process defaults

	// Blueprint default values are first
	if values, err = mergeValues(gwcb.Spec.Values.Default, values); err != nil {
		return nil, fmt.Errorf("while processing blueprint default values for gatewayclass %s: %w", gatewayClassName, err)
	}
	// GatewayClassConfig, ordered, global first
	for _, pol := range gwccFiltered {
		if values, err = mergeValues(pol.Spec.Default, values); err != nil {
			return nil, fmt.Errorf("while processing %s: %w", pol.Name, err)
		}
	}
	// GatewayConfig, ordered, namespace-targeted first
	for _, pol := range gwcFiltered {
		if values, err = mergeValues(pol.Spec.Default, values); err != nil {
			return nil, fmt.Errorf("while processing %s: %w", pol.Name, err)
		}
	}

	// Process overrides

	// GatewayConfig, ordered, namespace-targeted is first i.e. reverse loop
	for idx := len(gwcFiltered) - 1; idx >= 0; idx-- {
		if values, err = mergeValues(gwcFiltered[idx].Spec.Override, values); err != nil {
			return nil, fmt.Errorf("while processing %s: %w", gwcFiltered[idx].Name, err)
		}
	}

	// GatewayClassConfig, ordered, global is first i.e. reverse loop
	for idx := len(gwccFiltered) - 1; idx >= 0; idx-- {
		if values, err = mergeValues(gwccFiltered[idx].Spec.Override, values); err != nil {
			return nil, fmt.Errorf("while processing %s: %w", gwccFiltered[idx].Name, err)
		}
	}

	// Blueprint override values are last since they have highest precedence
	if values, err = mergeValues(gwcb.Spec.Values.Override, values); err != nil {
		return nil, fmt.Errorf("while processing blueprint override values for gatewayclass %s: %w", gatewayClassName, err)
	}

	return values, nil
}

func lookupGateway(ctx context.Context, r ControllerClient, name gatewayapi.ObjectName, namespace string) (*gatewayapi.Gateway, error) {
	var gw gatewayapi.Gateway
	if err := r.Client().Get(ctx, types.NamespacedName{Name: string(name), Namespace: namespace}, &gw); err != nil {
		return nil, err
	}
	return &gw, nil
}

// From an unstructured object, lookup GVR and whether resource is namespaced
func unstructuredToGVR(r ControllerClient, u *unstructured.Unstructured) (*schema.GroupVersionResource, bool, error) {
	gv, err := schema.ParseGroupVersion(u.GetAPIVersion())
	if err != nil {
		return nil, false, err
	}

	gk := schema.GroupKind{
		Group: gv.Group,
		Kind:  u.GetKind(),
	}

	mapping, err := r.Client().RESTMapper().RESTMapping(gk, gv.Version)
	if err != nil {
		return nil, false, err
	}

	isNamespaced := false
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		isNamespaced = true
	}

	return &schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: mapping.Resource.Resource,
	}, isNamespaced, nil
}

// Apply an unstructured object using server-side apply
func patchUnstructured(ctx context.Context, r ControllerDynClient, us *unstructured.Unstructured,
	gvr *schema.GroupVersionResource, namespace *string) error {
	jsonData, err := json.Marshal(us.Object)
	if err != nil {
		return fmt.Errorf("unable to marshal unstructured to json %w", err)
	}

	force := true

	metricPatchApply.Inc()
	if namespace != nil {
		dynamicClient := r.DynamicClient().Resource(*gvr).Namespace(*namespace)
		_, err = dynamicClient.Patch(ctx, us.GetName(), types.ApplyPatchType, jsonData, metav1.PatchOptions{
			Force:        &force,
			FieldManager: string(selfapi.SelfControllerName),
		})
	} else {
		dynamicClient := r.DynamicClient().Resource(*gvr)
		_, err = dynamicClient.Patch(ctx, us.GetName(), types.ApplyPatchType, jsonData, metav1.PatchOptions{
			Force:        &force,
			FieldManager: string(selfapi.SelfControllerName),
		})
	}

	if err != nil {
		metricPatchApplyErrs.Inc()
	}
	return err
}

func PtrTo[T any](val T) *T {
	return &val
}
