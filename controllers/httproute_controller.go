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
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"

	selfapi "github.com/tv2/bifrost-gateway-controller/pkg/api"
)

type HTTPRouteReconciler struct {
	client    client.Client
	scheme    *runtime.Scheme
	dynClient dynamic.Interface
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/finalizers,verbs=update

func (r *HTTPRouteReconciler) Client() client.Client {
	return r.client
}

func (r *HTTPRouteReconciler) Scheme() *runtime.Scheme {
	return r.scheme
}

func (r *HTTPRouteReconciler) DynamicClient() dynamic.Interface {
	return r.dynClient
}

func NewHTTPRouteController(mgr ctrl.Manager, config *rest.Config) *HTTPRouteReconciler {
	r := &HTTPRouteReconciler{
		client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		dynClient: dynamic.NewForConfigOrDie(config),
	}
	return r
}

func (r *HTTPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayapi.HTTPRoute{}).
		Complete(r)
}

// Compare values referenced by pointers. Both a and b must be pointers to the same type
func derefCmp[T comparable](a, b *T) bool {
	if (a != nil && b == nil) || (a == nil && b != nil) {
		return false // Different since one has nil pointer while other has non-nil
	}
	if a != nil && b != nil && (*a != *b) {
		return false // Actual value is different
	}
	return true
}

// Compare parentRefs, return true if same parent is referenced
func parentRefCmp(a, b gatewayapi.ParentReference) bool {
	// Group, Kind and Namespace, SectionName and Port are pointers and optional (may be nil)
	if !derefCmp(a.Group, b.Group) || !derefCmp(a.Kind, b.Kind) || !derefCmp(a.Namespace, b.Namespace) ||
		!derefCmp(a.SectionName, b.SectionName) || !derefCmp(a.Port, b.Port) {
		return false
	}
	return a.Name == b.Name
}

// Lookup Gateway from parentRef
func lookupParent(ctx context.Context, r ControllerClient, rt *gatewayapi.HTTPRoute, p gatewayapi.ParentReference) (*gatewayapi.Gateway, error) {
	if p.Namespace == nil {
		return lookupGateway(ctx, r, p.Name, rt.Namespace)
	}
	return lookupGateway(ctx, r, p.Name, string(*p.Namespace))
}

// Find statuscondition for a specific parentRef
func findParentRouteStatus(rtStatus *gatewayapi.RouteStatus, parent gatewayapi.ParentReference) *gatewayapi.RouteParentStatus {
	for i := range rtStatus.Parents {
		pStat := &rtStatus.Parents[i]
		if parentRefCmp(pStat.ParentRef, parent) && pStat.ControllerName == selfapi.SelfControllerName {
			return pStat
		}
	}
	return nil
}

// Set status condition for a specific parentRef
func setRouteStatusCondition(rtStatus *gatewayapi.RouteStatus, parent gatewayapi.ParentReference, newCondition *metav1.Condition) {
	if newCondition.LastTransitionTime.IsZero() {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
	}

	existingParentRouteStat := findParentRouteStatus(rtStatus, parent)
	if existingParentRouteStat == nil {
		newStatus := gatewayapi.RouteParentStatus{
			ParentRef:      parent,
			ControllerName: selfapi.SelfControllerName,
			Conditions:     []metav1.Condition{*newCondition},
		}
		rtStatus.Parents = append(rtStatus.Parents, newStatus)
		return
	}

	meta.SetStatusCondition(&existingParentRouteStat.Conditions, *newCondition)
}

func (r *HTTPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var doStatusUpdate = false
	var requeue = false
	var rt gatewayapi.HTTPRoute
	if err := r.Client().Get(ctx, req.NamespacedName, &rt); err != nil {
		logger.Info("HTTPRoute not found", "httproute", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("reconcile started", "httproute", req.NamespacedName, "parentRefs", len(rt.Spec.ParentRefs))

	// Prepare HTTPRoute resource for use in templates by converting to map[string]any
	rtMap, err := objectToMap(&rt)
	if err != nil {
		logger.Error(err, "cannot convert httproute to map", "httproute", req.NamespacedName)
		return ctrl.Result{}, fmt.Errorf("cannot convert httproute to map: %w", err)
	}

	templateValues := TemplateValues{
		HTTPRoute: rtMap,
	}

	// Prepare for setting status in parentRef loop
	if rt.Status.Parents == nil {
		rt.Status.Parents = []gatewayapi.RouteParentStatus{}
	}

	// Loop through Gateway parents, render HTTPRoute using templates defined by associated GatewayClassBlueprint
	for _, parent := range rt.Spec.ParentRefs {
		if *parent.Kind != gatewayapi.Kind("Gateway") {
			logger.V(1).Info("skipping non-Gateway parentRef", "kind", *parent.Kind, "name", parent.Name)
			continue
		}

		// Collect debug context for this parent iteration
		var parentDebugCtx []any
		parentDebugCtx = append(parentDebugCtx, "parentGateway", parent.Name)

		gw, err := lookupParent(ctx, r, &rt, parent)
		if err != nil {
			logger.Info("gateway for HTTPRoute not found, requeuing", "httproute", rt.Name, "parentGateway", parent.Name)
			requeue = true
			continue
		}

		// FIXME check route matches gateway hostname,
		// listener sectionname, port etc. From this a list of
		// listeners should be matched and we should validate
		// that listener allows route to attach

		gwc, err := lookupGatewayClass(ctx, r, gw.Spec.GatewayClassName)
		if err != nil {
			logger.Info("GatewayClass not found, requeuing", "gatewayClassName", gw.Spec.GatewayClassName, "parentGateway", parent.Name)
			requeue = true
			continue
		}
		if !isOurGatewayClass(gwc) {
			logger.Info("skipping parentRef with non-owned GatewayClass", "gatewayClassName", gwc.Name, "controllerName", gwc.Spec.ControllerName)
			continue
		}

		parentDebugCtx = append(parentDebugCtx, "gatewayClass", gwc.Name)

		gwcb, err := lookupGatewayClassBlueprint(ctx, r, gwc)
		if err != nil {
			logger.Info("blueprint not found for GatewayClass, requeuing", "gatewayClass", gwc.Name, "parametersRef", gwc.Spec.ParametersRef)
			requeue = true
			continue
		}

		parentDebugCtx = append(parentDebugCtx, "blueprint", gwcb.Name)

		values, err := lookupValues(ctx, r, gwc.Name, gwcb, gw.Namespace, gw.Name)
		if err != nil {
			logger.Error(err, "cannot lookup values", parentDebugCtx...)
			return ctrl.Result{}, fmt.Errorf("cannot lookup values: %w", err)
		}
		templateValues.Values = values

		// Prepare Gateway resource for use in templates by converting to map[string]any
		gatewayMap, err := objectToMap(gw)
		if err != nil {
			logger.Error(err, "cannot convert gateway to map", "parentGateway", parent.Name)
			return ctrl.Result{}, fmt.Errorf("cannot convert gateway to map: %w", err)
		}
		templateValues.Gateway = &gatewayMap

		templates, err := parseTemplates(gwcb.Spec.HTTPRouteTemplate.ResourceTemplates)
		if err != nil {
			logger.Error(err, "cannot parse templates", parentDebugCtx...)
			return ctrl.Result{}, err
		}

		parentDebugCtx = append(parentDebugCtx, "templateCount", len(templates))
		logger.V(1).Info("processing parent", parentDebugCtx...)

		// Resource templates may reference each other, with
		// the worst-case being a strictly linear DAG. This
		// means that we may have to loop N times, with N
		// being the number of resources.
		var renderedNum, existsNum int
		for attempt := 0; attempt < len(templates); attempt++ {
			logger.V(1).Info("reconcile loop", "attempt", attempt, "totalTemplates", len(templates), "parentGateway", parent.Name)
			isFinalAttempt := attempt == len(templates)-1

			templateValues.Resources = buildResourceValues(templates)

			renderedNum, existsNum = renderTemplates(ctx, r, &rt, templates, &templateValues, isFinalAttempt)
			logger.V(1).Info("rendered templates", "rendered", renderedNum, "exists", existsNum, "attempt", attempt)

			if err := applyTemplates(ctx, r, &rt, templates); err != nil {
				logger.Error(err, "unable to apply templates", parentDebugCtx...)
				return ctrl.Result{}, fmt.Errorf("unable to apply templates: %w", err)
			}
		}
		// If we haven't already decided to requeue, then requeue if not all templates could render (possibly a missing dependency)
		requeue = requeue || (renderedNum != len(templates))
		logger.V(1).Info("template loop completed", "parentGateway", parent.Name, "renderedNum", renderedNum, "totalNum", len(templates), "requeue", requeue)

		// FIXME errors in templating and status of sub-resources in general should set status conditions

		// Update status for current parent Gateway
		doStatusUpdate = true
		setRouteStatusCondition(&rt.Status.RouteStatus, parent,
			&metav1.Condition{
				Type:   string(gatewayapi.RouteConditionAccepted),
				Status: "True",
				Reason: string(gatewayapi.RouteReasonAccepted),
			})
	}

	if doStatusUpdate {
		if err := r.Client().Status().Update(ctx, &rt); err != nil {
			logger.Error(err, "unable to update HTTPRoute status", "httproute", req.NamespacedName)
			return ctrl.Result{}, err
		}
	}

	if requeue {
		logger.Info("requeuing, dependencies not yet available", "httproute", req.NamespacedName)
		return ctrl.Result{RequeueAfter: dependencyMissingRequeuePeriod}, nil
	}
	logger.Info("reconcile completed successfully", "httproute", req.NamespacedName)
	return ctrl.Result{}, nil
}
