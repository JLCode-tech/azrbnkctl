package k8s

import (
	"bytes"
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/yaml"
)

// FieldManager is the field-manager string used for every Server-Side Apply call.
const FieldManager = "azrbnkctl"

// RESTMappingWithRefresh resolves gvk to a RESTMapping, retrying once against
// a reset discovery cache if the kind is initially unknown.
func RESTMappingWithRefresh(mapper meta.RESTMapper, gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err == nil || !meta.IsNoMatchError(err) {
		return mapping, err
	}
	deferred, ok := mapper.(*restmapper.DeferredDiscoveryRESTMapper)
	if !ok {
		return nil, err
	}
	deferred.Reset()
	return mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
}

// Applier server-side-applies unstructured objects against a live Kubernetes cluster.
type Applier struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
}

// NewApplier builds a dynamic client + deferred-discovery RESTMapper from rest.Config.
func NewApplier(cfg *rest.Config) (*Applier, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("apply: dynamic client: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("apply: discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(NewNoopCachedDiscovery(disc))
	return &Applier{dyn: dyn, mapper: mapper}, nil
}

// NewApplierFromInterfaces constructs an Applier directly from interfaces (for testing).
func NewApplierFromInterfaces(dyn dynamic.Interface, mapper meta.RESTMapper) *Applier {
	return &Applier{dyn: dyn, mapper: mapper}
}

// Apply server-side-applies a single unstructured object.
func (a *Applier) Apply(ctx context.Context, obj *unstructured.Unstructured, force bool) (*unstructured.Unstructured, error) {
	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" {
		return nil, fmt.Errorf("apply: resource missing kind: %s", obj.GetName())
	}
	mapping, err := RESTMappingWithRefresh(a.mapper, gvk)
	if err != nil {
		return nil, fmt.Errorf("apply: REST mapping for %s: %w", gvk, err)
	}

	ns := obj.GetNamespace()
	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if ns == "" {
			ns = "default"
		}
		obj.SetNamespace(ns)
		ri = a.dyn.Resource(mapping.Resource).Namespace(ns)
	} else {
		ri = a.dyn.Resource(mapping.Resource)
	}

	body, err := yaml.Marshal(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("apply: marshaling %s/%s: %w", gvk.Kind, obj.GetName(), err)
	}

	po := metav1.PatchOptions{FieldManager: FieldManager}
	if force {
		t := true
		po.Force = &t
	}
	applied, err := ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, body, po)
	if err != nil {
		return nil, fmt.Errorf("apply: server-side apply %s %s/%s: %w",
			mapping.Resource.Resource, ns, obj.GetName(), err)
	}
	return applied, nil
}

// ApplyAll applies each object in objs in order.
func (a *Applier) ApplyAll(ctx context.Context, objs []*unstructured.Unstructured, force bool) error {
	for _, obj := range objs {
		if _, err := a.Apply(ctx, obj, force); err != nil {
			return err
		}
	}
	return nil
}

// ParseYAMLStream splits a multi-document YAML byte stream into unstructured objects.
func ParseYAMLStream(data []byte) ([]*unstructured.Unstructured, error) {
	parts := SplitYAMLDocs(data)
	out := make([]*unstructured.Unstructured, 0, len(parts))
	for _, doc := range parts {
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(doc, &u.Object); err != nil {
			return nil, fmt.Errorf("apply: parsing yaml: %w", err)
		}
		if len(u.Object) == 0 {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

// NewNoopCachedDiscovery adapts a discovery.DiscoveryInterface into a CachedDiscoveryInterface.
func NewNoopCachedDiscovery(delegate discovery.DiscoveryInterface) discovery.CachedDiscoveryInterface {
	return memCacheClient{DiscoveryInterface: delegate}
}

type memCacheClient struct{ discovery.DiscoveryInterface }

func (memCacheClient) Fresh() bool { return true }
func (memCacheClient) Invalidate() {}
