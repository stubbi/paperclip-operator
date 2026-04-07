package resources

import (
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	paperclipv1alpha1 "github.com/paperclipinc/paperclip-operator/api/v1alpha1"
)

// BuildHTTPRoute constructs the HTTPRoute for an Instance.
func BuildHTTPRoute(instance *paperclipv1alpha1.Instance) *gatewayv1.HTTPRoute {
	route := instance.Spec.Networking.HTTPRoute
	if route == nil || !route.Enabled {
		return nil
	}

	parentRefs := make([]gatewayv1.ParentReference, 0, len(route.ParentRefs))
	for _, ref := range route.ParentRefs {
		parentRef := gatewayv1.ParentReference{
			Name: gatewayv1.ObjectName(ref.Name),
		}
		if ref.Namespace != nil {
			namespace := gatewayv1.Namespace(*ref.Namespace)
			parentRef.Namespace = &namespace
		}
		if ref.SectionName != nil {
			sectionName := gatewayv1.SectionName(*ref.SectionName)
			parentRef.SectionName = &sectionName
		}
		parentRefs = append(parentRefs, parentRef)
	}

	var hostnames []gatewayv1.Hostname
	if len(route.Hostnames) > 0 {
		hostnames = make([]gatewayv1.Hostname, 0, len(route.Hostnames))
		for _, host := range route.Hostnames {
			hostnames = append(hostnames, gatewayv1.Hostname(host))
		}
	}

	pathPrefix := route.PathPrefix
	if pathPrefix == "" {
		pathPrefix = "/"
	}

	pathMatchType := gatewayv1.PathMatchPathPrefix
	port := gatewayv1.PortNumber(servicePort(instance))
	weight := int32(1)

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: ObjectMeta(instance, HTTPRouteName(instance)),
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Hostnames: hostnames,
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  Ptr(pathMatchType),
								Value: Ptr(pathPrefix),
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(ServiceName(instance)),
									Port: &port,
								},
								Weight: &weight,
							},
						},
					},
				},
			},
		},
	}

	if route.Annotations != nil {
		httpRoute.Annotations = route.Annotations
	}

	return httpRoute
}
