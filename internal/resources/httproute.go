package resources

import (
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	paperclipv1alpha1 "github.com/paperclipinc/paperclip-operator/api/v1alpha1"
)

// BuildHTTPRoute constructs a Gateway API HTTPRoute for a Instance.
func BuildHTTPRoute(instance *paperclipv1alpha1.Instance) *gatewayapiv1.HTTPRoute {
	spec := instance.Spec.Networking.HTTPRoute
	if spec == nil {
		return nil
	}

	port := gatewayapiv1.PortNumber(servicePort(instance))
	svcName := gatewayapiv1.ObjectName(ServiceName(instance))

	parentRefs := make([]gatewayapiv1.ParentReference, 0, len(spec.ParentRefs))
	for _, ref := range spec.ParentRefs {
		pr := gatewayapiv1.ParentReference{
			Name: gatewayapiv1.ObjectName(ref.Name),
		}
		if ref.Namespace != nil {
			ns := gatewayapiv1.Namespace(*ref.Namespace)
			pr.Namespace = &ns
		}
		if ref.SectionName != nil {
			sn := gatewayapiv1.SectionName(*ref.SectionName)
			pr.SectionName = &sn
		}
		parentRefs = append(parentRefs, pr)
	}

	hostnames := make([]gatewayapiv1.Hostname, 0, len(spec.Hostnames))
	for _, h := range spec.Hostnames {
		hostnames = append(hostnames, gatewayapiv1.Hostname(h))
	}

	pathMatch := gatewayapiv1.PathMatchPathPrefix
	path := "/"

	route := &gatewayapiv1.HTTPRoute{
		ObjectMeta: ObjectMeta(instance, HTTPRouteName(instance)),
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Hostnames: hostnames,
			Rules: []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  &pathMatch,
								Value: &path,
							},
						},
					},
					BackendRefs: []gatewayapiv1.HTTPBackendRef{
						{
							BackendRef: gatewayapiv1.BackendRef{
								BackendObjectReference: gatewayapiv1.BackendObjectReference{
									Name: svcName,
									Port: &port,
								},
							},
						},
					},
				},
			},
		},
	}

	if spec.Annotations != nil {
		route.Annotations = spec.Annotations
	}

	return route
}
