// Package controllerconfig removes subscription-controlled management endpoints.
package controllerconfig

import "strings"

func Strip(doc map[string]any) {
	for key := range doc {
		if key == "secret" || key == "external-controller" || strings.HasPrefix(key, "external-controller-") ||
			key == "external-doh-server" || key == "external-ui" || strings.HasPrefix(key, "external-ui-") {
			delete(doc, key)
		}
	}
}
