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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	metricPatchApply = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bifrost_patchapply_total",
			Help: "Number of server-side patch operations",
		},
	)
	metricPatchApplyErrs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bifrost_patchapply_errors_total",
			Help: "Number of server-side patch errors",
		},
	)
	metricTemplateErrs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bifrost_template_errors_total",
			Help: "Number of template render errors",
		},
	)
	metricTemplateParseErrs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bifrost_template_parse_errors_total",
			Help: "Number of template parse errors",
		},
	)
	metricResourceGet = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bifrost_resource_get_total",
			Help: "Number of resources fetched to use as dependency in templates",
		},
	)
)

func init() {
	metrics.Registry.MustRegister(metricPatchApply, metricPatchApplyErrs, metricTemplateErrs, metricTemplateParseErrs, metricResourceGet)
}
