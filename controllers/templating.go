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
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	sigsyaml "sigs.k8s.io/yaml"
)

// Information about a resource, rendered format as well as actual in API server
type ResourceComposite struct {
	// The rendered resource
	Rendered *unstructured.Unstructured

	// GVR for resource
	GVR *schema.GroupVersionResource

	// Current resource fetch from API-server (or as close as our local caching allows)
	Current *unstructured.Unstructured

	// Whether resource is namespaced or not
	IsNamespaced bool
}

// Rendering and applying templates is a multi-stage process. This
// structure holds information about a template between stages
type ResourceTemplateState struct {
	// Compiled template
	Template *template.Template

	// Name of template (from template key in GatewayClassBlueprint, not Kubernetes resource name)
	TemplateName string

	// Raw template
	StringTemplate string

	// Resource information, rendered and current
	Resources []ResourceComposite
}

// Parameters used when rendering templates
type TemplateValues struct {
	// Parent Gateway, always defined
	Gateway *map[string]any

	// Parent HTTPRoute. Only set when rendering HTTPRoute templates
	HTTPRoute map[string]any

	// Template values
	Values map[string]any

	// Current resources (i.e. sibling resources)
	Resources map[string]any

	// List of all hostnames across all listeners and attached
	// HTTPRoutes. These lists of hostnames are particularly
	// useful for TLS certificates which are not port specific.
	Hostnames TemplateHostnameValues
}

type TemplateHostnameValues struct {
	// Union and intersection of all hostnames across all
	// listeners and attached HTTPRoutes (with duplicates
	// removed). Intersection holds all hostnames from Union with
	// duplicates covered by wildcards removed.
	Union, Intersection []string
}

// Parse a single template with our additional functions added
func parseSingleTemplate(tmplKey, tmpl string) (*template.Template, error) {
	var funcs = template.FuncMap{
		"toYaml": helperToYaml,
	}
	return template.New(tmplKey).Option("missingkey=error").Funcs(sprig.TxtFuncMap()).Funcs(funcs).Parse(tmpl)
}

// Initialize ResourceTemplateState slice by parsing templates
func parseTemplates(resourceTemplates map[string]string) ([]*ResourceTemplateState, error) {
	var err error
	logger := ctrl.Log.WithName("templating")

	templates := make([]*ResourceTemplateState, 0, len(resourceTemplates))

	for tmplKey, tmpl := range resourceTemplates {
		r := ResourceTemplateState{}
		r.TemplateName = tmplKey
		r.StringTemplate = tmpl
		r.Template, err = parseSingleTemplate(tmplKey, tmpl)
		if err != nil {
			metricTemplateParseErrs.Inc()
			return nil, fmt.Errorf("cannot parse template %q: %w", tmplKey, err)
		}
		r.Resources = make([]ResourceComposite, 0)
		templates = append(templates, &r)
	}

	// Sort to increase predictability
	sort.SliceStable(templates, func(i, j int) bool { return templates[i].TemplateName < templates[j].TemplateName })

	templateNames := make([]string, 0, len(templates))
	for _, t := range templates {
		templateNames = append(templateNames, t.TemplateName)
	}
	logger.V(1).Info("parsed templates", "count", len(templates), "names", templateNames)

	return templates, nil
}

// Attempt to render templates and get current resource, skipping
// resources that have already been rendered/fetched. Note that
// fetching current resource from API server/cache require that we can
// render the template first. Rendering errors on final attempt are
// logged as errors.
func renderTemplates(ctx context.Context, r ControllerDynClient, parent metav1.Object,
	templates []*ResourceTemplateState, values *TemplateValues, isFinalAttempt bool) (rendered, exists int) {
	var err error

	logger := log.FromContext(ctx)
	ns := parent.GetNamespace()

	for tIdx := range templates {
		tmpl := templates[tIdx]
		if len(tmpl.Resources) == 0 {
			tmpl.Resources, err = template2Composite(ctx, r, tmpl.Template, values)
			if err != nil {
				if isFinalAttempt {
					logger.Error(err, "cannot render template", "templateName", tmpl.TemplateName)
					logger.V(2).Info("template render failure details", "templateName", tmpl.TemplateName, "template", tmpl.StringTemplate, "values", values)
					metricTemplateErrs.Inc()
				} else {
					logger.V(1).Info("template render deferred (missing dependency)", "templateName", tmpl.TemplateName, "error", err)
				}
				continue
			}
			logger.V(1).Info("template rendered", "templateName", tmpl.TemplateName, "resources", len(tmpl.Resources))
		}
		rendered++
		for resIdx := range tmpl.Resources {
			res := &tmpl.Resources[resIdx]
			if res.Current == nil {
				var dynamicClient dynamic.ResourceInterface
				if res.IsNamespaced {
					dynamicClient = r.DynamicClient().Resource(*res.GVR).Namespace(ns)
				} else {
					dynamicClient = r.DynamicClient().Resource(*res.GVR)
				}
				metricResourceGet.Inc()
				res.Current, err = dynamicClient.Get(ctx, res.Rendered.GetName(), metav1.GetOptions{})
				if err != nil {
					logger.V(1).Info("current resource not yet available", "templateName", tmpl.TemplateName, "resourceName", res.Rendered.GetName(), "gvr", res.GVR, "error", err)
					continue
				}
				logger.V(1).Info("fetched current resource", "templateName", tmpl.TemplateName, "resourceName", res.Rendered.GetName(), "gvr", res.GVR)
			} else {
				logger.V(1).Info("current resource already cached", "templateName", tmpl.TemplateName, "resourceName", res.Rendered.GetName())
			}
		}
		exists++
	}
	return rendered, exists
}

// Build a map of values from current resources. Useful for
// referencing values between resources, e.g. a status field from one
// resource may be used to template another resource
func buildResourceValues(templates []*ResourceTemplateState) map[string]any {
	resourceValues := map[string]any{}

	for _, tmpl := range templates {
		resSlice := make([]map[string]any, 0)
		for _, res := range tmpl.Resources {
			if res.Current != nil {
				resSlice = append(resSlice, res.Current.UnstructuredContent())
			}
		}
		resourceValues[tmpl.TemplateName] = resSlice
	}
	return resourceValues
}

// Apply a list of pre-rendered templates and set owner reference for
// namespaced resources
func applyTemplates(ctx context.Context, r ControllerDynClient, parent metav1.Object, templates []*ResourceTemplateState) error {
	var err error
	var errorCnt = 0

	logger := log.FromContext(ctx)

	for _, tmpl := range templates {
		for _, res := range tmpl.Resources {
			if res.Rendered == nil || res.GVR == nil {
				logger.V(1).Info("skipping unrendered resource", "templateName", tmpl.TemplateName)
				continue
			}
			if res.IsNamespaced {
				// Only namespaced objects can have namespaced object as owner
				err = ctrl.SetControllerReference(parent, res.Rendered, r.Scheme())
				if err != nil {
					logger.Error(err, "cannot set owner for namespaced template", "templateName", tmpl.TemplateName, "resourceName", res.Rendered.GetName())
					errorCnt++
				} else {
					ns := parent.GetNamespace()
					err = patchUnstructured(ctx, r, res.Rendered, res.GVR, &ns)
					if err != nil {
						logger.Error(err, "cannot apply namespaced template", "templateName", tmpl.TemplateName, "resourceName", res.Rendered.GetName(), "namespace", ns, "gvr", res.GVR)
						errorCnt++
					} else {
						logger.V(1).Info("applied namespaced resource", "templateName", tmpl.TemplateName, "resourceName", res.Rendered.GetName(), "namespace", ns, "gvr", res.GVR)
					}
				}
			} else {
				err = patchUnstructured(ctx, r, res.Rendered, res.GVR, nil)
				if err != nil {
					logger.Error(err, "cannot apply cluster-scoped template", "templateName", tmpl.TemplateName, "resourceName", res.Rendered.GetName(), "gvr", res.GVR)
					errorCnt++
				} else {
					logger.V(1).Info("applied cluster-scoped resource", "templateName", tmpl.TemplateName, "resourceName", res.Rendered.GetName(), "gvr", res.GVR)
				}
			}
		}
	}

	if errorCnt > 0 {
		return fmt.Errorf("found %v problems while applying %v templates", errorCnt, len(templates))
	}
	return nil
}

// This function is made available to templates as 'toYaml'
func helperToYaml(v interface{}) string {
	logger := ctrl.Log.WithName("templating")
	data, err := sigsyaml.Marshal(v)
	if err != nil {
		logger.V(1).Info("toYaml marshal error", "error", err)
		return ""
	}
	return strings.TrimSuffix(string(data), "\n")
}

func templateRender(ctx context.Context, tmpl *template.Template, templateValues *TemplateValues) (*bytes.Buffer, error) {
	logger := log.FromContext(ctx)
	var buffer bytes.Buffer

	if err := tmpl.Execute(io.Writer(&buffer), templateValues); err != nil {
		return nil, err
	}

	logger.V(2).Info("rendered template output", "output", buffer.String(), "values", templateValues)

	return &buffer, nil
}

func template2maps(ctx context.Context, tmpl *template.Template, tmplValues *TemplateValues) ([]map[string]any, error) {
	renderBuffer, err := templateRender(ctx, tmpl, tmplValues)
	if err != nil {
		return nil, err
	}

	rawSlice := bytes.SplitN(renderBuffer.Bytes(), []byte("---"), -1)
	resources := make([]map[string]any, 0, len(rawSlice))
	for _, raw := range rawSlice {
		r := map[string]any{}
		err = yaml.Unmarshal(raw, &r)
		if err != nil {
			return nil, err
		}
		if len(r) == 0 {
			continue // Empty resource
		}
		resources = append(resources, r)
	}
	return resources, nil
}

func template2Composite(ctx context.Context, r ControllerClient, tmpl *template.Template, tmplValues *TemplateValues) ([]ResourceComposite, error) {
	rawResources, err := template2maps(ctx, tmpl, tmplValues)
	if err != nil {
		return nil, err
	}
	composites := make([]ResourceComposite, 0, len(rawResources))
	for rawIdx := range rawResources {
		c := ResourceComposite{}
		c.Rendered = &unstructured.Unstructured{Object: rawResources[rawIdx]}
		c.GVR, c.IsNamespaced, err = unstructuredToGVR(r, c.Rendered)
		if err != nil {
			return nil, err
		}
		composites = append(composites, c)
	}
	return composites, nil
}

// Prepare a resource like Gateway or HTTPRoute for use in templates
// by converting to map[string]any
func objectToMap(obj runtime.Object) (map[string]any, error) {
	mapObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %+v: %w", obj, err)
	}
	return mapObj, nil
}
