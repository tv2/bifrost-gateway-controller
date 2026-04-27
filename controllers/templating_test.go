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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
)

// ------------------------------------
// Test Helpers
// ------------------------------------

// newTestParentGateway returns a Gateway suitable for use as a parent in templating tests
func newTestParentGateway() *gatewayapi.Gateway {
	return &gatewayapi.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "parent-gw",
			Namespace: "default",
			UID:       types.UID("parent-uid"),
		},
	}
}

// ------------------------------------
// [parseSingleTemplate] tests
// ------------------------------------

var _ = Describe("parseSingleTemplate", func() {

	It("Should parse a valid template with custom helpers", func() {
		tmpl, err := parseSingleTemplate("test", `{{ toYaml .Values | nindent 2 }}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(tmpl).NotTo(BeNil())
	})

	It("Should return error for invalid template syntax", func() {
		_, err := parseSingleTemplate("test", "key: {{ .Values.foo }")
		Expect(err).To(HaveOccurred())
	})
})

// ------------------------------------
// [parseTemplates] tests
// ------------------------------------

var _ = Describe("parseTemplates", func() {

	It("Should parse a map of templates", func() {
		templates, err := parseTemplates(map[string]string{
			"resource1": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1",
			"resource2": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm2",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(templates[0].TemplateName).To(Equal("resource1"))
		Expect(templates[0].StringTemplate).To(ContainSubstring("name: cm1"))
		Expect(templates[0].Template).NotTo(BeNil())
		Expect(templates[1].TemplateName).To(Equal("resource2"))
		Expect(templates[1].StringTemplate).To(ContainSubstring("name: cm2"))
		Expect(templates[1].Template).NotTo(BeNil())
	})

	It("Should sort templates alphabetically by key", func() {
		templates, err := parseTemplates(map[string]string{
			"cResource": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c",
			"aResource": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a",
			"bResource": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(templates[0].TemplateName).To(Equal("aResource"))
		Expect(templates[1].TemplateName).To(Equal("bResource"))
		Expect(templates[2].TemplateName).To(Equal("cResource"))
	})

	It("Should return error for invalid template in the map", func() {
		_, err := parseTemplates(map[string]string{
			"bad": "{{ .Values.foo }",
		})
		Expect(err).To(HaveOccurred())
	})

	It("Should handle empty map", func() {
		templates, err := parseTemplates(map[string]string{})
		Expect(err).NotTo(HaveOccurred())
		Expect(templates).To(BeEmpty())
	})
})

// ------------------------------------
// [renderTemplates] tests
// ------------------------------------

var _ = Describe("renderTemplates", func() {
	var (
		ctx   = context.Background()
		cmGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	)

	It("Should render a template and count it", func() {
		r := newFakeDynClient()
		tmpl, err := parseSingleTemplate("t1", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1")
		Expect(err).NotTo(HaveOccurred())
		// Pre-populate Resources to skip template2Composite (which needs a REST mapper)
		templates := []*ResourceTemplateState{
			{
				TemplateName:   "t1",
				Template:       tmpl,
				StringTemplate: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1",
				Resources: []ResourceComposite{
					{
						Rendered:     &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "cm1"}}},
						GVR:          &cmGVR,
						IsNamespaced: true,
					},
				},
			},
		}
		rendered, exists := renderTemplates(ctx, r, newTestParentGateway(), templates, &TemplateValues{}, false)
		Expect(rendered).To(Equal(1))
		Expect(exists).To(Equal(0)) // Resource not yet in cluster (fake client has no objects)
	})

	It("Should skip already rendered templates", func() {
		r := newFakeDynClient()
		current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "cm1"}}}
		templates := []*ResourceTemplateState{
			{
				TemplateName: "t1",
				Resources: []ResourceComposite{
					{
						Rendered:     &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "cm1"}}},
						GVR:          &cmGVR,
						IsNamespaced: true,
						Current:      current,
					},
				},
			},
		}
		rendered, exists := renderTemplates(ctx, r, newTestParentGateway(), templates, &TemplateValues{}, false)
		Expect(rendered).To(Equal(1))
		Expect(exists).To(Equal(1))
		// Current should remain unchanged (not re-fetched)
		Expect(templates[0].Resources[0].Current).To(Equal(current))
	})

	It("Should count rendered and exists independently", func() {
		r := newFakeDynClient()
		current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "cm1"}}}
		badTmpl, err := parseSingleTemplate("bad", "val: {{ .Values.missing }}")
		Expect(err).NotTo(HaveOccurred())
		templates := []*ResourceTemplateState{
			// This one has resources and current — should count as rendered + exists
			{
				TemplateName: "good",
				Resources: []ResourceComposite{
					{Rendered: &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "cm1"}}},
						GVR: &cmGVR, IsNamespaced: true, Current: current},
				},
			},
			// This one fails to render — should not count
			{TemplateName: "bad", Template: badTmpl, StringTemplate: "val: {{ .Values.missing }}"},
		}
		rendered, exists := renderTemplates(ctx, r, newTestParentGateway(), templates, &TemplateValues{}, false)
		Expect(rendered).To(Equal(1))
		Expect(exists).To(Equal(1))
	})
})

// ------------------------------------
// [buildResourceValues] tests
// ------------------------------------

var _ = Describe("buildResourceValues", func() {

	It("Should build map from template resource list", func() {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "cm1"},
			"data":       map[string]any{"key": "val"},
		}}
		templates := []*ResourceTemplateState{
			{TemplateName: "myResource", Resources: []ResourceComposite{{Current: u}}},
		}
		vals := buildResourceValues(templates)
		Expect(vals).To(HaveKey("myResource"))
		resources, ok := vals["myResource"].([]map[string]any)
		Expect(ok).To(BeTrue())
		data, ok := resources[0]["data"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(data["key"]).To(Equal("val"))
	})

	It("Should return empty map entries for resources without Current", func() {
		templates := []*ResourceTemplateState{
			{TemplateName: "pending", Resources: []ResourceComposite{{Current: nil}}},
		}
		vals := buildResourceValues(templates)
		Expect(vals).To(HaveKey("pending"))
		resources, ok := vals["pending"].([]map[string]any)
		Expect(ok).To(BeTrue())
		Expect(resources).To(HaveLen(0))
	})

	It("Should handle templates with no resources", func() {
		templates := []*ResourceTemplateState{
			{TemplateName: "empty"},
		}
		vals := buildResourceValues(templates)
		Expect(vals).To(HaveKey("empty"))
	})
})

// ------------------------------------
// [applyTemplates] tests
// ------------------------------------

var _ = Describe("applyTemplates", func() {
	var (
		ctx    = context.Background()
		cmGVR  = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
		crdGVR = schema.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "widgets"}
	)

	newRendered := func(name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": name, "namespace": "default"},
		}}
	}

	It("Should skip resources with nil Rendered", func() {
		r := newFakeDynClient()
		templates := []*ResourceTemplateState{
			{TemplateName: "t1", Resources: []ResourceComposite{
				{Rendered: nil, GVR: &cmGVR, IsNamespaced: true},
			}},
		}
		err := applyTemplates(ctx, r, newTestParentGateway(), templates)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Should skip resources with nil GVR", func() {
		r := newFakeDynClient()
		templates := []*ResourceTemplateState{
			{TemplateName: "t1", Resources: []ResourceComposite{
				{Rendered: newRendered("cm1"), GVR: nil, IsNamespaced: true},
			}},
		}
		err := applyTemplates(ctx, r, newTestParentGateway(), templates)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Should apply namespaced resource with owner reference", func() {
		r := newFakeDynClient()
		templates := []*ResourceTemplateState{
			{TemplateName: "t1", Resources: []ResourceComposite{
				{Rendered: newRendered("cm1"), GVR: &cmGVR, IsNamespaced: true},
			}},
		}
		err := applyTemplates(ctx, r, newTestParentGateway(), templates)
		Expect(err).NotTo(HaveOccurred())
		// Verify owner reference was set on the rendered resource
		ownerRefs := templates[0].Resources[0].Rendered.GetOwnerReferences()
		Expect(ownerRefs[0].UID).To(Equal(types.UID("parent-uid")))
	})

	It("Should apply cluster-scoped resource without owner reference", func() {
		r := newFakeDynClient()
		rendered := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "example.io/v1",
			"kind":       "Widget",
			"metadata":   map[string]any{"name": "my-widget"},
		}}
		templates := []*ResourceTemplateState{
			{TemplateName: "t1", Resources: []ResourceComposite{
				{Rendered: rendered, GVR: &crdGVR, IsNamespaced: false},
			}},
		}
		err := applyTemplates(ctx, r, newTestParentGateway(), templates)
		Expect(err).NotTo(HaveOccurred())
		Expect(rendered.GetOwnerReferences()).To(BeEmpty())
	})

	It("Should return error when SetControllerReference fails", func() {
		r := newFakeDynClient()
		parent := &metav1.ObjectMeta{
			Name:      "no-uid-gw",
			Namespace: "default",
		}
		templates := []*ResourceTemplateState{
			{TemplateName: "t1", Resources: []ResourceComposite{
				{Rendered: newRendered("cm1"), GVR: &cmGVR, IsNamespaced: true},
			}},
		}
		err := applyTemplates(ctx, r, parent, templates)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("found 1 problems while applying 1 templates"))
	})

	It("Should aggregate errors across multiple templates", func() {
		r := newFakeDynClient()
		parent := &metav1.ObjectMeta{Name: "no-uid", Namespace: "default"}
		templates := []*ResourceTemplateState{
			{TemplateName: "t1", Resources: []ResourceComposite{
				{Rendered: newRendered("cm1"), GVR: &cmGVR, IsNamespaced: true},
			}},
			{TemplateName: "t2", Resources: []ResourceComposite{
				{Rendered: newRendered("cm2"), GVR: &cmGVR, IsNamespaced: true},
			}},
		}
		err := applyTemplates(ctx, r, parent, templates)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("found 2 problems while applying 2 templates"))
	})
})

// ------------------------------------
// [helperToYaml] tests
// ------------------------------------

var _ = Describe("helperToYaml", func() {

	It("Should marshal a simple map to YAML", func() {
		result := helperToYaml(map[string]any{"key": "value"})
		Expect(result).To(ContainSubstring("key: value"))
	})

	It("Should marshal a list", func() {
		result := helperToYaml([]string{"a", "b"})
		Expect(result).To(ContainSubstring("- a"))
		Expect(result).To(ContainSubstring("- b"))
	})

	It("Should handle nil", func() {
		result := helperToYaml(nil)
		Expect(result).To(Equal("null"))
	})
})

// ------------------------------------
// [template2maps] tests
// ------------------------------------

var _ = Describe("template2maps", func() {
	ctx := context.Background()

	It("Should render a single document", func() {
		tmpl, err := parseSingleTemplate("test", "key1: val1\nkey2: val2")
		Expect(err).NotTo(HaveOccurred())
		values := &TemplateValues{}
		maps, err := template2maps(ctx, tmpl, values)
		Expect(err).NotTo(HaveOccurred())
		Expect(maps[0]["key1"]).To(Equal("val1"))
		Expect(maps[0]["key2"]).To(Equal("val2"))
	})

	It("Should render multiple documents", func() {
		tmpl, err := parseSingleTemplate("test", "name: doc1\n---\nname: doc2\n---\nname: doc3")
		Expect(err).NotTo(HaveOccurred())
		values := &TemplateValues{}
		maps, err := template2maps(ctx, tmpl, values)
		Expect(err).NotTo(HaveOccurred())
		Expect(maps[0]["name"]).To(Equal("doc1"))
		Expect(maps[1]["name"]).To(Equal("doc2"))
		Expect(maps[2]["name"]).To(Equal("doc3"))
	})

	It("Should use template values in rendering", func() {
		tmpl, err := parseSingleTemplate("test", "val: {{ .Values.myKey }}")
		Expect(err).NotTo(HaveOccurred())
		values := &TemplateValues{Values: map[string]any{"myKey": "hello"}}
		maps, err := template2maps(ctx, tmpl, values)
		Expect(err).NotTo(HaveOccurred())
		Expect(maps[0]["val"]).To(Equal("hello"))
	})
})

// ------------------------------------
// [template2Composite] tests
// ------------------------------------

var _ = Describe("template2Composite", func() {
	ctx := context.Background()

	It("Should convert a single-document template into a ResourceComposite", func() {
		r := newFakeClientWithMapper()
		tmpl, err := parseSingleTemplate("test", "apiVersion: gateway.networking.k8s.io/v1\nkind: Gateway\nmetadata:\n  name: my-gw")
		Expect(err).NotTo(HaveOccurred())
		composites, err := template2Composite(ctx, r, tmpl, &TemplateValues{})
		Expect(err).NotTo(HaveOccurred())
		Expect(composites[0].Rendered.GetName()).To(Equal("my-gw"))
		Expect(composites[0].Rendered.GetKind()).To(Equal("Gateway"))
		Expect(composites[0].GVR).NotTo(BeNil())
		Expect(composites[0].GVR.Resource).To(Equal("gateways"))
		Expect(composites[0].IsNamespaced).To(BeTrue())
	})

	It("Should convert a multi-document template into multiple ResourceComposites", func() {
		r := newFakeClientWithMapper()
		tmpl, err := parseSingleTemplate("test",
			"apiVersion: gateway.networking.k8s.io/v1\nkind: Gateway\nmetadata:\n  name: gw1\n---\napiVersion: gateway.networking.k8s.io/v1\nkind: Gateway\nmetadata:\n  name: gw2")
		Expect(err).NotTo(HaveOccurred())
		composites, err := template2Composite(ctx, r, tmpl, &TemplateValues{})
		Expect(err).NotTo(HaveOccurred())
		Expect(composites[0].Rendered.GetName()).To(Equal("gw1"))
		Expect(composites[1].Rendered.GetName()).To(Equal("gw2"))
	})

	It("Should return error when template rendering fails", func() {
		r := newFakeClientWithMapper()
		tmpl, err := parseSingleTemplate("test", "val: {{ .Values.missing }}")
		Expect(err).NotTo(HaveOccurred())
		_, err = template2Composite(ctx, r, tmpl, &TemplateValues{})
		Expect(err).To(HaveOccurred())
	})

	It("Should return error when GVR lookup fails for unknown kind", func() {
		r := newFakeClientWithMapper()
		tmpl, err := parseSingleTemplate("test", "apiVersion: unknown.io/v1\nkind: NoSuchKind\nmetadata:\n  name: x")
		Expect(err).NotTo(HaveOccurred())
		_, err = template2Composite(ctx, r, tmpl, &TemplateValues{})
		Expect(err).To(HaveOccurred())
	})
})

// ------------------------------------
// [objectToMap] tests
// ------------------------------------

var _ = Describe("objectToMap", func() {

	It("Should convert a Gateway to a map", func() {
		gw := &gatewayapi.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
			Spec: gatewayapi.GatewaySpec{
				GatewayClassName: "my-class",
			},
		}
		m, err := objectToMap(gw)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(HaveKey("metadata"))
		Expect(m).To(HaveKey("spec"))
		metadata, ok := m["metadata"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(metadata["name"]).To(Equal("test-gw"))
		Expect(metadata["namespace"]).To(Equal("default"))
		spec, ok := m["spec"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(spec["gatewayClassName"]).To(Equal("my-class"))
	})

	It("Should handle unstructured object", func() {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "cm1"},
		}}
		m, err := objectToMap(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(m["kind"]).To(Equal("ConfigMap"))
	})
})
