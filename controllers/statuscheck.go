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
	"fmt"

	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// Given a slice of template states, compute the overall
// health/readiness status.  The general approach is to test for a
// `Ready` status condition, which is implemented through kstatus.
func statusIsReady(templates []*ResourceTemplateState) (bool, string, error) {
	for _, tmpl := range templates {
		for _, res := range tmpl.Resources {
			if res.Current == nil {
				return false, fmt.Sprintf("resource %q not yet available", tmpl.TemplateName), nil
			}
			res, err := status.Compute(res.Current)
			if err != nil {
				return false, "", err
			}
			if res.Status != status.CurrentStatus {
				return false, fmt.Sprintf("resource %q not ready, status: %s", tmpl.TemplateName, res.Status), nil
			}
		}
	}
	return true, "", nil
}

// Build a list of template names which are not yet reconciled. Useful for status reporting
func statusExistingTemplates(templates []*ResourceTemplateState) []string {
	var missing []string
	for _, tmpl := range templates {
		if len(tmpl.Resources) == 0 {
			// No resources - we treat this an e.g. a render error and mark the unknown resulting number of resources with '[]'
			missing = append(missing, fmt.Sprintf("%s[]", tmpl.TemplateName))
		} else {
			for resIdx, res := range tmpl.Resources {
				if res.Current == nil {
					missing = append(missing, fmt.Sprintf("%s[%d]", tmpl.TemplateName, resIdx))
				}
			}
		}
	}
	return missing
}
