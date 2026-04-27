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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ------------------------------------
// [statusIsReady] tests
// ------------------------------------

var _ = Describe("statusIsReady", func() {

	It("Should return true for empty templates", func() {
		ready, _, err := statusIsReady(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(ready).To(BeTrue())
	})

	It("Should return false when a resource has no Current", func() {
		templates := []*ResourceTemplateState{
			{Resources: []ResourceComposite{{Current: nil}}},
		}
		ready, _, err := statusIsReady(templates)
		Expect(err).NotTo(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("Should return false when a resource is not ready", func() {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata":   map[string]any{"name": "dep1", "generation": int64(2)},
			"status": map[string]any{
				"observedGeneration": int64(1),
				"conditions": []any{
					map[string]any{
						"type":   "Available",
						"status": "False",
						"reason": "MinimumReplicasUnavailable",
					},
				},
			},
		}}
		templates := []*ResourceTemplateState{
			{Resources: []ResourceComposite{{Current: u}}},
		}
		ready, _, err := statusIsReady(templates)
		Expect(err).NotTo(HaveOccurred())
		Expect(ready).To(BeFalse())
	})

	It("Should return true when all resources have Current with ready status", func() {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "cm1", "generation": int64(1)},
		}}
		templates := []*ResourceTemplateState{
			{Resources: []ResourceComposite{{Current: u}}},
		}
		ready, _, err := statusIsReady(templates)
		Expect(err).NotTo(HaveOccurred())
		Expect(ready).To(BeTrue())
	})
})

// ------------------------------------
// [statusExistingTemplates] tests
// ------------------------------------

var _ = Describe("statusExistingTemplates", func() {

	It("Should return empty for all-existing resources", func() {
		u := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "x"}}}
		templates := []*ResourceTemplateState{
			{TemplateName: "t1", Resources: []ResourceComposite{{Current: u}}},
		}
		missing := statusExistingTemplates(templates)
		Expect(missing).To(BeEmpty())
	})

	It("Should report template name with [] when no resources rendered", func() {
		templates := []*ResourceTemplateState{
			{TemplateName: "configMapTest"},
		}
		missing := statusExistingTemplates(templates)
		Expect(missing).To(ConsistOf("configMapTest[]"))
	})

	It("Should report template name with index for missing Current", func() {
		templates := []*ResourceTemplateState{
			{TemplateName: "multiResource", Resources: []ResourceComposite{
				{Current: &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "x"}}}},
				{Current: nil},
			}},
		}
		missing := statusExistingTemplates(templates)
		Expect(missing).To(ConsistOf("multiResource[1]"))
	})

	It("Should report multiple missing across templates", func() {
		templates := []*ResourceTemplateState{
			{TemplateName: "a", Resources: []ResourceComposite{{Current: nil}}},
			{TemplateName: "b"},
		}
		missing := statusExistingTemplates(templates)
		Expect(missing).To(ConsistOf("a[0]", "b[]"))
	})
})
