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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
)

// GatewayClassReconciler reconciles a GatewayClass object
type GatewayClassReconciler struct {
	client client.Client
	scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/finalizers,verbs=update

//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayclassblueprints,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayclassblueprints/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayclassblueprints/finalizers,verbs=update

//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayclassconfigs,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayclassconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayclassconfigs/finalizers,verbs=update

//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayconfigs,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.tv2.dk,resources=gatewayconfigs/finalizers,verbs=update

func (r *GatewayClassReconciler) Client() client.Client {
	return r.client
}

func (r *GatewayClassReconciler) Scheme() *runtime.Scheme {
	return r.scheme
}

func NewGatewayClassController(mgr ctrl.Manager) *GatewayClassReconciler {
	r := &GatewayClassReconciler{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		//dynClient: dynamic.NewForConfigOrDie(ctrl.GetConfigOrDie()),
	}
	return r
}

func (r *GatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayapi.GatewayClass{}).
		Complete(r)
}

func (r *GatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconcile started", "gatewayClass", req.Name)

	var valid = true
	var errWhyInvalid error

	gwc, err := lookupGatewayClass(ctx, r, gatewayapi.ObjectName(req.Name))
	if err != nil {
		logger.Info("GatewayClass not found", "gatewayClass", req.Name)
		return ctrl.Result{}, nil
	}

	if !isOurGatewayClass(gwc) {
		logger.Info("skipping GatewayClass not managed by us", "gatewayClass", req.Name, "controllerName", gwc.Spec.ControllerName)
		return ctrl.Result{}, nil
	}

	_, err = lookupGatewayClassBlueprint(ctx, r, gwc)
	if err != nil {
		valid = false
		errWhyInvalid = fmt.Errorf("blueprint for GatewayClass '%s' not found", gwc.Name)
		logger.Info("blueprint not found for GatewayClass", "gatewayClass", gwc.Name, "parametersRef", gwc.Spec.ParametersRef)
	}

	if valid {
		logger.V(1).Info("Accepted", "gatewayClass", req.Name)
		meta.SetStatusCondition(&gwc.Status.Conditions, metav1.Condition{
			Type:               string(gatewayapi.GatewayClassConditionStatusAccepted),
			Status:             "True",
			Reason:             string(gatewayapi.GatewayClassReasonAccepted),
			ObservedGeneration: gwc.Generation})
	} else {
		logger.V(1).Info("InvalidParameters", "gatewayClass", req.Name, "reason", errWhyInvalid)
		meta.SetStatusCondition(&gwc.Status.Conditions, metav1.Condition{
			Type:               string(gatewayapi.GatewayClassConditionStatusAccepted),
			Status:             "False",
			Reason:             string(gatewayapi.GatewayClassReasonInvalidParameters),
			ObservedGeneration: gwc.Generation})
	}

	err = r.Client().Status().Update(ctx, gwc)
	if err != nil {
		logger.Error(err, "failed to update GatewayClass status", "gatewayClass", req.Name, "valid", valid)
		return ctrl.Result{}, fmt.Errorf("failed to update GatewayClass status condition: %w", err)
	}
	logger.V(1).Info("status updated", "gatewayClass", req.Name, "accepted", valid)

	if !valid {
		return ctrl.Result{RequeueAfter: dependencyMissingRequeuePeriod}, errWhyInvalid
	}
	logger.Info("reconcile completed successfully", "gatewayClass", req.Name)
	return ctrl.Result{}, nil
}
