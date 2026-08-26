package provider

import (
	"context"
	"fmt"
	"strings"
)

// Gateway routes credential verification calls by provider name + capability.
// Adding a new vendor means: provider YAML + Caller registration; credential
// types only change their provider.name / provider.capability references.
type Gateway struct {
	catalog *Catalog
	callers map[string]Caller
}

// NewGateway binds a provider catalog to runtime Callers (one per provider name).
// Every catalogued provider must have a registered Caller.
func NewGateway(catalog *Catalog, callers map[string]Caller) (*Gateway, error) {
	if catalog == nil {
		return nil, fmt.Errorf("provider catalog is required")
	}
	normalized := make(map[string]Caller, len(callers))
	for name, c := range callers {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			return nil, fmt.Errorf("caller name must not be empty")
		}
		if c == nil {
			return nil, fmt.Errorf("caller for provider %q is nil", key)
		}
		normalized[key] = c
	}
	for _, def := range catalog.All() {
		key := strings.ToLower(def.Name)
		if _, ok := normalized[key]; !ok {
			return nil, fmt.Errorf("no runtime client registered for provider %q", def.Name)
		}
	}
	return &Gateway{catalog: catalog, callers: normalized}, nil
}

// EnsureCapability fails fast when a credential-type references an unknown
// provider or capability (used at verifier-registry build time).
func (g *Gateway) EnsureCapability(providerName, capability string) error {
	if g == nil {
		return fmt.Errorf("provider gateway is nil")
	}
	def := g.catalog.Get(providerName)
	if def == nil {
		return fmt.Errorf("unknown provider %q", providerName)
	}
	capName := strings.TrimSpace(capability)
	if _, ok := def.Capabilities[capName]; !ok {
		return fmt.Errorf("provider %q has no capability %q", def.Name, capName)
	}
	if _, ok := g.callers[strings.ToLower(def.Name)]; !ok {
		return fmt.Errorf("no runtime client registered for provider %q", def.Name)
	}
	return nil
}

// Invoke resolves provider+capability to an HTTP call via the registered Caller.
func (g *Gateway) Invoke(ctx context.Context, providerName, capability string, payload any) ([]byte, int, error) {
	if err := g.EnsureCapability(providerName, capability); err != nil {
		return nil, 0, err
	}
	def := g.catalog.Get(providerName)
	cap := def.Capabilities[strings.TrimSpace(capability)]
	caller := g.callers[strings.ToLower(def.Name)]
	return caller.Call(ctx, cap.Method, cap.Path, payload)
}

// Catalog exposes loaded provider definitions.
func (g *Gateway) Catalog() *Catalog {
	if g == nil {
		return nil
	}
	return g.catalog
}
