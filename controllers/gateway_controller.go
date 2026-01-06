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
	"sort"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
)

// Used to requeue when a resource is missing a dependency
var dependencyMissingRequeuePeriod = 30 * time.Second

// GatewayReconciler reconciles a Gateway object
type GatewayReconciler struct {
	client    client.Client
	scheme    *runtime.Scheme
	dynClient dynamic.Interface
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update

func (r *GatewayReconciler) Client() client.Client {
	return r.client
}

func (r *GatewayReconciler) Scheme() *runtime.Scheme {
	return r.scheme
}

func (r *GatewayReconciler) DynamicClient() dynamic.Interface {
	return r.dynClient
}

func NewGatewayController(mgr ctrl.Manager, config *rest.Config) *GatewayReconciler {
	r := &GatewayReconciler{
		client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		dynClient: dynamic.NewForConfigOrDie(config),
	}
	return r
}

func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayapi.Gateway{}).
		Complete(r)
}

func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var requeue bool

	logger := log.FromContext(ctx)
	logger.Info("Reconcile")

	var gw gatewayapi.Gateway

	if err := r.Client().Get(ctx, req.NamespacedName, &gw); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Gateway")

	gwc, err := lookupGatewayClass(ctx, r, gw.Spec.GatewayClassName)
	if err != nil {
		return ctrl.Result{RequeueAfter: dependencyMissingRequeuePeriod}, client.IgnoreNotFound(err)
	}

	if !isOurGatewayClass(gwc) {
		return ctrl.Result{}, nil
	}

	gwcb, err := lookupGatewayClassBlueprint(ctx, r, gwc)
	if err != nil {
		return ctrl.Result{RequeueAfter: dependencyMissingRequeuePeriod}, fmt.Errorf("parameters for GatewayClass %q not found: %w", gwc.ObjectMeta.Name, err)
	}

	routes, err := lookupHTTPRoutes(ctx, r)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot look up routes: %w", err)
	}
	gwRoutes := filterHTTPRoutesForGateway(&gw, routes)
	union, isect := combineHostnames(&gw, gwRoutes)

	// Prepare Gateway resource for use in templates by converting to map[string]any
	gatewayMap, err := objectToMap(&gw)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot convert gateway to map: %w", err)
	}

	values, err := lookupValues(ctx, r, gwc.Name, gwcb, gw.ObjectMeta.Namespace, gw.ObjectMeta.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot lookup values: %w", err)
	}

	// Setup template variables context
	templateValues := TemplateValues{
		Gateway: &gatewayMap,
		Values:  values,
		Hostnames: TemplateHostnameValues{
			Union:        union,
			Intersection: isect,
		},
	}

	templates, err := parseTemplates(gwcb.Spec.GatewayTemplate.ResourceTemplates)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot parse templates: %w", err)
	}

	// At this point we are ready to accept the Gateway resource. If we encounter errors we track then in this variable
	var errStatus error

	// Resource templates may reference each other, with the
	// worst-case being a strictly linear DAG. This means that we
	// may have to loop N times, with N being the number of
	// resources.
	var renderedNum, existsNum int
	for attempt := 0; attempt < len(templates); attempt++ {
		logger.Info("reconcile loop", "attempt", attempt)
		isFinalAttempt := attempt == len(templates)-1

		templateValues.Resources = buildResourceValues(templates)

		renderedNum, existsNum = renderTemplates(ctx, r, &gw, templates, &templateValues, isFinalAttempt)
		logger.Info("Rendered", "rendered", renderedNum, "exists", existsNum)

		if err = applyTemplates(ctx, r, &gw, templates); err != nil {
			errStatus = fmt.Errorf("unable to apply templates: %w", err)
		}
	}

	requeue = (renderedNum != len(templates))
	logger.Info("ending reconcile loop", "renderedNum", renderedNum, "totalNum", len(templates), "requeue", requeue)

	beforeStatusUpdate := gw.DeepCopy()

	// Update status.addresses field
	tmplStr, found := gwcb.Spec.GatewayTemplate.Status["template"]
	statusUpdateOK := true
	if found {
		statusUpdateOK = false
		templateValues.Resources = buildResourceValues(templates) // Needed in case of a single-pass render loop above
		if tmpl, errs := parseSingleTemplate("status", tmplStr); errs != nil {
			logger.Info("unable to parse status template", "temporary error", errs)
		} else {
			if statusMap, errs := template2maps(tmpl, &templateValues); errs != nil {
				logger.Info("unable to render status template", "temporary error", errs, "template", tmplStr, "values", templateValues)
			} else {
				gw.Status.Addresses = []gatewayapi.GatewayStatusAddress{}
				_, found := statusMap[0]["addresses"] // FIXME, more addresses?
				if found {
					addresses := statusMap[0]["addresses"]
					if errs := mapstructure.Decode(addresses, &gw.Status.Addresses); errs != nil {
						// This is probably not a temporary error
						logger.Error(errs, "unable to decode status data")
					} else {
						statusUpdateOK = true
					}
				}
			}
		}
	}
	if !statusUpdateOK {
		requeue = true
	}

	// TODO: Consider if we can set listener status conditions calculated from child resources
	for _, listener := range gw.Spec.Listeners {
		var status *gatewayapi.ListenerStatus
		for idx := range gw.Status.Listeners { // Locate existing status
			if gw.Status.Listeners[idx].Name == listener.Name {
				break
			}
		}
		if status == nil {
			status = &gatewayapi.ListenerStatus{ // Existing not found, create new
				Name: listener.Name,
				SupportedKinds: []gatewayapi.RouteGroupKind{{
					Group: (*gatewayapi.Group)(&gatewayapi.GroupVersion.Group),
					Kind:  gatewayapi.Kind("HTTPRoute"),
				}},
				AttachedRoutes: int32(len(gwRoutes)), // FIXME, not necessarily correct
			}
		}
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type:               string(gatewayapi.ListenerConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayapi.ListenerReasonAccepted),
			ObservedGeneration: gw.ObjectMeta.Generation})
	}

	// Gateway was accepted as 'ours'
	meta.SetStatusCondition(&gw.Status.Conditions, metav1.Condition{
		Type:               string(gatewayapi.GatewayConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayapi.GatewayReasonAccepted),
		ObservedGeneration: gw.ObjectMeta.Generation})

	// Consider Gateway as 'programmed' when all resources have
	// been templated and applied
	progStatus := metav1.ConditionFalse
	progReason := "Pending"
	progMsg := ""
	if existsNum == len(templates) { // 'Programmed' relates to templates alone
		progStatus = metav1.ConditionTrue
		progReason = string(gatewayapi.GatewayReasonProgrammed)
	} else {
		missing := statusExistingTemplates(templates)
		sort.Strings(missing)
		progMsg = fmt.Sprintf("missing %v resources: %s", len(templates)-existsNum, strings.Join(missing, ","))
	}
	meta.SetStatusCondition(&gw.Status.Conditions, metav1.Condition{
		Type:               string(gatewayapi.GatewayConditionProgrammed),
		Status:             progStatus,
		Reason:             progReason,
		Message:            progMsg,
		ObservedGeneration: gw.ObjectMeta.Generation})

	// Set `Ready` condition based on child resource statuses, status update and programmed status
	status := metav1.ConditionFalse
	isReady, err := statusIsReady(templates)
	if err != nil {
		logger.Error(err, "unable to update status condition due to sub-resource status error")
		return ctrl.Result{}, err
	}
	if isReady && statusUpdateOK && progStatus == metav1.ConditionTrue {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&gw.Status.Conditions, metav1.Condition{
		Type:   string(gatewayapi.GatewayConditionReady),
		Status: status,
		//nolint:staticcheck // ready status is deprecated in gw-api 0.7.0 but since our implementation fits pre-0.7.0 and intended future use we keep the code
		Reason:             string(gatewayapi.GatewayReasonReady),
		ObservedGeneration: gw.ObjectMeta.Generation})

	if !equality.Semantic.DeepEqual(beforeStatusUpdate.Status, gw.Status) {
		if err := r.Client().Status().Update(ctx, &gw); err != nil {
			logger.Error(err, "unable to update Gateway status")
			return ctrl.Result{}, err
		}
	}

	if requeue {
		logger.Info("requeue - not all resources updated")
		return ctrl.Result{RequeueAfter: dependencyMissingRequeuePeriod}, nil
	}
	return ctrl.Result{}, errStatus
}

// Calculate union and intersection of Hostnames for use in templates.
// Union is useful to reduce the number of child resources
// changes. I.e. imagine a Gateway with hostname '*.example.com' and a
// HTTPRoute with 'foo.example.com'. We may want to create a TLS
// certificate using '*.example.com' (intersection) and not
// 'foo.example.com' (union). Calculating both allows template authors
// to choose which to use.
func combineHostnames(gw *gatewayapi.Gateway, rtList []*gatewayapi.HTTPRoute) (union, isect []string) {
	wildcards := sets.New[string]() // Wildcard hostnames without '*.' prefix
	hostnames := sets.New[string]() // Non-wildcard hostnames

	// Add 'hostname' to either 'wildcards' or 'hostnames' depending on presence of '*.' prefix
	addHostname := func(hostname string) {
		if strings.HasPrefix(hostname, "*.") {
			domain := strings.TrimPrefix(hostname, "*.")
			if !wildcards.Has(domain) {
				wildcards = sets.Insert(wildcards, domain)
			}
		} else if !hostnames.Has(hostname) {
			hostnames = sets.Insert(hostnames, hostname)
		}
	}

	for _, l := range gw.Spec.Listeners {
		if l.Hostname != nil {
			addHostname(string(*l.Hostname))
		}
	}
	for _, rt := range rtList {
		for _, rtHostname := range rt.Spec.Hostnames {
			addHostname(string(rtHostname))
		}
	}

	for hostname := range wildcards { // Unique wildcards goes in both union and intersection
		hostname = "*." + hostname
		union = append(union, hostname)
		isect = append(isect, hostname)
	}
	for hostname := range hostnames {
		union = append(union, hostname) // Unique hostnames goes in union
		covered := false
		for wildcard := range wildcards {
			if strings.HasSuffix(hostname, "."+wildcard) || hostname == wildcard {
				covered = true
				break
			}
		}
		if !covered {
			isect = append(isect, hostname)
		}
	}
	return union, isect
}

// Match HTTPRoutes against Gateway listeners, return valid HTTPRoute matches
func filterHTTPRoutesForGateway(gw *gatewayapi.Gateway, rtList []*gatewayapi.HTTPRoute) []*gatewayapi.HTTPRoute {
	rtOut := make([]*gatewayapi.HTTPRoute, 0, len(rtList))
	for _, rt := range rtList {
		for _, pRef := range rt.Spec.ParentRefs {
			if (pRef.Group != nil && *pRef.Group != gatewayapi.Group(gatewayapi.GroupName)) ||
				(pRef.Kind != nil && *pRef.Kind != gatewayapi.Kind("Gateway")) ||
				(pRef.Namespace != nil && *pRef.Namespace != gatewayapi.Namespace(gw.ObjectMeta.Namespace)) ||
				// Unspecified namespace means use HTTPRoute namespace
				(pRef.Namespace == nil && rt.ObjectMeta.Namespace != gw.ObjectMeta.Namespace) ||
				(pRef.Name != gatewayapi.ObjectName(gw.ObjectMeta.Name)) {
				// Skip as ParentRef does not refer to Gateway
				continue
			}
			// FIXME: Also include SectionName and Port in match
			// for _, l := range gw.Spec.Listeners {
			// 	if xxx
			// }
			rtOut = append(rtOut, rt)
		}
	}
	return rtOut
}

// Lookup all HTTPRoutes
func lookupHTTPRoutes(ctx context.Context, r ControllerClient) ([]*gatewayapi.HTTPRoute, error) {
	var rtList gatewayapi.HTTPRouteList

	if err := r.Client().List(ctx, &rtList); err != nil {
		return nil, err
	}

	// Return pointer slice
	rtOut := make([]*gatewayapi.HTTPRoute, 0, len(rtList.Items))
	for idx := range rtList.Items {
		rtOut = append(rtOut, &rtList.Items[idx])
	}
	return rtOut, nil
}
