//go:build !darwin

package sysproxy

import "net/http"

type ServiceBroker struct{}

func NewServiceBroker(string) *ServiceBroker { return &ServiceBroker{} }
func (*ServiceBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Unsupported", http.StatusNotImplemented)
}
func (*ServiceBroker) Close()         {}
func (*ServiceBroker) Recover() error { return nil }
