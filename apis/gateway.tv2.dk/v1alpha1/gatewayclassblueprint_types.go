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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A ResourceTemplate is a map with templates for individual resources.
type ResourceTemplate struct {
	ResourceTemplates map[string]string `json:"resourceTemplates,omitempty"`
}

// A ResourceStatusSpec defines how the parent resource status should be updated
// FIXME: Improve schema
type ResourceStatusSpec struct {
	Status map[string]string `json:"status,omitempty"`
}

// A ResourceSpec defines how a gateway API resource like `Gateway`
// and `HTTPRoute` should be implemented through child resource
// templates and parent status updates
type ResourceSpec struct {
	ResourceStatusSpec `json:",inline"`
	ResourceTemplate   `json:",inline"`
}

type GatewayClassBlueprintSpec struct {
	// Template for hardcoded values
	//
	// +optional
	Values TemplateValues `json:"values,omitempty"`

	// Template for child resources created from Gateways
	//
	// +optional
	GatewayTemplate ResourceSpec `json:"gatewayTemplate,omitempty"`

	// Template for child resources created from HTTPRoutes
	//
	// +optional
	HTTPRouteTemplate ResourceSpec `json:"httpRouteTemplate,omitempty"`
}

type GatewayClassBlueprintStatus struct {
	// Conditions is the current status from the controller for
	// this GatewayClassParameter. Updates follow the same
	// specification as conditions for GatewayClass.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Accepted", status: "Unknown", message: "Waiting for controller", reason: "Pending", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:resource:scope=Cluster
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GatewayClassBlueprint represents parameters and settings for a specific GatewayClass.
type GatewayClassBlueprint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayClassBlueprintSpec   `json:"spec,omitempty"`
	Status GatewayClassBlueprintStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

type GatewayClassBlueprintList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatewayClassBlueprint `json:"items"`
}
