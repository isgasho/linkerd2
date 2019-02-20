package injector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pkgTls "github.com/linkerd/linkerd2/pkg/tls"
	"k8s.io/client-go/kubernetes"
)

// WebhookServer is the webhook's HTTP server. It has an embedded webhook which
// mutate all the requests.
type WebhookServer struct {
	*http.Server
	*Webhook
}

// NewWebhookServer returns a new instance of the WebhookServer.
func NewWebhookServer(client kubernetes.Interface, resources *WebhookResources, addr, controllerNamespace string, noInitContainer bool, rootCA *pkgTls.CA) (*WebhookServer, error) {
	c, err := tlsConfig(rootCA, controllerNamespace)
	if err != nil {
		return nil, err
	}

	server := &http.Server{
		Addr:      addr,
		TLSConfig: c,
	}

	webhook, err := NewWebhook(client, resources, controllerNamespace, noInitContainer)
	if err != nil {
		return nil, err
	}

	ws := &WebhookServer{server, webhook}
	ws.Handler = http.HandlerFunc(ws.serve)
	return ws, nil
}

func (w *WebhookServer) serve(res http.ResponseWriter, req *http.Request) {
	var (
		data []byte
		err  error
	)
	if req.Body != nil {
		data, err = ioutil.ReadAll(req.Body)
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if len(data) == 0 {
		return
	}

	response := w.Mutate(data)
	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := res.Write(responseJSON); err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Shutdown initiates a graceful shutdown of the underlying HTTP server.
func (w *WebhookServer) Shutdown() error {
	return w.Server.Shutdown(context.Background())
}

func tlsConfig(ca *pkgTls.CA, controllerNamespace string) (*tls.Config, error) {
	tlsIdentity := k8s.TLSIdentity{
		Name:                "linkerd-proxy-injector",
		Kind:                k8s.Service,
		Namespace:           controllerNamespace,
		ControllerNamespace: controllerNamespace,
	}
	dnsName := tlsIdentity.ToDNSName()

	leaf, err := ca.GenerateEndEntity(dnsName)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate end entity for %s: %s", dnsName, err)
	}

	c := &tls.Config{
		Certificates: []tls.Certificate{
			tls.Certificate{
				Certificate: leaf.Crt.EncodeTrustChainDER(),
				Leaf:        leaf.Certificate,
				PrivateKey:  leaf.PrivateKey,
			},
		},
	}
	return c, nil
}
