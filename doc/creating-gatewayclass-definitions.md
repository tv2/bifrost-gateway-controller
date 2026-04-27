# Creating GatewayClass Definitions

This document describes how to create new `GatewayClass`
definitions. See also [Example GatewayClass
Definitions](../blueprints/README.md) for the definitions provided
with the *bifrost-gateway-controller*.

Before preparing new `GatewayClass` definitions, it is important to
understand the normalization implemented by the controller, since
`GatewayClass` definitions typically will use the normalized
specifications to define actual data path resources. See
[Normalization of Gateway Resources](normalization.md).

## `GatewayClass` and `GatewayClassBlueprint` Resources

The actual implementation of data-paths are defined by
`GatewayClassBlueprint` resources and the purpose of `GatewayClass`
resources is to name a given class, reference a
`GatewayClassBlueprint` resource and map the class to a specific
controller. See also [Gateway API on GatewayClass
documentation](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1beta1.GatewayClass)

The `GatewayClassBlueprint` is a specific extension of this
controller.

A `GatewayClass` resource may look like the following. Note how we
specify our own controller name and a `default-gateway-class` resource
of kind `GatewayClassBlueprint` for parameters associated with the
`GatewayClass`:

```yaml
apiVersion: gateway.networking.k8s.io/v1beta1
kind: GatewayClass
metadata:
  name: example
spec:
  controllerName: "github.com/tv2/bifrost-gateway-controller"
  parametersRef:
    group: v1alpha1
    kind: GatewayClassBlueprint
    name: default-gateway-class
```

A `GatewayClassBlueprint` contains templates for the sub-resource(s)
that implement the data-path. There are template(s) related to both
`Gateway` and `HTTPRoute` resources, and the general format is shown
below (with template details left out):

```yaml
apiVersion: gateway.tv2.dk/v1alpha1
kind: GatewayClassBlueprint
metadata:
  name: default-gateway-class
spec:

  # The following are templates used to 'implement' a 'parent' Gateway
  gatewayTemplate:
    resourceTemplates:
      # ... actual templates go here

  # The following are templates used to 'implement' a 'parent' HTTPRoute
  httpRouteTemplate:
    resourceTemplates:
      # ... actual templates go here
```

`Gateway` and `HTTPRoute` resources are handled independently.
I.e. when a `Gateway` resource is defined using a `GatewayClass` for
our controller, 'shadow' resources will be created using the
template(s) defined under `gatewayTemplate.resourceTemplates` in the
`GatewayClassBlueprint` associated with the given
`GatewayClass`. Similarly, 'shadow' resources will be created for
`HTTPRoute` resources using the templates under
`httpRouteTemplate.resourceTemplates`

Templates are Golang YAML templates (similar to e.g. Helm), and
includes support for the 100+ functions from the [Sprig
library](http://masterminds.github.io/sprig) as well as a `toYaml`
function.

Typically templates will result in a single resource, but conditionals
and loops may result in templates rendering to zero or more than one
resource. This is supported but should be used with caution.

Consideration for multi-resource templates:

- Resources should be separated by a line with `---` (like in Helm).

- The template as a whole is a single unit in the graph of resources,
  i.e. individual resources in a template cannot refer to each other
  using the `.Resources` method described below. References across
  templates using multiple resources are supported.

- It is supported to mix resource kinds in a single template, however,
  consider if it would be more appropriate to use separate templates
  in such cases.

## Namespaced Resources

Namespace-scoped templated resources are always created in the
namespace of the parent resource, e.g. a resource defined under
`gatewayTemplate` will be created in the namespace of the parent
`Gateway` resource.

## Inter-resource References

Resources may reference other resources, e.g. a `status` field from
one resource can be used in the template of another resource. When
resources have been created in the API server, they will be available
for templating of other resources. However, the dependencies must be a
directed acyclic graph.

When a resource template can be rendered without missing references,
the rendered template will be used to retrieve the current version of
the resource from the API server. These 'current resources' will be
made available as template variables under `.Resources` and the name
of the template. **Since a template may render to more than one
resource, the `.Resources` variable is a list**.

The following excerpt from a `GatewayClassBlueprint` illustrates how a
value is read from the status field of one resource `LBTargetGroup`
and how the `status.atProvider.arn` value is used in the template of
`TargetGroupBinding` through `.Resources.LBTargetGroup`. The use of
the `index` function is because we refer to the first resource
rendered from the `LBTargetGroup` template. This is necessary even if
we in this case know that there is always only a single resource
rendered from the template.

```yaml
...
spec:
  gatewayTemplate:
    resourceTemplates:
      LBTargetGroup: |
        kind: LBTargetGroup
        metadata:
          name: gw-{{ .Gateway.metadata.namespace }}-{{ .Gateway.metadata.name }}
        spec:
          ...
          # This resource will have its 'status.atProvider.arn' set when resource is created
          ...
      TargetGroupBinding: |
        kind: TargetGroupBinding
          ...
        spec:
          # And here we use the value 'status.atProvider.arn' from the 'LBTargetGroup' resource
          targetGroupARN: {{ (index .Resources.LBTargetGroup 0).status.atProvider.arn }}
```

The following figure illustrates variables available to templates,
including normalization and inter-resource variables:

![Template variables](doc/images/template-variables.png)

## Available Templating Variables

This section documents the variables that are available for templates
in `GatewayClassBlueprint`.

The following structure is passed when rendering `Gateway` and
`HTTPRoute` templates:

```go
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
```

The `Gateway` field of the structure above holds the parent `Gateway`
and fields can be referenced in the template as shown in the excerpt
below:

```yaml
  metadata:
    name: {{ .Gateway.metadata.name }}-child
    namespace: {{ .Gateway.metadata.namespace }}
```

Note, that if a `HTTPRoute` is attached to multiple `Gateway`s (which
may be using different `GatewayClassBlueprint`), rendering of the
`HTTPRoute` will be done independently for each parent `Gateway` the
`HTTPRoute` is attached to. The `ParentRef` field will contain the
specific parent Gateway.
